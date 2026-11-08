package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Username  string         `json:"username" gorm:"size:50;uniqueIndex"`
	Email     string         `json:"email" gorm:"size:100;uniqueIndex"`
	Phone     string         `json:"phone" gorm:"size:20;uniqueIndex"`
	Password  string         `json:"-" gorm:"size:255;not null"`
	Nickname  string         `json:"nickname" gorm:"size:50"`
	Avatar    string         `json:"avatar" gorm:"size:255"`
	Status    UserStatus     `json:"status" gorm:"default:active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	// TokenVersion 登录 token 的版本号：改密码/重置密码时 +1，旧 token 全部失效。
	// 不进 JSON：既不该暴露给前端，也不该被 Redis 用户缓存携带（版本查询总是走数据库/专用缓存）。
	TokenVersion int `json:"-" gorm:"default:0"`
	// EmailVerified 注册邮箱是否已通过验证码确认
	EmailVerified bool `json:"email_verified" gorm:"default:false"`
}

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusInactive UserStatus = "inactive"
	UserStatusBanned   UserStatus = "banned"
)

type Device struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	UserID     uint      `json:"user_id" gorm:"index;uniqueIndex:idx_user_device"`
	DeviceID   string    `json:"device_id" gorm:"size:100;uniqueIndex:idx_user_device"`
	DeviceName string    `json:"device_name" gorm:"size:100"`
	DeviceType string    `json:"device_type" gorm:"size:50"`
	UserAgent  string    `json:"user_agent" gorm:"size:255"`
	IP         string    `json:"ip" gorm:"size:50"`
	LastActive time.Time `json:"last_active"`
	CreatedAt  time.Time `json:"created_at"`
}

type LoginRequest struct {
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
	Remember bool   `json:"remember"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=20"`
	Email    string `json:"email" binding:"required,email"`
	Phone    string `json:"phone" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=20"`
}

type UpdateProfileRequest struct {
	Nickname string `json:"nickname" binding:"required,min=2,max=10"`
	Avatar   string `json:"avatar"`
}

type UpdatePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=20"`
}

// ForgotPasswordRequest 申请找回密码：account 可以是用户名/邮箱/手机号
type ForgotPasswordRequest struct {
	Account string `json:"account" binding:"required"`
}

// ResetPasswordRequest 用邮件验证码重置密码
type ResetPasswordRequest struct {
	Account     string `json:"account" binding:"required"`
	Code        string `json:"code" binding:"required,len=6"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=20"`
}

// VerifyEmailRequest 用邮件验证码确认邮箱
type VerifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required,len=6"`
}

// ResendVerificationRequest 重新发送邮箱验证码
type ResendVerificationRequest struct {
	Email string `json:"email" binding:"required,email"`
}
