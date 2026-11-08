package handlers

import (
	"conference-sfu/backend/internal/auth"
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/service"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/internal/signaling"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Handler struct {
	userService      service.UserService
	userRepo         repository.UserRepository
	roomService      service.RoomService
	chatService      service.ChatService
	hub              *signaling.Hub
	sfuRouter        *sfu.ControlRouter
	logger           *zap.Logger
	sfuCallbackToken string
	// sfuHTTPURL：API 访问 SFU HTTP 接口（录制列表/下载代理）的内网地址
	sfuHTTPURL     string
	uploadDir      string
	uploadMaxBytes int64
	// ice：下发给浏览器的 STUN/TURN 配置（见 ice.go）
	ice ICEConfig
}

func NewHandler(userService service.UserService, userRepo repository.UserRepository, roomService service.RoomService, chatService service.ChatService, hub *signaling.Hub, sfuRouter *sfu.ControlRouter, sfuCallbackToken string, sfuHTTPURL string, uploadDir string, uploadMaxBytes int64, logger *zap.Logger) *Handler {
	return &Handler{
		userService:      userService,
		userRepo:         userRepo,
		roomService:      roomService,
		chatService:      chatService,
		hub:              hub,
		sfuRouter:        sfuRouter,
		logger:           logger,
		sfuCallbackToken: sfuCallbackToken,
		sfuHTTPURL:       sfuHTTPURL,
		uploadDir:        uploadDir,
		uploadMaxBytes:   uploadMaxBytes,
	}
}

func userPayload(user *models.User) gin.H {
	return gin.H{
		"id":             user.ID,
		"username":       user.Username,
		"nickname":       user.Nickname,
		"email":          user.Email,
		"phone":          user.Phone,
		"avatar":         user.Avatar,
		"email_verified": user.EmailVerified,
	}
}

func (h *Handler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, verificationRequired, err := h.userService.Register(req.Username, req.Email, req.Phone, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":               "registration successful",
		"verification_required": verificationRequired,
		"user":                  userPayload(user),
	})
}

func (h *Handler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	device := deviceInfoFromRequest(c)
	token, user, err := h.userService.Login(req.Account, req.Password, device)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 登录成功后记录设备（best-effort，失败不阻断）
	h.recordLoginDevice(user.ID, device)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  userPayload(user),
	})
}

// Logout 吊销当前会话：这个 token 立即失效，其它设备不受影响。
func (h *Handler) Logout(c *gin.Context) {
	if err := h.userService.Logout(GetUserID(c), GetJTI(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// deviceInfoFromRequest 从请求头提取设备指纹（X-Device-Id，缺省用 UA 哈希）、UA、IP。
func deviceInfoFromRequest(c *gin.Context) service.DeviceInfo {
	ua := c.GetHeader("User-Agent")
	deviceID := c.GetHeader("X-Device-Id")
	if deviceID == "" {
		sum := sha256.Sum256([]byte(ua))
		deviceID = hex.EncodeToString(sum[:8])
	}
	return service.DeviceInfo{
		ID:        truncateString(deviceID, 100),
		UserAgent: truncateString(ua, 255),
		IP:        c.ClientIP(),
	}
}

func (h *Handler) recordLoginDevice(userID uint, device service.DeviceInfo) {
	if h.userRepo == nil {
		return
	}
	record := &models.Device{
		UserID:     userID,
		DeviceID:   device.ID,
		UserAgent:  device.UserAgent,
		IP:         device.IP,
		LastActive: time.Now(),
	}
	if err := h.userRepo.UpsertDevice(record); err != nil {
		h.logger.Warn("UpsertDevice failed", zap.Uint("userID", userID), zap.Error(err))
	}
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (h *Handler) GetMyDevices(c *gin.Context) {
	userID := GetUserID(c)
	devices, err := h.userRepo.GetDevicesByUserID(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": devices, "current_device_id": GetDeviceID(c)})
}

// DeleteMyDevice 删除设备记录并吊销该设备上的全部会话（真正把那台设备踢下线）。
func (h *Handler) DeleteMyDevice(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	deviceID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	if err := h.userService.RevokeDevice(userID, uint(deviceID)); err != nil {
		if err.Error() == "device not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "device removed and its sessions revoked"})
}

func (h *Handler) GetMe(c *gin.Context) {
	userID := GetUserID(c)
	user, err := h.userService.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	payload := userPayload(user)
	payload["status"] = user.Status
	c.JSON(http.StatusOK, gin.H{"user": payload})
}

func (h *Handler) UpdateProfile(c *gin.Context) {
	userID := GetUserID(c)
	var req models.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.userService.UpdateProfile(userID, req.Nickname, req.Avatar)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "profile updated"})
}

// UpdatePassword 修改密码：所有旧 token 失效，响应里带上为当前设备签发的新 token。
func (h *Handler) UpdatePassword(c *gin.Context) {
	userID := GetUserID(c)
	var req models.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, err := h.userService.UpdatePassword(userID, req.OldPassword, req.NewPassword, deviceInfoFromRequest(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password updated", "token": token})
}

// ForgotPassword 申请重置密码验证码。无论账号是否存在都返回同样的提示，避免账号枚举。
func (h *Handler) ForgotPassword(c *gin.Context) {
	var req models.ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.userService.RequestPasswordReset(req.Account); err != nil {
		h.logger.Warn("RequestPasswordReset failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send reset code, please try again later"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "if the account exists, a reset code has been sent to its email"})
}

// ResetPassword 用邮件验证码设置新密码。
func (h *Handler) ResetPassword(c *gin.Context) {
	var req models.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.userService.ResetPassword(req.Account, req.Code, req.NewPassword); err != nil {
		if errors.Is(err, service.ErrInvalidCode) || errors.Is(err, service.ErrTooManyAttempts) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "password reset, please login with the new password"})
}

// VerifyEmail 用邮件验证码确认邮箱。
func (h *Handler) VerifyEmail(c *gin.Context) {
	var req models.VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.userService.VerifyEmail(req.Email, req.Code); err != nil {
		if errors.Is(err, service.ErrInvalidCode) || errors.Is(err, service.ErrTooManyAttempts) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "email verified"})
}

// ResendVerification 重新发送邮箱验证码（不泄露邮箱是否已注册）。
func (h *Handler) ResendVerification(c *gin.Context) {
	var req models.ResendVerificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.userService.SendEmailVerification(req.Email); err != nil {
		h.logger.Warn("SendEmailVerification failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send verification code, please try again later"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "if the email is registered and unverified, a code has been sent"})
}

// AuthMiddleware 校验 Bearer token：签名/过期 → 会话仍有效 → token 版本未被改密码作废。
func AuthMiddleware(validator *auth.Validator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		claims, err := validator.Validate(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("jti", claims.JTI)
		c.Set("device_id", claims.DeviceID)
		c.Next()
	}
}

func GetUserID(c *gin.Context) uint {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	return userID.(uint)
}

// GetJTI 返回当前请求的会话 ID（由 AuthMiddleware 写入）。
func GetJTI(c *gin.Context) string {
	jti, _ := c.Get("jti")
	s, _ := jti.(string)
	return s
}

// GetDeviceID 返回当前请求的设备指纹（由 AuthMiddleware 写入）。
func GetDeviceID(c *gin.Context) string {
	dev, _ := c.Get("device_id")
	s, _ := dev.(string)
	return s
}
