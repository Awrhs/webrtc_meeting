package signaling

import (
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/service"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type MessageType string

const (
	MessageTypeJoinRoom    MessageType = "join_room"
	MessageTypeLeaveRoom   MessageType = "leave_room"
	MessageTypeOffer       MessageType = "offer"
	MessageTypeAnswer      MessageType = "answer"
	MessageTypeIceCandidate MessageType = "ice_candidate"
	MessageTypeChatMessage MessageType = "chat_message"
	MessageTypeUserJoined  MessageType = "user_joined"
	MessageTypeUserLeft    MessageType = "user_left"
	MessageTypeKicked      MessageType = "kicked"
	MessageTypeMuted       MessageType = "muted"
	MessageTypeRoleChanged MessageType = "role_changed"
	MessageTypeMediaState  MessageType = "media_state"
)

type Message struct {
	Type    MessageType     `json:"type"`
	RoomID  uint            `json:"room_id,omitempty"`
	UserID  uint            `json:"user_id,omitempty"`
	TargetID uint           `json:"target_id,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	userID  uint
	roomIDs map[uint]bool
}

type Hub struct {
	clients    map[uint]*Client
	rooms      map[uint]map[uint]*Client
	roomService service.RoomService
	chatService service.ChatService
	logger     *zap.Logger
	mu         sync.Mutex
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewHub(roomService service.RoomService, chatService service.ChatService, logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[uint]*Client),
		rooms:      make(map[uint]map[uint]*Client),
		roomService: roomService,
		chatService: chatService,
		logger:     logger,
	}
}

func (h *Hub) Run() {
	for {
	}
}

func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.userID] = client
}

func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for roomID := range client.roomIDs {
		h.RemoveClientFromRoom(client, roomID)
	}
	delete(h.clients, client.userID)
	close(client.send)
}

func (h *Hub) AddClientToRoom(client *Client, roomID uint) {
	if _, ok := h.rooms[roomID]; !ok {
		h.rooms[roomID] = make(map[uint]*Client)
	}
	h.rooms[roomID][client.userID] = client
	client.roomIDs[roomID] = true
}

func (h *Hub) RemoveClientFromRoom(client *Client, roomID uint) {
	if room, ok := h.rooms[roomID]; ok {
		if _, ok := room[client.userID]; ok {
			delete(room, client.userID)
			client.roomIDs[roomID] = false
			if len(room) == 0 {
				delete(h.rooms, roomID)
			}
		}
	}
}

func (h *Hub) BroadcastToRoom(roomID uint, message []byte, excludeUserID uint) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if room, ok := h.rooms[roomID]; ok {
		for _, client := range room {
			if client.userID != excludeUserID {
				select {
				case client.send <- message:
				default:
					close(client.send)
					h.RemoveClientFromRoom(client, roomID)
					delete(h.clients, client.userID)
				}
			}
		}
	}
}

func (h *Hub) SendToUser(userID uint, message []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, ok := h.clients[userID]; ok {
		select {
		case client.send <- message:
		default:
			close(client.send)
			delete(h.clients, userID)
		}
	}
}

func (h *Hub) BroadcastUserJoined(roomID uint, userID uint) {
	msg := Message{
		Type:   MessageTypeUserJoined,
		RoomID: roomID,
		UserID: userID,
	}
	data, _ := json.Marshal(msg)
	h.BroadcastToRoom(roomID, data, 0)
}

func (h *Hub) BroadcastUserLeft(roomID uint, userID uint) {
	msg := Message{
		Type:   MessageTypeUserLeft,
		RoomID: roomID,
		UserID: userID,
	}
	data, _ := json.Marshal(msg)
	h.BroadcastToRoom(roomID, data, 0)
}

func (h *Hub) BroadcastKick(roomID uint, targetID uint) {
	msg := Message{
		Type:    MessageTypeKicked,
		RoomID:  roomID,
		TargetID: targetID,
	}
	data, _ := json.Marshal(msg)
	h.BroadcastToRoom(roomID, data, 0)
	h.SendToUser(targetID, data)
}

func (h *Hub) BroadcastMute(roomID uint, targetID uint, isMuted bool) {
	data, _ := json.Marshal(map[string]bool{"is_muted": isMuted})
	msg := Message{
		Type:   MessageTypeMuted,
		RoomID: roomID,
		UserID: targetID,
		Data:   json.RawMessage(data),
	}
	data, _ = json.Marshal(msg)
	h.BroadcastToRoom(roomID, data, 0)
}

func (h *Hub) BroadcastRoleChanged(roomID uint, userID uint, role models.Role) {
	data, _ := json.Marshal(map[string]interface{}{"role": role})
	msg := Message{
		Type:   MessageTypeRoleChanged,
		RoomID: roomID,
		UserID: userID,
		Data:   data,
	}
	raw, _ := json.Marshal(msg)
	h.BroadcastToRoom(roomID, raw, 0)
}

func (h *Hub) BroadcastMessage(roomID uint, message *models.Message, receiverID *uint) {
	if receiverID != nil {
		data, _ := json.Marshal(message)
		msg := Message{
			Type:    MessageTypeChatMessage,
			RoomID:  roomID,
			UserID:  message.SenderID,
			Data:    data,
		}
		raw, _ := json.Marshal(msg)
		h.SendToUser(message.SenderID, raw)
		h.SendToUser(*receiverID, raw)
	} else {
		msg := Message{
			Type:    MessageTypeChatMessage,
			RoomID:  roomID,
			UserID:  message.SenderID,
		}
		msg.Data, _ = json.Marshal(message)
		raw, _ := json.Marshal(msg)
		h.BroadcastToRoom(roomID, raw, 0)
	}
}

func (c *Client) ReadPump() {
		defer func() {
			c.hub.Unregister(c)
			c.conn.Close()
		}()

		for {
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("error: %v", err)
				}
				break
			}

			var msg Message
			if err = json.Unmarshal(message, &msg); err != nil {
				continue
			}

			c.handleMessage(&msg)
		}
	}

func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case MessageTypeJoinRoom:
		c.hub.AddClientToRoom(c, msg.RoomID)
		c.hub.BroadcastUserJoined(msg.RoomID, c.userID)
	case MessageTypeLeaveRoom:
		c.hub.RemoveClientFromRoom(c, msg.RoomID)
		c.hub.BroadcastUserLeft(msg.RoomID, c.userID)
	case MessageTypeOffer, MessageTypeAnswer, MessageTypeIceCandidate:
		if msg.TargetID != 0 {
			data, _ := json.Marshal(msg)
			c.hub.SendToUser(msg.TargetID, data)
		} else {
			data, _ := json.Marshal(msg)
			c.hub.BroadcastToRoom(msg.RoomID, data, c.userID)
		}
	case MessageTypeMediaState:
		data, _ := json.Marshal(msg)
		c.hub.BroadcastToRoom(msg.RoomID, data, c.userID)
	}
}

func (c *Client) WritePump() {
	defer c.conn.Close()
	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Printf("write error: %v", err)
			break
		}
	}
}

func HandleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request, userID uint, logger *zap.Logger) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade websocket", zap.Error(err))
		return
	}

	client := &Client{
		hub:     hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		userID:  userID,
		roomIDs: make(map[uint]bool),
	}

	hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}