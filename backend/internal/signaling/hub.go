package signaling

import (
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/service"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// safeMarshal 封装 json.Marshal，失败时打 warn 日志
// 内部消息结构体序列化理论上不会失败，失败即表示代码有bug
func safeMarshal(v interface{}, logger *zap.Logger) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		logger.Warn("json.Marshal failed", zap.Any("data", v), zap.Error(err))
	}
	return data
}

type MessageType string

const (
	MessageTypeJoinRoom         MessageType = "join_room"
	MessageTypeLeaveRoom        MessageType = "leave_room"
	MessageTypeOffer            MessageType = "offer"
	MessageTypeAnswer           MessageType = "answer"
	MessageTypeIceCandidate     MessageType = "ice_candidate"
	MessageTypeChatMessage      MessageType = "chat_message"
	MessageTypeUserJoined       MessageType = "user_joined"
	MessageTypeUserLeft         MessageType = "user_left"
	MessageTypeKicked           MessageType = "kicked"
	MessageTypeMuted            MessageType = "muted"
	MessageTypeRoleChanged      MessageType = "role_changed"
	MessageTypeMediaState       MessageType = "media_state"
	MessageTypeConnectionFailed MessageType = "connection_failed"
	MessageTypePublicKey        MessageType = "public_key"
	MessageTypeSenderKey        MessageType = "sender_key"
	MessageTypeKeyRotation      MessageType = "key_rotation"
)

type Message struct {
	Type     MessageType     `json:"type"`
	RoomID   uint            `json:"room_id,omitempty"`
	UserID   uint            `json:"user_id,omitempty"`
	TargetID uint            `json:"target_id,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	userID  uint
	roomIDs map[uint]bool
	mu      sync.RWMutex
}

type Hub struct {
	// 所有在线客户端
	clients map[uint]*Client
	// 房间 -> 房间内的客户端集合
	rooms map[uint]map[uint]*Client

	// Channel用于序列化所有操作（不需要互斥锁）
	register       chan *Client
	unregister     chan *Client
	broadcast      chan *BroadcastMessage
	sendToUser     chan *UserMessage
	addToRoom      chan *RoomClientAction
	removeFromRoom chan *RoomClientAction

	roomService    service.RoomService
	chatService    service.ChatService
	allowedOrigins []string
	logger         *zap.Logger
}

type RoomClientAction struct {
	Client *Client
	RoomID uint
}

type BroadcastMessage struct {
	RoomID        uint
	Message       []byte
	ExcludeUserID uint
}

type UserMessage struct {
	UserID  uint
	Message []byte
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == ""
	},
}

func NewHub(roomService service.RoomService, chatService service.ChatService, allowedOrigins []string, logger *zap.Logger) *Hub {
	return &Hub{
		clients:        make(map[uint]*Client),
		rooms:          make(map[uint]map[uint]*Client),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		broadcast:      make(chan *BroadcastMessage),
		sendToUser:     make(chan *UserMessage),
		addToRoom:      make(chan *RoomClientAction),
		removeFromRoom: make(chan *RoomClientAction),
		roomService:    roomService,
		chatService:    chatService,
		allowedOrigins: allowedOrigins,
		logger:         logger,
	}
}

// Run 是Hub的主事件循环，所有操作通过channel串行化执行，无需互斥锁
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			// 同一 userID 重复注册（如同一账号开两个标签页）时，踢掉旧连接：
			// 否则 h.clients[userID] 会被覆盖，旧连接残留在房间映射里，
			// 其后续 Unregister 还会误删新连接的注册状态。
			if existing, ok := h.clients[client.userID]; ok && existing != client {
				h.removeClientNoLock(existing)
			}
			h.clients[client.userID] = client
			h.logger.Info("Client registered", zap.Uint("userID", client.userID))

		case client := <-h.unregister:
			// 幂等且按对象身份匹配：只有当前注册的还是这个 client 才清理。
			// （client 可能已被慢消费者清理路径移除，或已被新连接顶替）
			if current, ok := h.clients[client.userID]; ok && current == client {
				h.removeClientNoLock(client)
				h.logger.Info("Client unregistered", zap.Uint("userID", client.userID))
			}

		case msg := <-h.broadcast:
			if room, ok := h.rooms[msg.RoomID]; ok {
				for _, client := range room {
					if client.userID != msg.ExcludeUserID {
						select {
						case client.send <- msg.Message:
						default:
							// 发送队列满：完整移除该客户端（所有房间 + 全局表）。
							// 只从当前房间移除会使其残留在其他房间映射里，
							// 之后广播会向已关闭的 send 通道发送，导致整个 Hub panic。
							h.removeClientNoLock(client)
						}
					}
				}
			}

		case msg := <-h.sendToUser:
			if client, ok := h.clients[msg.UserID]; ok {
				select {
				case client.send <- msg.Message:
				default:
					h.removeClientNoLock(client)
				}
			}

		case action := <-h.addToRoom:
			if _, ok := h.rooms[action.RoomID]; !ok {
				h.rooms[action.RoomID] = make(map[uint]*Client)
			}
			h.rooms[action.RoomID][action.Client.userID] = action.Client
			action.Client.setRoomJoined(action.RoomID, true)
			h.logger.Info("Client added to room", zap.Uint("userID", action.Client.userID), zap.Uint("roomID", action.RoomID))

		case action := <-h.removeFromRoom:
			h.removeClientFromRoomNoLock(action.Client, action.RoomID)
			h.logger.Info("Client removed from room", zap.Uint("userID", action.Client.userID), zap.Uint("roomID", action.RoomID))
		}
	}
}

// ==================== 外部调用方法（通过channel提交任务） ====================

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) BroadcastToRoom(roomID uint, message []byte, excludeUserID uint) {
	h.broadcast <- &BroadcastMessage{
		RoomID:        roomID,
		Message:       message,
		ExcludeUserID: excludeUserID,
	}
}

func (h *Hub) SendToUser(userID uint, message []byte) {
	h.sendToUser <- &UserMessage{
		UserID:  userID,
		Message: message,
	}
}

func (h *Hub) AddClientToRoom(client *Client, roomID uint) {
	h.addToRoom <- &RoomClientAction{
		Client: client,
		RoomID: roomID,
	}
}

func (h *Hub) RemoveClientFromRoom(client *Client, roomID uint) {
	h.removeFromRoom <- &RoomClientAction{
		Client: client,
		RoomID: roomID,
	}
}

// ==================== 辅助方法（仅在Run() goroutine内部调用，无需锁） ====================

// removeClientNoLock 从所有房间和全局客户端表中完整移除 client，并关闭其 send 通道。
// 关闭 send 会让 WritePump 退出并关闭底层连接，进而触发 ReadPump 的 Unregister（幂等）。
// 【注意】仅在 Run() goroutine 内部调用，无需锁。
func (h *Hub) removeClientNoLock(client *Client) {
	for _, roomID := range client.joinedRoomIDs() {
		h.removeClientFromRoomNoLock(client, roomID)
	}
	if current, ok := h.clients[client.userID]; ok && current == client {
		delete(h.clients, client.userID)
	}
	close(client.send)
}

func (h *Hub) removeClientFromRoomNoLock(client *Client, roomID uint) {
	if room, ok := h.rooms[roomID]; ok {
		if _, ok := room[client.userID]; ok {
			delete(room, client.userID)
			client.setRoomJoined(roomID, false)
			if len(room) == 0 {
				delete(h.rooms, roomID)
			}
		}
	}
}

func (c *Client) setRoomJoined(roomID uint, joined bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if joined {
		c.roomIDs[roomID] = true
		return
	}
	delete(c.roomIDs, roomID)
}

func (c *Client) isRoomJoined(roomID uint) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.roomIDs[roomID]
}

func (c *Client) joinedRoomIDs() []uint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	roomIDs := make([]uint, 0, len(c.roomIDs))
	for roomID := range c.roomIDs {
		roomIDs = append(roomIDs, roomID)
	}
	return roomIDs
}

func (c *Client) isActiveRoomMember(roomID uint) bool {
	_, err := c.hub.roomService.GetMemberRole(roomID, c.userID)
	return err == nil
}

func (c *Client) isActiveTargetMember(roomID, targetID uint) bool {
	_, err := c.hub.roomService.GetMemberRole(roomID, targetID)
	return err == nil
}

func (h *Hub) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range h.allowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

// ==================== 业务方法 ====================

func (h *Hub) BroadcastUserJoined(roomID uint, userID uint) {
	msg := Message{
		Type:   MessageTypeUserJoined,
		RoomID: roomID,
		UserID: userID,
	}
	data := safeMarshal(msg, h.logger)
	h.BroadcastToRoom(roomID, data, 0)
}

func (h *Hub) BroadcastUserLeft(roomID uint, userID uint) {
	msg := Message{
		Type:   MessageTypeUserLeft,
		RoomID: roomID,
		UserID: userID,
	}
	data := safeMarshal(msg, h.logger)
	h.BroadcastToRoom(roomID, data, 0)
}

func (h *Hub) BroadcastKick(roomID uint, targetID uint) {
	msg := Message{
		Type:     MessageTypeKicked,
		RoomID:   roomID,
		TargetID: targetID,
	}
	data := safeMarshal(msg, h.logger)
	h.BroadcastToRoom(roomID, data, 0)
	h.SendToUser(targetID, data)
}

func (h *Hub) BroadcastMute(roomID uint, targetID uint, isMuted bool) {
	data := safeMarshal(map[string]bool{"is_muted": isMuted}, h.logger)
	msg := Message{
		Type:   MessageTypeMuted,
		RoomID: roomID,
		UserID: targetID,
		Data:   json.RawMessage(data),
	}
	raw := safeMarshal(msg, h.logger)
	h.BroadcastToRoom(roomID, raw, 0)
}

func (h *Hub) BroadcastRoleChanged(roomID uint, userID uint, role models.Role) {
	data := safeMarshal(map[string]interface{}{"role": role}, h.logger)
	msg := Message{
		Type:   MessageTypeRoleChanged,
		RoomID: roomID,
		UserID: userID,
		Data:   json.RawMessage(data),
	}
	raw := safeMarshal(msg, h.logger)
	h.BroadcastToRoom(roomID, raw, 0)
}

func (h *Hub) SendConnectionFailed(roomID uint, userID uint) {
	msg := Message{
		Type:   MessageTypeConnectionFailed,
		RoomID: roomID,
		UserID: userID,
	}
	data := safeMarshal(msg, h.logger)
	h.SendToUser(userID, data)
}

func (h *Hub) BroadcastMediaState(roomID uint, userID uint, videoEnabled bool, audioEnabled bool) {
	data := safeMarshal(map[string]bool{
		"video_enabled": videoEnabled,
		"audio_enabled": audioEnabled,
	}, h.logger)
	msg := Message{
		Type:   MessageTypeMediaState,
		RoomID: roomID,
		UserID: userID,
		Data:   json.RawMessage(data),
	}
	raw := safeMarshal(msg, h.logger)
	h.BroadcastToRoom(roomID, raw, 0)
}

func (h *Hub) BroadcastMessage(roomID uint, message *models.Message, receiverID *uint) {
	if receiverID != nil {
		data := safeMarshal(message, h.logger)
		msg := Message{
			Type:   MessageTypeChatMessage,
			RoomID: roomID,
			UserID: message.SenderID,
			Data:   json.RawMessage(data),
		}
		raw := safeMarshal(msg, h.logger)
		h.SendToUser(message.SenderID, raw)
		h.SendToUser(*receiverID, raw)
	} else {
		msg := Message{
			Type:   MessageTypeChatMessage,
			RoomID: roomID,
			UserID: message.SenderID,
		}
		msg.Data = safeMarshal(message, h.logger)
		raw := safeMarshal(msg, h.logger)
		h.BroadcastToRoom(roomID, raw, 0)
	}
}

// ==================== Client 读写循环 ====================

func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Error("Read error", zap.Error(err))
			}
			break
		}

		var msg Message
		if err = json.Unmarshal(message, &msg); err != nil {
			c.hub.logger.Warn("Failed to parse message", zap.Error(err))
			continue
		}

		c.handleMessage(&msg)
	}
}

func (c *Client) handleMessage(msg *Message) {
	msg.UserID = c.userID

	switch msg.Type {
	case MessageTypeJoinRoom:
		if msg.RoomID == 0 || !c.isActiveRoomMember(msg.RoomID) {
			c.hub.logger.Warn("Unauthorized signaling room join", zap.Uint("userID", c.userID), zap.Uint("roomID", msg.RoomID))
			return
		}
		c.hub.AddClientToRoom(c, msg.RoomID)
		c.hub.BroadcastUserJoined(msg.RoomID, c.userID)
	case MessageTypeLeaveRoom:
		if !c.isRoomJoined(msg.RoomID) {
			return
		}
		c.hub.RemoveClientFromRoom(c, msg.RoomID)
		c.hub.BroadcastUserLeft(msg.RoomID, c.userID)
	case MessageTypeOffer, MessageTypeAnswer, MessageTypeIceCandidate, MessageTypeMediaState, MessageTypePublicKey, MessageTypeSenderKey, MessageTypeKeyRotation:
		if msg.RoomID == 0 || !c.isRoomJoined(msg.RoomID) || !c.isActiveRoomMember(msg.RoomID) {
			c.hub.logger.Warn("Unauthorized signaling message", zap.Uint("userID", c.userID), zap.Uint("roomID", msg.RoomID), zap.String("type", string(msg.Type)))
			return
		}
		if msg.TargetID != 0 {
			if !c.isActiveTargetMember(msg.RoomID, msg.TargetID) {
				c.hub.logger.Warn("Unauthorized signaling target", zap.Uint("userID", c.userID), zap.Uint("roomID", msg.RoomID), zap.Uint("targetID", msg.TargetID), zap.String("type", string(msg.Type)))
				return
			}
			data := safeMarshal(msg, c.hub.logger)
			c.hub.SendToUser(msg.TargetID, data)
			return
		}
		data := safeMarshal(msg, c.hub.logger)
		c.hub.BroadcastToRoom(msg.RoomID, data, c.userID)
	}
}

func (c *Client) WritePump() {
	defer c.conn.Close()
	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			c.hub.logger.Error("Write error", zap.Error(err))
			break
		}
	}
}

func HandleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request, userID uint, logger *zap.Logger) {
	if !hub.originAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	wsUpgrader := upgrader
	wsUpgrader.CheckOrigin = hub.originAllowed
	conn, err := wsUpgrader.Upgrade(w, r, nil)
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
