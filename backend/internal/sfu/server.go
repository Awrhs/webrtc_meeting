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
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

type Room struct {
	ID       uint
	Peers    map[uint]*Peer
	mu       sync.Mutex
}

type TrackInfo struct {
	Track        *webrtc.TrackLocalStaticRTP
	SSRC         uint32
	Kind         string // "audio" or "video"
	StreamID     string // e.g., "camera" or "screen"
	IsEnabled    bool
}

type Peer struct {
	UserID       uint
	RoomID       uint
	PC           *webrtc.PeerConnection
	Conn         *websocket.Conn
	Tracks       map[string]*TrackInfo // key: track kind + "-" + stream_id (e.g., "video-camera", "video-screen", "audio-main")
	Subscriptions map[uint][]*webrtc.RTPSender // key: fromUserID, value: list of senders (one per track)
	mu           sync.Mutex
}

type SFUServer struct {
	rooms         map[uint]*Room
	api           *webrtc.API
	configuration webrtc.Configuration
	logger        *zap.Logger
	mu            sync.Mutex
}

type SSRC struct {
	SSRC     uint32 `json:"ssrc"`
	UserID   uint   `json:"user_id"`
	Kind     string `json:"kind"`     // "audio" or "video"
	StreamID string `json:"stream_id"` // "camera" or "screen"
}

type SignalingMessage struct {
	Type          string          `json:"type"` // join, leave, offer, answer, ice-candidate, muted, ssrc_mapping
	RoomID        uint            `json:"room_id,omitempty"`
	UserID        uint            `json:"user_id,omitempty"`
	SDP           string          `json:"sdp,omitempty"`
	Candidate     string          `json:"candidate,omitempty"`
	SDPMLineIndex *int            `json:"sdp_m_line_index,omitempty"`
	SDPMid        *string         `json:"sdp_mid,omitempty"`
	Data          interface{}     `json:"data,omitempty"`
	SSRCs         []SSRC          `json:"ssrcs,omitempty"` // for ssrc_mapping message
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

	// WebRTC 端口范围配置（可选）
	if minPortStr := os.Getenv("WEBRTC_PORT_RANGE_MIN"); minPortStr != "" {
		if maxPortStr := os.Getenv("WEBRTC_PORT_RANGE_MAX"); maxPortStr != "" {
			minPort, _ := strconv.Atoi(minPortStr)
			maxPort, _ := strconv.Atoi(maxPortStr)
			if minPort > 0 && maxPort > minPort {
				settingEngine.SetEphemeralUDPPortRange(uint16(minPort), uint16(maxPort))
			}
		}
	}

	// 允许回环候选，支持本地开发测试
	settingEngine.SetIncludeLoopbackCandidate(true)

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

// onTrack 当 SFU 从某个浏览器收到 RTP 媒体轨道时触发
// 核心职责：1) 将收到的远程轨道包装为本地可转发轨道；2) 启动 RTP 包转发 goroutine；
//          3) 将新书目推送给房间内所有其他用户；4) 触发重新协商让已连接用户接收新轨道
//
// 预期正确逻辑：
//   当用户A先连接、用户B后加入时，用户B的handleJoin会为其PC添加用户A的现有轨道（用户B能看到A）。
//   但用户A的PC已经连接完成，如果不重新协商，用户A的浏览器不会知道有新的接收轨道，就看不到B。
//   因此，在 onTrack 中给已连接用户的PC AddTrack 后，必须调用 renegotiate() 创建新offer并发送给浏览器，
//   浏览器收到offer后创建answer回传，SFU设置远程描述，新轨道才能正式传输。
func (s *SFUServer) onTrack(room *Room, peer *Peer, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	trackKind := string(track.Kind())
	streamID := track.StreamID()
	trackKey := trackKind + "-" + streamID

	s.logger.Info("New track received",
		zap.Uint("userID", peer.UserID),
		zap.String("kind", trackKind),
		zap.String("streamID", streamID),
		zap.Uint32("ssrc", uint32(track.SSRC())))

	// 创建本地可转发轨道：TrackLocalStaticRTP 用于将收到的 RTP 包原样转发给其他用户
	localTrack, err := webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, track.ID(), streamID)
	if err != nil {
		s.logger.Error("Failed to create local track", zap.Error(err))
		return
	}

	// 统一锁顺序：先 room.mu 后 peer.mu，避免与 handleJoin 发生死锁
	// handleJoin 获取锁的顺序也是 room.mu -> peer.mu，保持一致
	room.mu.Lock()
	defer room.mu.Unlock()

	peer.mu.Lock()
	peer.Tracks[trackKey] = &TrackInfo{
		Track:     localTrack,
		SSRC:      uint32(track.SSRC()),
		Kind:      trackKind,
		StreamID:  streamID,
		IsEnabled: true,
	}
	peer.mu.Unlock()

	// 启动 RTP 包转发 goroutine：从浏览器发来的 track 读取 RTP 包，写入 localTrack
	// localTrack 会被挂载到其他用户的 PeerConnection 上，Pion 会自动将其 RTP 包发送出去
	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := track.Read(buf)
			if err != nil {
				s.logger.Debug("Track read ended, stopping forwarding",
					zap.Uint("userID", peer.UserID),
					zap.String("trackKey", trackKey))
				return
			}

			peer.mu.Lock()
			trackInfo := peer.Tracks[trackKey]
			isEnabled := trackInfo != nil && trackInfo.IsEnabled
			peer.mu.Unlock()

			// 只有启用的轨道才转发（被 MuteUser 禁用的轨道会跳过）
			if isEnabled {
				_, err = localTrack.Write(buf[:n])
				if err != nil {
					return
				}
			}
		}
	}()

	// 构建 SSRC 映射消息：通知所有客户端有一条新轨道到达（用于前端将流归属到具体用户）
	ssrcMapping := SignalingMessage{
		Type:   "ssrc_mapping",
		RoomID: room.ID,
		SSRCs: []SSRC{
			{
				SSRC:     uint32(track.SSRC()),
				UserID:   peer.UserID,
				Kind:     trackKind,
				StreamID: streamID,
			},
		},
	}

	for _, otherPeer := range room.Peers {
		// 给所有用户（包括发布者自己）发送 SSRC 映射，方便前端做流归属
		if otherPeer.Conn != nil {
			otherPeer.Conn.WriteJSON(ssrcMapping)
		}

		// 给其他用户添加轨道订阅：将 localTrack 挂载到对方的 PeerConnection 上
		if otherPeer.UserID != peer.UserID {
			sender, err := otherPeer.PC.AddTrack(localTrack)
			if err != nil {
				s.logger.Error("Failed to add track to peer",
					zap.Uint("fromUser", peer.UserID),
					zap.Uint("toUser", otherPeer.UserID),
					zap.Error(err))
				continue
			}
			otherPeer.Subscriptions[peer.UserID] = append(otherPeer.Subscriptions[peer.UserID], sender)
			s.logger.Debug("Track subscribed",
				zap.Uint("from", peer.UserID),
				zap.Uint("to", otherPeer.UserID),
				zap.String("trackKey", trackKey))

			// 【关键修复】为新加入的轨道触发重新协商
			// 原因：otherPeer 的 PC 可能已经连接完成（answer 已交换），浏览器不知道有新轨道
			// 做法：SFU 主动创建新 offer 发送给浏览器，浏览器回复 answer，完成二次协商
			go s.renegotiate(otherPeer)
		}
	}
}

// renegotiate 当已连接的 PeerConnection 需要增加新接收轨道时触发重新协商
// 预期正确逻辑：
//   WebRTC 连接建立后，如果一方需要增加新的发送轨道（如SFU要转发新用户的书目），
//   必须通过新的 Offer/Answer 交换让对端知道有新的 m-line 或 SSRC。
//   这里SFU作为offerer创建新offer，通过WebSocket发送给浏览器，浏览器回复answer，
//   SFU收到answer后调用SetRemoteDescription，新轨道才能开始传输RTP包。
func (s *SFUServer) renegotiate(peer *Peer) {
	// 短暂等待，避免与 SetLocalDescription 的 ICE 候选生成冲突
	// Pion 中 SetLocalDescription 后需要一小段时间才能安全调用 CreateOffer
	time.Sleep(100 * time.Millisecond)

	peer.mu.Lock()
	defer peer.mu.Unlock()

	s.logger.Info("Starting renegotiation", zap.Uint("userID", peer.UserID))

	// 创建重新协商用的 Offer
	offer, err := peer.PC.CreateOffer(nil)
	if err != nil {
		s.logger.Error("Failed to create renegotiation offer", zap.Uint("userID", peer.UserID), zap.Error(err))
		return
	}

	// 设置本地描述：Pion 会根据本地轨道生成 SDP
	err = peer.PC.SetLocalDescription(offer)
	if err != nil {
		s.logger.Error("Failed to set local description for renegotiation", zap.Uint("userID", peer.UserID), zap.Error(err))
		return
	}

	// 通过 WebSocket 将新的 Offer 发送给浏览器
	if peer.Conn != nil {
		peer.Conn.WriteJSON(SignalingMessage{
			Type: "offer",
			SDP:  offer.SDP,
		})
		s.logger.Info("Sent renegotiation offer", zap.Uint("userID", peer.UserID))
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
		return "http://localhost:8080"
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
		case "answer":
			// 处理浏览器回复的 Answer（用于重新协商场景）
			// 初次协商时浏览器是 offerer、SFU 是 answerer，浏览器不会发 answer。
			// 重新协商时 SFU 是 offerer，浏览器回复 answer，需要在这里处理。
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			err = s.handleAnswer(peer, &msg)
			if err != nil {
				s.logger.Error("Answer handling failed", zap.Uint("userID", peer.UserID), zap.Error(err))
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
		Tracks:       make(map[string]*TrackInfo),
		Subscriptions: make(map[uint][]*webrtc.RTPSender),
	}

	// 监听轨道
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		s.onTrack(room, peer, track, receiver)
	})

	// 监听ICE候选，发送给前端
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			s.logger.Info("ICE gathering completed",
				zap.Uint("userID", peer.UserID))
			return
		}

		candidateInit := candidate.ToJSON()
		s.logger.Info("ICE candidate generated",
			zap.Uint("userID", peer.UserID),
			zap.String("candidate", candidateInit.Candidate),
			zap.String("foundation", candidate.Foundation),
			zap.Uint16("component", candidate.Component),
			zap.String("protocol", string(candidate.Protocol)),
			zap.Uint32("priority", candidate.Priority),
			zap.String("address", candidate.Address),
			zap.Uint16("port", candidate.Port),
			zap.String("typ", string(candidate.Typ)))

		peer.mu.Lock()
		defer peer.mu.Unlock()

		var sdpMLineIndex *int
		if candidateInit.SDPMLineIndex != nil {
			idx := int(*candidateInit.SDPMLineIndex)
			sdpMLineIndex = &idx
		}
		iceMsg := SignalingMessage{
			Type:          "ice-candidate",
			Candidate:     candidateInit.Candidate,
			SDPMLineIndex: sdpMLineIndex,
			SDPMid:        candidateInit.SDPMid,
		}
		conn.WriteJSON(iceMsg)
	})

	// 监听连接状态
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.onConnectionStateChange(room, peer, state)
	})

	room.Peers[msg.UserID] = peer

	// 发送joined确认
	conn.WriteJSON(SignalingMessage{
		Type:   "joined",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
	})

	// 重要：订阅房间内已存在的其他用户的所有流
	// 注意：room.mu 已在函数开头通过 defer 持有，此处无需再次加锁
	var allSSRCs []SSRC
	for _, existingPeer := range room.Peers {
		if existingPeer.UserID != peer.UserID {
			existingPeer.mu.Lock()
			for trackKey, trackInfo := range existingPeer.Tracks {
				sender, err := peer.PC.AddTrack(trackInfo.Track)
				if err == nil {
					peer.Subscriptions[existingPeer.UserID] = append(peer.Subscriptions[existingPeer.UserID], sender)
					allSSRCs = append(allSSRCs, SSRC{
						SSRC:     trackInfo.SSRC,
						UserID:   existingPeer.UserID,
						Kind:     trackInfo.Kind,
						StreamID: trackInfo.StreamID,
					})
					s.logger.Debug("Subscribed to existing track",
						zap.Uint("newUser", peer.UserID),
						zap.Uint("existingUser", existingPeer.UserID),
						zap.String("trackKey", trackKey))
				} else {
					s.logger.Warn("Failed to subscribe existing track",
						zap.Uint("newUser", peer.UserID),
						zap.Uint("existingUser", existingPeer.UserID),
						zap.String("trackKey", trackKey),
						zap.Error(err))
				}
			}
			existingPeer.mu.Unlock()
		}
	}

	// 如果有现有流，发送批量SSRC映射给新用户
	if len(allSSRCs) > 0 {
		peer.Conn.WriteJSON(SignalingMessage{
			Type:   "ssrc_mapping",
			RoomID: msg.RoomID,
			SSRCs:  allSSRCs,
		})
		s.logger.Info("Existing tracks subscribed",
			zap.Uint("userID", peer.UserID),
			zap.Int("tracksCount", len(allSSRCs)))
	}

	s.logger.Info("Peer joined successfully",
		zap.Uint("room", msg.RoomID),
		zap.Uint("user", msg.UserID),
		zap.Int("roomPeersCount", len(room.Peers)))

	return peer, nil
}

// handleOffer 处理浏览器发来的初始 SDP Offer（初次协商）
// 预期正确逻辑：
//   浏览器是 offerer，SFU 是 answerer。
//   1) SFU 将浏览器的 offer 设为远程描述（声明浏览器要发送的轨道）
//   2) SFU 创建 answer（包含 SFU 要发送给浏览器的轨道，如现有用户的流）
//   3) SFU 将 answer 设为本地描述，启动 ICE 收集
//   4) 通过 WebSocket 将 answer 发送回浏览器
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

// handleAnswer 处理浏览器回复的 SDP Answer（重新协商场景）
// 预期正确逻辑：
//   重新协商时 SFU 是 offerer，浏览器是 answerer。
//   浏览器收到 renegotiate() 发来的 offer 后，创建 answer 并发回。
//   SFU 收到 answer 后设为远程描述，新添加的轨道即可开始传输。
func (s *SFUServer) handleAnswer(peer *Peer, msg *SignalingMessage) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()

	s.logger.Info("Received answer (renegotiation)", zap.Uint("user", peer.UserID))

	err := peer.PC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  msg.SDP,
	})
	if err != nil {
		return err
	}

	s.logger.Info("Renegotiation completed", zap.Uint("user", peer.UserID))
	return nil
}

func (s *SFUServer) handleIceCandidate(peer *Peer, msg *SignalingMessage) error {
	peer.mu.Lock()
	defer peer.mu.Unlock()

	candidateStr := msg.Candidate

	candidate := webrtc.ICECandidateInit{
		Candidate: candidateStr,
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

	s.logger.Info("Added remote ICE candidate",
		zap.Uint("user", peer.UserID),
		zap.String("candidate", candidateStr[:min(len(candidateStr), 100)]))
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

// MuteUser mutes or unmutes a user's audio by enabling/disabling track forwarding
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

	peer.mu.Lock()
	defer peer.mu.Unlock()

	// 实际控制所有音频轨道的转发
	audioTracksCount := 0
	for trackKey, trackInfo := range peer.Tracks {
		if trackInfo.Kind == "audio" {
			trackInfo.IsEnabled = !isMuted
			audioTracksCount++
			s.logger.Debug("Audio track state changed",
				zap.String("trackKey", trackKey),
				zap.Bool("enabled", trackInfo.IsEnabled))
		}
	}

	// 发送静音通知给用户（通过信令WebSocket通知客户端）
	if peer.Conn != nil {
		muteMsg := SignalingMessage{
			Type:   "muted",
			UserID: userID,
			Data:   map[string]bool{"is_muted": isMuted},
		}
		peer.Conn.WriteJSON(muteMsg)
	}

	// 广播静音状态给房间内所有用户
	for _, otherPeer := range room.Peers {
		if otherPeer.Conn != nil && otherPeer.UserID != userID {
			otherPeer.Conn.WriteJSON(SignalingMessage{
				Type:   "muted",
				UserID: userID,
				Data:   map[string]bool{"is_muted": isMuted},
			})
		}
	}

	s.logger.Info("User mute state changed - tracks enabled/disabled",
		zap.Uint("roomID", roomID),
		zap.Uint("userID", userID),
		zap.Bool("isMuted", isMuted),
		zap.Int("audioTracksCount", audioTracksCount))

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