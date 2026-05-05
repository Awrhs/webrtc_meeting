package models

import (
	"time"

	"gorm.io/gorm"
)

type Room struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	RoomID      string         `json:"room_id" gorm:"size:6;uniqueIndex;not null"`
	Name        string         `json:"name" gorm:"size:50;not null"`
	Topic       string         `json:"topic" gorm:"size:100"`
	OwnerID     uint           `json:"owner_id" gorm:"not null"`
	MaxMembers  int            `json:"max_members" gorm:"default:50"`
	IsPermanent bool           `json:"is_permanent" gorm:"default:false"`
	IsLocked    bool           `json:"is_locked" gorm:"default:false"`
	AuthType    AuthType       `json:"auth_type" gorm:"default:none"`
	Password    string         `json:"-" gorm:"size:255"`
	InviteCode  string         `json:"invite_code" gorm:"size:8;uniqueIndex"`
	CurrentSize int            `json:"current_size" gorm:"default:0"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type AuthType string

const (
	AuthTypeNone    AuthType = "none"
	AuthTypePassword AuthType = "password"
	AuthTypeInviteCode AuthType = "invite_code"
)

type RoomMember struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	RoomID     uint      `json:"room_id"`
	UserID     uint      `json:"user_id"`
	Role       Role      `json:"role" gorm:"default:member"`
	IsMuted    bool      `json:"is_muted" gorm:"default:false"`
	IsVideoOn  bool      `json:"is_video_on" gorm:"default:true"`
	IsAudioOn  bool      `json:"is_audio_on" gorm:"default:true"`
	JoinedAt   time.Time `json:"joined_at"`
	LeftAt     *time.Time `json:"left_at"`
}

type Role string

const (
	RoleOwner   Role = "owner"
	RoleAdmin   Role = "admin"
	RoleMember  Role = "member"
)

type CreateRoomRequest struct {
	Name        string  `json:"name" binding:"required,min=2,max=20"`
	Topic       string  `json:"topic" binding:"max=50"`
	MaxMembers  int     `json:"max_members" binding:"min=1,max=500"`
	IsPermanent bool    `json:"is_permanent"`
	AuthType    AuthType `json:"auth_type"`
	Password    string  `json:"password" binding:"max=12"`
}

type JoinRoomRequest struct {
	VerifyCode string `json:"verify_code"`
}

type RoomResponse struct {
	Room        *Room            `json:"room"`
	Members     []*RoomMemberInfo `json:"members"`
}

type RoomMemberInfo struct {
	ID        uint   `json:"id"`
	UserID    uint   `json:"user_id"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar"`
	Role      Role   `json:"role"`
	IsMuted   bool   `json:"is_muted"`
	IsVideoOn bool   `json:"is_video_on"`
	IsAudioOn bool   `json:"is_audio_on"`
}

type KickBan struct {
	ID         uint       `json:"id" gorm:"primaryKey"`
	RoomID     uint       `json:"room_id"`
	UserID     uint       `json:"user_id"`
	KickedAt   time.Time   `json:"kicked_at"`
	ExpiresAt  time.Time   `json:"expires_at"`
	Reason     string     `json:"reason" gorm:"size:200"`
}
