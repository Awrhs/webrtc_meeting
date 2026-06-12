package handlers

import (
	"conference-sfu/backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *Handler) audit(c *gin.Context, roomID, userID uint, targetID *uint, action, details string) {
	if err := h.roomService.LogAudit(roomID, userID, targetID, action, details, c.ClientIP(), c.GetHeader("User-Agent")); err != nil {
		h.logger.Warn("LogAudit failed",
			zap.Error(err),
			zap.Uint("roomID", roomID),
			zap.Uint("userID", userID),
			zap.String("action", action),
		)
	}
}

func (h *Handler) GetAuditLogs(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	role, err := h.roomService.GetMemberRole(uint(roomID), userID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}
	if role != models.RoleOwner && role != models.RoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	logs, err := h.roomService.GetAuditLogs(uint(roomID), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"audit_logs": logs})
}
