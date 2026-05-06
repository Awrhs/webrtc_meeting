package handlers

import (
	"conference-sfu/backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetMessages(c *gin.Context) {
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 50
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		offset = 0
	}

	messages, err := h.chatService.GetMessages(uint(roomID), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

func (h *Handler) SendMessage(c *gin.Context) {
	senderID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	var req struct {
		ReceiverID *uint               `json:"receiver_id"`
		Type       models.MessageType  `json:"type"`
		Content    string              `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message, err := h.chatService.SendMessage(senderID, uint(roomID), req.ReceiverID, req.Type, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastMessage(uint(roomID), message, req.ReceiverID)

	c.JSON(http.StatusOK, gin.H{"message": message})
}

// UploadPublicKey 上传用户的端到端加密公钥
func (h *Handler) UploadPublicKey(c *gin.Context) {
	userID := GetUserID(c)

	var req models.E2EEPublicKeyUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.chatService.SavePublicKey(userID, req.PublicKey, req.KeyType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "public key uploaded successfully"})
}

// GetPublicKey 获取指定用户的公钥
func (h *Handler) GetPublicKey(c *gin.Context) {
	userIDStr := c.Param("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	result, err := h.chatService.GetPublicKey(uint(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeletePublicKey 删除当前用户的公钥
func (h *Handler) DeletePublicKey(c *gin.Context) {
	userID := GetUserID(c)

	if err := h.chatService.DeletePublicKey(userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "public key deleted successfully"})
}