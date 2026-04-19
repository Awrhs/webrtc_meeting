package sfu

import (
	"encoding/json"
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
	Tracks       map[string]*webrtc.TrackLocalStaticRTP
	Subscriptions map[uint]*webrtc.RTPSender
}

type SFUServer struct {
	rooms         map[uint]*Room
	api           *webrtc.API
	configuration webrtc.Configuration
	logger        *zap.Logger
	mu            sync.Mutex
}

type OfferRequest struct {
	RoomID uint   `json:"room_id"`
	UserID uint   `json:"user_id"`
	SDP    string `json:"sdp"`
}

type AnswerResponse struct {
	SDP string `json:"sdp"`
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

func (s *SFUServer) HandleOffer(w http.ResponseWriter, r *http.Request) {
	var req OfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if _, ok := s.rooms[req.RoomID]; !ok {
		s.rooms[req.RoomID] = &Room{
			ID:    req.RoomID,
			Peers: make(map[uint]*Peer),
		}
	}
	room := s.rooms[req.RoomID]
	s.mu.Unlock()

	room.mu.Lock()
	defer room.mu.Unlock()

	peerConnection, err := s.api.NewPeerConnection(s.configuration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	peer := &Peer{
		UserID:       req.UserID,
		RoomID:       req.RoomID,
		PC:           peerConnection,
		Tracks:       make(map[string]*webrtc.TrackLocalStaticRTP),
		Subscriptions: make(map[uint]*webrtc.RTPSender),
	}

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		s.onTrack(room, peer, track, receiver)
	})

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.onConnectionStateChange(room, peer, state)
	})

	err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  req.SDP,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, existingPeer := range room.Peers {
		if existingPeer.UserID != peer.UserID {
			for _, track := range existingPeer.Tracks {
				sender, err := peer.PC.AddTrack(track)
				if err == nil {
					peer.Subscriptions[existingPeer.UserID] = sender
				}
			}
		}
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	room.Peers[req.UserID] = peer

	response := AnswerResponse{
		SDP: answer.SDP,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Content-Type", "application/json")
	
	_, err := client.Do(req)
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
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				s.logger.Error("Read error", zap.Error(err))
			}
			break
		}

		var msg struct {
			Type string `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		conn.WriteMessage(websocket.TextMessage, message)
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}