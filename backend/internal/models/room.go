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
	AuthTypeNone       AuthType = "none"
	AuthTypePassword   AuthType = "password"
	AuthTypeInviteCode AuthType = "invite_code"
)

// RoomMember 房间成员。
// IsVideoOn 不能带 gorm default 标签：GORM 会把零值 false 从 INSERT 里省掉、落回列默认值，
// 入会时写的 IsVideoOn=false 就变成了 true（所有成员一进房都"摄像头已开"）。
type RoomMember struct {
	ID        uint       `json:"id" gorm:"primaryKey"`
	RoomID    uint       `json:"room_id"`
	UserID    uint       `json:"user_id"`
	Role      Role       `json:"role" gorm:"default:member"`
	IsMuted   bool       `json:"is_muted" gorm:"default:false"`
	IsVideoOn bool       `json:"is_video_on"`
	IsAudioOn bool       `json:"is_audio_on" gorm:"default:true"`
	JoinedAt  time.Time  `json:"joined_at"`
	LeftAt    *time.Time `json:"left_at"`
}

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Permission 细粒度权限枚举
type Permission string

const (
	PermKickMember       Permission = "kick_member"
	PermMuteMember       Permission = "mute_member"
	PermSetAdmin         Permission = "set_admin"
	PermLockRoom         Permission = "lock_room"
	PermDeleteRoom       Permission = "delete_room"
	PermTransferOwner    Permission = "transfer_owner"
	PermSendMessage      Permission = "send_message"
	PermUpdateMediaState Permission = "update_media_state"
	PermViewRoomInfo     Permission = "view_room_info"
	PermViewMemberList   Permission = "view_member_list"
)

// 角色权限矩阵 - 定义每个角色拥有的权限
var RolePermissions = map[Role][]Permission{
	RoleMember: {
		PermSendMessage,
		PermUpdateMediaState,
		PermViewRoomInfo,
		PermViewMemberList,
	},
	RoleAdmin: {
		PermKickMember,
		PermMuteMember,
		PermSendMessage,
		PermUpdateMediaState,
		PermViewRoomInfo,
		PermViewMemberList,
	},
	RoleOwner: {
		PermKickMember,
		PermMuteMember,
		PermSetAdmin,
		PermLockRoom,
		PermDeleteRoom,
		PermTransferOwner,
		PermSendMessage,
		PermUpdateMediaState,
		PermViewRoomInfo,
		PermViewMemberList,
	},
}

// HasPermission 检查角色是否拥有指定权限
func (r Role) HasPermission(perm Permission) bool {
	perms, ok := RolePermissions[r]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

type CreateRoomRequest struct {
	Name        string   `json:"name" binding:"required,min=2,max=20"`
	Topic       string   `json:"topic" binding:"max=50"`
	MaxMembers  int      `json:"max_members" binding:"min=1,max=500"`
	IsPermanent bool     `json:"is_permanent"`
	AuthType    AuthType `json:"auth_type"`
	Password    string   `json:"password" binding:"max=12"`
}

type JoinRoomRequest struct {
	VerifyCode string `json:"verify_code"`
}

type RoomResponse struct {
	Room    *Room             `json:"room"`
	Members []*RoomMemberInfo `json:"members"`
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
	ID        uint      `json:"id" gorm:"primaryKey"`
	RoomID    uint      `json:"room_id"`
	UserID    uint      `json:"user_id"`
	KickedAt  time.Time `json:"kicked_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason" gorm:"size:200"`
}

// JoinRoomResponse 加入房间响应
type JoinRoomResponse struct {
	Room        *Room             `json:"room"`
	Members     []*RoomMemberInfo `json:"members"`
	Permissions []Permission      `json:"my_permissions"`
	SFUToken    string            `json:"sfu_token"`
}

// AuditLog 房间操作审计日志
type AuditLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	RoomID    uint      `json:"room_id" gorm:"index"`
	UserID    uint      `json:"user_id"`
	TargetID  *uint     `json:"target_id"`
	Action    string    `json:"action" gorm:"size:50"`
	Details   string    `json:"details" gorm:"size:500"`
	IPAddress string    `json:"ip_address" gorm:"size:50"`
	UserAgent string    `json:"user_agent" gorm:"size:255"`
	CreatedAt time.Time `json:"created_at"`
}
