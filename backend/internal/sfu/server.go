package sfu

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

type Room struct {
	ID       uint
	Peers    map[uint]*Peer
	mu       sync.Mutex
}

type Peer struct {
	UserID       uint
	RoomID       uint
	PC           *webrtc.PeerConnection
	Conn         *websocket.Conn
	Tracks       map[string]*webrtc.TrackLocalStaticRTP
	Subscriptions map[uint]*webrtc.RTPSender
	mu           sync.Mutex
}

type SFUServer struct {
	rooms         map[uint]*Room
	api           *webrtc.API
	configuration webrtc.Configuration
	logger        *zap.Logger
	mu            sync.Mutex
}

type SignalingMessage struct {
	Type    string          `json:"type"` // join, leave, offer, answer, ice-candidate, muted
	RoomID  uint            `json:"room_id,omitempty"`
	UserID  uint            `json:"user_id,omitempty"`
	SDP     string          `json:"sdp,omitempty"`
	Candidate string         `json:"candidate,omitempty"`
	SDPMLineIndex *int       `json:"sdp_m_line_index,omitempty"`
	SDPMid     *string      `json:"sdp_mid,omitempty"`
	Data    interface{}     `json:"data,omitempty"`
}

func NewSFUServer(logger *zap.Logger) *SFUServer {
	configuration := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	settingEngine := webrtc.SettingEngine{}

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
	)

	return &SFUServer{
		rooms:         make(map[uint]*Room),
		api:           api,
		configuration: configuration,
		logger:        logger,
	}
}

func (s *SFUServer) onTrack(room *Room, peer *Peer, track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	localTrack, err := webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, track.ID(), track.StreamID())
	if err != nil {
		s.logger.Error("Failed to create local track", zap.Error(err))
		return
	}

	peer.Tracks[track.ID()] = localTrack

	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := track.Read(buf)
			if err != nil {
				return
			}
			_, err = localTrack.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()

	room.mu.Lock()
	defer room.mu.Unlock()

	for _, otherPeer := range room.Peers {
		if otherPeer.UserID != peer.UserID {
			sender, err := otherPeer.PC.AddTrack(localTrack)
			if err != nil {
				s.logger.Error("Failed to add track to peer", zap.Error(err))
				continue
			}
			otherPeer.Subscriptions[peer.UserID] = sender
		}
	}
}

func (s *SFUServer) onConnectionStateChange(room *Room, peer *Peer, state webrtc.PeerConnectionState) {
	if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
		room.mu.Lock()
		defer room.mu.Unlock()
		delete(room.Peers, peer.UserID)
		peer.PC.Close()

		if len(room.Peers) == 0 {
			s.mu.Lock()
			delete(s.rooms, room.ID)
			s.mu.Unlock()
		}

		if state == webrtc.PeerConnectionStateFailed {
			s.logger.Warn("Peer connection failed (ICE negotiation failed)", 
				zap.Uint("roomID", room.ID), 
				zap.Uint("userID", peer.UserID))
			s.notifyConnectionFailed(room.ID, peer.UserID)
		}
	}
}

func (s *SFUServer) notifyConnectionFailed(roomID uint, userID uint) {
	apiHost := getAPIHost()
	url := apiHost + "/api/v1/sfu/callback/" + strconv.Itoa(int(roomID)) + "/" + strconv.Itoa(int(userID)) + "/connection-failed"

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		s.logger.Error("Failed to create request for connection failed notification",
			zap.Error(err),
			zap.Uint("roomID", roomID),
			zap.Uint("userID", userID))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	_, err = client.Do(req)
	if err != nil {
		s.logger.Error("Failed to notify connection failed to API server",
			zap.Error(err),
			zap.Uint("roomID", roomID),
			zap.Uint("userID", userID))
	}
}

func getAPIHost() string {
	apiHost := os.Getenv("API_SERVER_HOST")
	if apiHost == "" {
		return "http://api-server:8080"
	}
	if !strings.HasPrefix(apiHost, "http") {
		return "http://" + apiHost
	}
	return apiHost
}

func (s *SFUServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade websocket", zap.Error(err))
		return
	}

	var peer *Peer

	defer func() {
		if peer != nil {
			s.removePeer(peer.RoomID, peer.UserID)
		}
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				s.logger.Error("Read error", zap.Error(err))
			}
			break
		}

		var msg SignalingMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			s.logger.Error("Failed to parse signaling message", zap.Error(err))
			continue
		}

		switch msg.Type {
		case "join":
			peer, err = s.handleJoin(conn, &msg)
			if err != nil {
				s.logger.Error("Join failed", zap.Error(err))
				sendError(conn, "join_failed", err.Error())
			}
		case "offer":
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			err = s.handleOffer(peer, &msg)
			if err != nil {
				s.logger.Error("Offer failed", zap.Error(err))
				sendError(conn, "offer_failed", err.Error())
			}
		case "ice-candidate":
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			err = s.handleIceCandidate(peer, &msg)
			if err != nil {
				s.logger.Error("ICE candidate failed", zap.Error(err))
			}
		}
	}
}

func (s *SFUServer) handleJoin(conn *websocket.Conn, msg *SignalingMessage) (*Peer, error) {
	s.logger.Info("Peer joining", zap.Uint("room", msg.RoomID), zap.Uint("user", msg.UserID))

	s.mu.Lock()
	if _, ok := s.rooms[msg.RoomID]; !ok {
		s.rooms[msg.RoomID] = &Room{
			ID:    msg.RoomID,
			Peers: make(map[uint]*Peer),
		}
	}
	room := s.rooms[msg.RoomID]
	s.mu.Unlock()

	room.mu.Lock()
	defer room.mu.Unlock()

	// 创建PeerConnection
	pc, err := s.api.NewPeerConnection(s.configuration)
	if err != nil {
		return nil, err
	}

	peer := &Peer{
		UserID:       msg.UserID,
		RoomID:       msg.RoomID,
		PC:           pc,
		Conn:         conn,
		Tracks:       make(map[string]*webrtc.TrackLocalStaticRTP),
		Subscriptions: make(map[uint]*webrtc.RTPSender),
	}

	// 监听轨道
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		s.onTrack(room, peer, track, receiver)
	})

	// 监听ICE候选，发送给前端
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		peer.mu.Lock()
		defer peer.mu.Unlock()

		sdpMLineIndex := int(candidate.SDPMLineIndex)
		sdpMid := candidate.SDPMid
		iceMsg := SignalingMessage{
			Type:          "ice-candidate",
			Candidate:     candidate.String(),
			SDPMLineIndex: &sdpMLineIndex,
			SDPMid:        &sdpMid,
		}
		conn.WriteJSON(iceMsg)
	})

	// 监听连接状态
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.onConnectionStateChange(room, peer, state)
	})

	// 订阅房间内已存在的其他用户的流
	for _, existingPeer := range room.Peers {
		for _, track := range existingPeer.Tracks {
			sender, err := peer.PC.AddTrack(track)
			if err == nil {
				peer.Subscriptions[existingPeer.UserID] = sender
			}
		}
	}

	room.Peers[msg.UserID] = peer

	// 通知其他房间内的新用户加入（让他们也订阅新用户的流）
	// 注意：需要等待新用户发布流后再通知

	s.logger.Info("Peer joined successfully", zap.Uint("room", msg.RoomID), zap.Uint("user", msg.UserID))

	// 发送joined确认
	conn.WriteJSON(SignalingMessage{
		Type:   "joined",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
	})

	return peer, nil
}

func (s *SFUServer) handleOffer(peer *Peer, msg *SignalingMessage) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()

	s.logger.Info("Received offer", zap.Uint("user", peer.UserID))

	err := peer.PC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	})
	if err != nil {
		return err
	}

	answer, err := peer.PC.CreateAnswer(nil)
	if err != nil {
		return err
	}

	err = peer.PC.SetLocalDescription(answer)
	if err != nil {
		return err
	}

	// 发送Answer回前端
	peer.Conn.WriteJSON(SignalingMessage{
		Type: "answer",
		SDP:  answer.SDP,
	})

	s.logger.Info("Sent answer", zap.Uint("user", peer.UserID))
	return nil
}

func (s *SFUServer) handleIceCandidate(peer *Peer, msg *SignalingMessage) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()

	candidate := webrtc.ICECandidateInit{
		Candidate: msg.Candidate,
	}
	if msg.SDPMLineIndex != nil {
		idx := uint16(*msg.SDPMLineIndex)
		candidate.SDPMLineIndex = &idx
	}
	if msg.SDPMid != nil {
		candidate.SDPMid = msg.SDPMid
	}

	err := peer.PC.AddICECandidate(candidate)
	if err != nil {
		s.logger.Warn("Failed to add ICE candidate", zap.Error(err))
		return err
	}

	s.logger.Debug("Added ICE candidate", zap.Uint("user", peer.UserID))
	return nil
}

func (s *SFUServer) removePeer(roomID, userID uint) {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return
	}

	peer.PC.Close()
	delete(room.Peers, userID)

	if len(room.Peers) == 0 {
		s.mu.Lock()
		delete(s.rooms, roomID)
		s.mu.Unlock()
	}

	s.logger.Info("Peer removed", zap.Uint("room", roomID), zap.Uint("user", userID))
}

func sendError(conn *websocket.Conn, errorType, message string) {
	conn.WriteJSON(map[string]string{
		"type":    "error",
		"error":   errorType,
		"message": message,
	})
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ==================== SFU Control Methods ====================

// KickUser force disconnects a user from the room
func (s *SFUServer) KickUser(roomID, userID uint, reason string) error {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return errors.New("peer not found in room")
	}

	// Close the peer connection
	if err := peer.PC.Close(); err != nil {
		s.logger.Error("Failed to close peer connection",
			zap.Uint("roomID", roomID),
			zap.Uint("userID", userID),
			zap.Error(err))
		return err
	}

	// Remove peer from room
	delete(room.Peers, userID)

	// Clean up empty room
	if len(room.Peers) == 0 {
		s.mu.Lock()
		delete(s.rooms, roomID)
		s.mu.Unlock()
	}

	s.logger.Info("User kicked from room",
		zap.Uint("roomID", roomID),
		zap.Uint("userID", userID),
		zap.String("reason", reason))

	return nil
}

// MuteUser mutes or unmutes a user's audio by removing/adding audio tracks
func (s *SFUServer) MuteUser(roomID, userID uint, isMuted bool) error {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return errors.New("peer not found in room")
	}

	// 发送静音通知给用户（通过信令WebSocket通知客户端）
	if peer.Conn != nil {
		muteMsg := SignalingMessage{
			Type: "muted",
			Data: map[string]bool{"is_muted": isMuted},
		}
		peer.Conn.WriteJSON(muteMsg)
	}

	s.logger.Info("User mute state changed - signaling sent to client",
		zap.Uint("roomID", roomID),
		zap.Uint("userID", userID),
		zap.Bool("isMuted", isMuted),
		zap.Int("trackCount", len(peer.Tracks)))

	return nil
}

// ClosePeerConnection force closes a specific peer connection
func (s *SFUServer) ClosePeerConnection(roomID, userID uint) error {
	return s.KickUser(roomID, userID, "administrative close")
}

// GetRoomStats returns statistics for a specific room
func (s *SFUServer) GetRoomStats(roomID uint) (uint, []uint, error) {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return 0, nil, errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peerIDs := make([]uint, 0, len(room.Peers))
	for id := range room.Peers {
		peerIDs = append(peerIDs, id)
	}

	return uint(len(room.Peers)), peerIDs, nil
}

// ListRooms returns all active rooms on this SFU node
func (s *SFUServer) ListRooms() (map[uint]uint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[uint]uint)
	for roomID, room := range s.rooms {
		room.mu.Lock()
		result[roomID] = uint(len(room.Peers))
		room.mu.Unlock()
	}

	return result, nil
}

// GetPeer returns a specific peer for advanced operations
func (s *SFUServer) GetPeer(roomID, userID uint) (*Peer, error) {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return nil, errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return nil, errors.New("peer not found")
	}

	return peer, nil
}