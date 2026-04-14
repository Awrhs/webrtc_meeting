package handlers

import (
	"conference-sfu/backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetMessages(c *gin.Context) {
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

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
	roomID, _ := strconv.Atoi(idStr)

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