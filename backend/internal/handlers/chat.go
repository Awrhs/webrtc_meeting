package handlers

import (
	"conference-sfu/backend/internal/models"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (h *Handler) GetMessages(c *gin.Context) {
	userID := GetUserID(c)
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

	messages, err := h.chatService.GetMessages(userID, uint(roomID), limit, offset)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
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

	var req models.SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message, err := h.chatService.SendMessage(senderID, uint(roomID), req.ReceiverID, req.Type, req.Content, req.Encrypted)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	h.hub.BroadcastMessage(uint(roomID), message, req.ReceiverID)

	c.JSON(http.StatusOK, gin.H{"message": message})
}

// GetPrivateMessages 查询与某成员的私聊记录（双向）。
func (h *Handler) GetPrivateMessages(c *gin.Context) {
	requesterID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}
	otherIDStr := c.Param("userId")
	otherID, err := strconv.Atoi(otherIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	messages, err := h.chatService.GetPrivateMessages(uint(roomID), requesterID, uint(otherID), limit, offset)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

// UploadFile 上传图片/文件到房间（multipart），返回可访问的相对 URL。
// 消息本身仍通过 SendMessage 发送：type=image/file，content 为 JSON{url,name,size}。
func (h *Handler) UploadFile(c *gin.Context) {
	userID := GetUserID(c)
	idStr := c.Param("id")
	roomID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	// 成员才能上传
	if _, err := h.roomService.GetMemberRole(uint(roomID), userID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	if err := c.Request.ParseMultipartForm(h.uploadMaxBytes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large or invalid form"})
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file field required"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedUploadExts[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file type not allowed: " + ext})
		return
	}
	if header.Size > h.uploadMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large"})
		return
	}

	// 安全文件名：时间戳 + 随机后缀，保留原始扩展名
	safeExt := strings.TrimPrefix(ext, ".")
	name := fmt.Sprintf("%d_%s.%s", time.Now().UnixNano(), randHex(8), safeExt)
	if err := os.MkdirAll(h.uploadDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
		return
	}
	out, err := os.Create(filepath.Join(h.uploadDir, name))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage unavailable"})
		return
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(file, h.uploadMaxBytes+1))
	if err != nil || written > h.uploadMaxBytes {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store file"})
		return
	}

	isImage := imageExts[ext]
	msgType := models.MessageTypeFile
	if isImage {
		msgType = models.MessageTypeImage
	}

	c.JSON(http.StatusOK, gin.H{
		"url":  "/uploads/" + name,
		"name": filepath.Base(header.Filename),
		"size": written,
		"type": msgType,
	})
}

var allowedUploadExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
	".pdf": true, ".txt": true, ".md": true, ".zip": true, ".csv": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".mp4": true, ".webm": true, ".mov": true, ".mp3": true, ".wav": true, ".ogg": true,
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
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
