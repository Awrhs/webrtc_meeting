package handlers

import (
	"conference-sfu/backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *Handler) CreateRoom(c *gin.Context) {
	userID := GetUserID(c)
	var req models.CreateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	room, err := h.roomService.CreateRoom(userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"room": room})
}

func (h *Handler) GetMyRooms(c *gin.Context) {
	userID := GetUserID(c)
	rooms, err := h.roomService.GetMyRooms(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"rooms": rooms})
}

func (h *Handler) GetRoom(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	room, err := h.roomService.GetRoomByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
		return
	}

	members, err := h.roomService.GetRoomMembersInfo(room.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"room":    room,
		"members": members,
	})
}

func (h *Handler) GetRoomByNumber(c *gin.Context) {
	roomNumber := c.Param("number")

	room, err := h.roomService.GetRoomByRoomID(roomNumber)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"room": room})
}

func (h *Handler) JoinRoom(c *gin.Context) {
	userID := GetUserID(c)

	// roomID 从 URL 路径参数获取（roomService 期望 string 类型）
	roomIDStr := c.Param("id")

	var req models.JoinRoomRequest
	_ = c.ShouldBindJSON(&req) // verify_code 可选，绑定失败也没关系

	room, members, err := h.roomService.JoinRoom(userID, roomIDStr, req.VerifyCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastUserJoined(room.ID, userID)

	c.JSON(http.StatusOK, gin.H{
		"room":    room,
		"members": members,
	})
}

func (h *Handler) DeleteRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)

	err := h.roomService.DeleteRoom(uint(id), userID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "room deleted"})
}

func (h *Handler) LockRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)

	err := h.roomService.LockRoom(uint(id), userID, true)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "room locked", "is_locked": true})
}

func (h *Handler) UnlockRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)

	err := h.roomService.LockRoom(uint(id), userID, false)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "room unlocked", "is_locked": false})
}

func (h *Handler) KickMember(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	targetID, _ := strconv.Atoi(userIdStr)

	err := h.roomService.KickMember(uint(roomID), userID, uint(targetID))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	// Call SFU to force disconnect the user's media connection
	if h.sfuClient != nil {
		if err := h.sfuClient.KickUser(uint(roomID), uint(targetID), "kicked by admin"); err != nil {
			h.logger.Warn("Failed to kick user from SFU, continuing with signaling", zap.Error(err))
		}
	}

	h.hub.BroadcastKick(uint(roomID), uint(targetID))

	c.JSON(http.StatusOK, gin.H{"message": "member kicked"})
}

func (h *Handler) MuteMember(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	targetID, _ := strconv.Atoi(userIdStr)

	err := h.roomService.MuteMember(uint(roomID), userID, uint(targetID), true)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastMute(uint(roomID), uint(targetID), true)

	c.JSON(http.StatusOK, gin.H{"message": "member muted", "is_muted": true})
}

func (h *Handler) UnmuteMember(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	targetID, _ := strconv.Atoi(userIdStr)

	err := h.roomService.MuteMember(uint(roomID), userID, uint(targetID), false)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastMute(uint(roomID), uint(targetID), false)

	c.JSON(http.StatusOK, gin.H{"message": "member unmuted", "is_muted": false})
}

func (h *Handler) SetAdmin(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	targetID, _ := strconv.Atoi(userIdStr)

	err := h.roomService.SetAdmin(uint(roomID), userID, uint(targetID), true)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastRoleChanged(uint(roomID), uint(targetID), models.RoleAdmin)

	c.JSON(http.StatusOK, gin.H{"message": "admin set", "role": models.RoleAdmin})
}

func (h *Handler) RemoveAdmin(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	targetID, _ := strconv.Atoi(userIdStr)

	err := h.roomService.SetAdmin(uint(roomID), userID, uint(targetID), false)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastRoleChanged(uint(roomID), uint(targetID), models.RoleMember)

	c.JSON(http.StatusOK, gin.H{"message": "admin removed", "role": models.RoleMember})
}

func (h *Handler) TransferOwner(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	targetID, _ := strconv.Atoi(userIdStr)

	err := h.roomService.TransferOwner(uint(roomID), userID, uint(targetID))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastRoleChanged(uint(roomID), userID, models.RoleMember)
	h.hub.BroadcastRoleChanged(uint(roomID), uint(targetID), models.RoleOwner)

	c.JSON(http.StatusOK, gin.H{"message": "ownership transferred"})
}

func (h *Handler) NotifyConnectionFailed(c *gin.Context) {
	idStr := c.Param("roomId")
	roomID, _ := strconv.Atoi(idStr)
	userIdStr := c.Param("userId")
	userID, _ := strconv.Atoi(userIdStr)

	h.hub.SendConnectionFailed(uint(roomID), uint(userID))
	c.JSON(http.StatusOK, gin.H{"message": "notification sent"})
}

func (h *Handler) UpdateMediaState(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, _ := strconv.Atoi(idStr)

	var req struct {
		VideoEnabled bool `json:"video_enabled"`
		AudioEnabled bool `json:"audio_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastMediaState(uint(roomID), userID, req.VideoEnabled, req.AudioEnabled)
	c.JSON(http.StatusOK, gin.H{"message": "media state updated"})
}