package models

import (
	"time"

	"gorm.io/gorm"
)

type Message struct {
	ID         uint           `json:"id" gorm:"primaryKey"`
	RoomID     uint           `json:"room_id"`
	SenderID   uint           `json:"sender_id"`
	ReceiverID *uint          `json:"receiver_id"`
	Type       MessageType    `json:"type" gorm:"default:text"`
	Content    string         `json:"content"`
	Encrypted  bool           `json:"encrypted" gorm:"default:true"`
	CreatedAt  time.Time      `json:"created_at"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"index"`
}

type MessageType string

const (
	MessageTypeText   MessageType = "text"
	MessageTypeImage  MessageType = "image"
	MessageTypeFile   MessageType = "file"
	MessageTypeSystem MessageType = "system"
)

type SendMessageRequest struct {
	RoomID     uint        `json:"room_id"`
	ReceiverID *uint       `json:"receiver_id"`
	Type       MessageType `json:"type"`
	Content    string      `json:"content" binding:"required"`
	Encrypted  bool        `json:"encrypted"`
}

type MessageResponse struct {
	ID             uint        `json:"id"`
	RoomID         uint        `json:"room_id"`
	SenderID       uint        `json:"sender_id"`
	SenderNickname string      `json:"sender_nickname"`
	SenderAvatar   string      `json:"sender_avatar"`
	ReceiverID     *uint       `json:"receiver_id"`
	Type           MessageType `json:"type"`
	Content        string      `json:"content"`
	Encrypted      bool        `json:"encrypted"`
	CreatedAt      time.Time   `json:"created_at"`
}

// E2EEPublicKey 端到端加密公钥存储
type E2EEPublicKey struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"uniqueIndex;not null"`
	PublicKey string    `json:"public_key" gorm:"type:text;not null"`
	KeyType   string    `json:"key_type" gorm:"size:20;default:ecdh_p256"` // ecdh_p256、bundle_v2（ECDH+ECDSA 公钥包）等
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 显式指定表名。GORM 的默认命名会把 E2EEPublicKey 变成 e2_ee_public_keys，
// 与迁移脚本里的 e2ee_public_keys 不一致，导致公钥接口一直报"relation does not exist"。
func (E2EEPublicKey) TableName() string {
	return "e2ee_public_keys"
}

// E2EEKeyExchangeRequest 密钥交换请求
type E2EEKeyExchangeRequest struct {
	ReceiverID   uint   `json:"receiver_id" binding:"required"`
	RoomID       uint   `json:"room_id" binding:"required"`
	EphemeralKey string `json:"ephemeral_key" binding:"required"` // 临时公钥（用于密钥交换用）
}

// E2EEPublicKeyUploadRequest 上传公钥请求
type E2EEPublicKeyUploadRequest struct {
	PublicKey string `json:"public_key" binding:"required"`
	KeyType   string `json:"key_type"`
}

// E2EEGetPublicKeyResponse 获取公钥响应
type E2EEGetPublicKeyResponse struct {
	UserID    uint   `json:"user_id"`
	PublicKey string `json:"public_key"`
	KeyType   string `json:"key_type"`
	Exists    bool   `json:"exists"`
}
