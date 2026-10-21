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
	uploadDir        string
	uploadMaxBytes   int64
}

func NewHandler(userService service.UserService, userRepo repository.UserRepository, roomService service.RoomService, chatService service.ChatService, hub *signaling.Hub, sfuRouter *sfu.ControlRouter, sfuCallbackToken string, uploadDir string, uploadMaxBytes int64, logger *zap.Logger) *Handler {
	return &Handler{
		userService:      userService,
		userRepo:         userRepo,
		roomService:       roomService,
		chatService:       chatService,
		hub:               hub,
		sfuRouter:         sfuRouter,
		logger:            logger,
		sfuCallbackToken:  sfuCallbackToken,
		uploadDir:         uploadDir,
		uploadMaxBytes:    uploadMaxBytes,
	}
}

func (h *Handler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userService.Register(req.Username, req.Email, req.Phone, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "registration successful",
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"nickname": user.Nickname,
			"email":    user.Email,
			"phone":    user.Phone,
			"avatar":   user.Avatar,
		},
	})
}

func (h *Handler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, user, err := h.userService.Login(req.Account, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 登录成功后记录设备（best-effort，失败不阻断）
	h.recordLoginDevice(c, user.ID)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"nickname": user.Nickname,
			"email":    user.Email,
			"phone":    user.Phone,
			"avatar":   user.Avatar,
		},
	})
}

func (h *Handler) recordLoginDevice(c *gin.Context, userID uint) {
	if h.userRepo == nil {
		return
	}
	ua := c.GetHeader("User-Agent")
	deviceID := c.GetHeader("X-Device-Id")
	if deviceID == "" {
		// 用 UA 哈希作为设备指纹
		sum := sha256.Sum256([]byte(ua))
		deviceID = hex.EncodeToString(sum[:8])
	}
	device := &models.Device{
		UserID:     userID,
		DeviceID:   deviceID,
		UserAgent:  truncateString(ua, 255),
		IP:         c.ClientIP(),
		LastActive: time.Now(),
	}
	if err := h.userRepo.UpsertDevice(device); err != nil {
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
	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

func (h *Handler) DeleteMyDevice(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	deviceID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	if err := h.userRepo.DeleteDevice(uint(deviceID), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "device removed"})
}

func (h *Handler) GetMe(c *gin.Context) {
	userID := GetUserID(c)
	user, err := h.userService.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"nickname": user.Nickname,
			"email":    user.Email,
			"phone":    user.Phone,
			"avatar":   user.Avatar,
			"status":   user.Status,
		},
	})
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

func (h *Handler) UpdatePassword(c *gin.Context) {
	userID := GetUserID(c)
	var req models.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.userService.UpdatePassword(userID, req.OldPassword, req.NewPassword)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password updated"})
}

func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
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

		tokenStr := parts[1]
		userID, err := ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		c.Set("user_id", userID)
		c.Next()
	}
}

func ValidateToken(tokenStr string, jwtSecret string) (uint, error) {
	return auth.ValidateUserToken(tokenStr, jwtSecret)
}

func GetUserID(c *gin.Context) uint {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	return userID.(uint)
}
