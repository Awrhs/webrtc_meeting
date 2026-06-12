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
	userID := GetUserID(c)
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

	if _, err := h.roomService.GetMemberRole(room.ID, userID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
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

	c.JSON(http.StatusOK, gin.H{"room": gin.H{
		"id":           room.ID,
		"room_id":      room.RoomID,
		"name":         room.Name,
		"topic":        room.Topic,
		"max_members":  room.MaxMembers,
		"current_size": room.CurrentSize,
		"is_locked":    room.IsLocked,
		"auth_type":    room.AuthType,
	}})
}

func (h *Handler) JoinRoom(c *gin.Context) {
	userID := GetUserID(c)

	// roomID 从 URL 路径参数获取（roomService 期望 string 类型）
	roomIDStr := c.Param("id")

	var req models.JoinRoomRequest
	_ = c.ShouldBindJSON(&req) // verify_code 可选，绑定失败也没关系

	result, err := h.roomService.JoinRoom(userID, roomIDStr, req.VerifyCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastUserJoined(result.Room.ID, userID)

	c.JSON(http.StatusOK, gin.H{
		"room":           result.Room,
		"members":        result.Members,
		"my_permissions": result.Permissions,
		"sfu_token":      result.SFUToken,
	})
}

func (h *Handler) GetSFUToken(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	token, err := h.roomService.GenerateSFUToken(uint(roomID), userID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"sfu_token": token})
}

func (h *Handler) DeleteRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	err = h.roomService.DeleteRoom(uint(id), userID)
	if err != nil {
		h.logger.Warn("Failed to delete room", zap.Error(err), zap.Uint("roomID", uint(id)), zap.Uint("userID", userID))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, uint(id), userID, nil, "delete_room", "")

	c.JSON(http.StatusOK, gin.H{"message": "room deleted"})
}

func (h *Handler) LockRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	err = h.roomService.LockRoom(uint(id), userID, true)
	if err != nil {
		h.logger.Warn("Failed to lock room", zap.Error(err), zap.Uint("roomID", uint(id)), zap.Uint("userID", userID))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, uint(id), userID, nil, "lock_room", "")

	c.JSON(http.StatusOK, gin.H{"message": "room locked", "is_locked": true})
}

func (h *Handler) UnlockRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	err = h.roomService.LockRoom(uint(id), userID, false)
	if err != nil {
		h.logger.Warn("Failed to unlock room", zap.Error(err), zap.Uint("roomID", uint(id)), zap.Uint("userID", userID))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.audit(c, uint(id), userID, nil, "unlock_room", "")

	c.JSON(http.StatusOK, gin.H{"message": "room unlocked", "is_locked": false})
}

func (h *Handler) KickMember(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	targetID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.roomService.KickMember(uint(roomID), userID, uint(targetID))
	if err != nil {
		h.logger.Warn("Failed to kick member", zap.Error(err), zap.Uint("roomID", uint(roomID)), zap.Uint("userID", userID), zap.Uint("targetID", uint(targetID)))
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

	tID := uint(targetID)
	h.audit(c, uint(roomID), userID, &tID, "kick", "")

	c.JSON(http.StatusOK, gin.H{"message": "member kicked"})
}

func (h *Handler) MuteMember(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	targetID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.roomService.MuteMember(uint(roomID), userID, uint(targetID), true)
	if err != nil {
		h.logger.Warn("Failed to mute member", zap.Error(err), zap.Uint("roomID", uint(roomID)), zap.Uint("userID", userID), zap.Uint("targetID", uint(targetID)))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	// Call SFU to mute the user's media stream
	if h.sfuClient != nil {
		if err := h.sfuClient.MuteUser(uint(roomID), uint(targetID), true); err != nil {
			h.logger.Warn("Failed to mute user in SFU, continuing with signaling", zap.Error(err))
		}
	}

	h.hub.BroadcastMute(uint(roomID), uint(targetID), true)

	tID := uint(targetID)
	h.audit(c, uint(roomID), userID, &tID, "mute", "")

	c.JSON(http.StatusOK, gin.H{"message": "member muted", "is_muted": true})
}

func (h *Handler) UnmuteMember(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	targetID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.roomService.MuteMember(uint(roomID), userID, uint(targetID), false)
	if err != nil {
		h.logger.Warn("Failed to unmute member", zap.Error(err), zap.Uint("roomID", uint(roomID)), zap.Uint("userID", userID), zap.Uint("targetID", uint(targetID)))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	// Call SFU to unmute the user's media stream
	if h.sfuClient != nil {
		if err := h.sfuClient.MuteUser(uint(roomID), uint(targetID), false); err != nil {
			h.logger.Warn("Failed to unmute user in SFU, continuing with signaling", zap.Error(err))
		}
	}

	h.hub.BroadcastMute(uint(roomID), uint(targetID), false)

	tID := uint(targetID)
	h.audit(c, uint(roomID), userID, &tID, "unmute", "")

	c.JSON(http.StatusOK, gin.H{"message": "member unmuted", "is_muted": false})
}

func (h *Handler) SetAdmin(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	targetID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.roomService.SetAdmin(uint(roomID), userID, uint(targetID), true)
	if err != nil {
		h.logger.Warn("Failed to set admin", zap.Error(err), zap.Uint("roomID", uint(roomID)), zap.Uint("userID", userID), zap.Uint("targetID", uint(targetID)))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastRoleChanged(uint(roomID), uint(targetID), models.RoleAdmin)

	tID := uint(targetID)
	h.audit(c, uint(roomID), userID, &tID, "set_admin", "")

	c.JSON(http.StatusOK, gin.H{"message": "admin set", "role": models.RoleAdmin})
}

func (h *Handler) RemoveAdmin(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	targetID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.roomService.SetAdmin(uint(roomID), userID, uint(targetID), false)
	if err != nil {
		h.logger.Warn("Failed to remove admin", zap.Error(err), zap.Uint("roomID", uint(roomID)), zap.Uint("userID", userID), zap.Uint("targetID", uint(targetID)))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastRoleChanged(uint(roomID), uint(targetID), models.RoleMember)

	tID := uint(targetID)
	h.audit(c, uint(roomID), userID, &tID, "remove_admin", "")

	c.JSON(http.StatusOK, gin.H{"message": "admin removed", "role": models.RoleMember})
}

func (h *Handler) TransferOwner(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	targetID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	err = h.roomService.TransferOwner(uint(roomID), userID, uint(targetID))
	if err != nil {
		h.logger.Warn("Failed to transfer owner", zap.Error(err), zap.Uint("roomID", uint(roomID)), zap.Uint("userID", userID), zap.Uint("targetID", uint(targetID)))
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastRoleChanged(uint(roomID), userID, models.RoleMember)
	h.hub.BroadcastRoleChanged(uint(roomID), uint(targetID), models.RoleOwner)

	tID := uint(targetID)
	h.audit(c, uint(roomID), userID, &tID, "transfer_owner", "")

	c.JSON(http.StatusOK, gin.H{"message": "ownership transferred"})
}

func (h *Handler) NotifyConnectionFailed(c *gin.Context) {
	if h.sfuCallbackToken != "" && c.GetHeader("X-SFU-Callback-Token") != h.sfuCallbackToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	idStr := c.Param("roomId")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	userIdStr := c.Param("userId")
	userID, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	h.hub.SendConnectionFailed(uint(roomID), uint(userID))
	c.JSON(http.StatusOK, gin.H{"message": "notification sent"})
}

func (h *Handler) UpdateMediaState(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	var req struct {
		VideoEnabled *bool `json:"video_enabled"`
		AudioEnabled *bool `json:"audio_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.VideoEnabled == nil && req.AudioEnabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing media state"})
		return
	}

	if _, err := h.roomService.GetMemberRole(uint(roomID), userID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}
	videoEnabled, audioEnabled, err := h.roomService.UpdateMediaState(uint(roomID), userID, req.VideoEnabled, req.AudioEnabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastMediaState(uint(roomID), userID, videoEnabled, audioEnabled)
	c.JSON(http.StatusOK, gin.H{"message": "media state updated", "video_enabled": videoEnabled, "audio_enabled": audioEnabled})
}
