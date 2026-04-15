package handlers

import (
	"conference-sfu/backend/internal/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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

func (h *Handler) JoinRoom(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	var req models.JoinRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	room, members, err := h.roomService.JoinRoom(userID, req.RoomID, req.VerifyCode)
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