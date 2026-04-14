package models

import (
	"time"

	"gorm.io/gorm"
)

type Message struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	RoomID      uint           `json:"room_id"`
	SenderID    uint           `json:"sender_id"`
	ReceiverID  *uint          `json:"receiver_id"`
	Type        MessageType    `json:"type" gorm:"default:text"`
	Content     string         `json:"content"`
	Encrypted   bool           `json:"encrypted" gorm:"default:true"`
	CreatedAt   time.Time      `json:"created_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type MessageType string

const (
	MessageTypeText  MessageType = "text"
	MessageTypeImage MessageType = "image"
	MessageTypeFile  MessageType = "file"
	MessageTypeSystem MessageType = "system"
)

type SendMessageRequest struct {
	RoomID     uint        `json:"room_id" binding:"required"`
	ReceiverID *uint       `json:"receiver_id"`
	Type       MessageType `json:"type"`
	Content    string      `json:"content" binding:"required"`
}

type MessageResponse struct {
	ID         uint           `json:"id"`
	RoomID     uint           `json:"room_id"`
	SenderID   uint           `json:"sender_id"`
	SenderNickname string     `json:"sender_nickname"`
	SenderAvatar string      `json:"sender_avatar"`
	ReceiverID *uint          `json:"receiver_id"`
	Type       MessageType    `json:"type"`
	Content    string         `json:"content"`
	Encrypted  bool           `json:"encrypted"`
	CreatedAt  time.Time      `json:"created_at"`
}
