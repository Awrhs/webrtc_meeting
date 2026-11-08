package handlers

import (
	"conference-sfu/backend/internal/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 录制文件此前只能拿 SFU 的内部服务 token 直接从 SFU 下载，且不区分房间。
// 现在由 API 代理：校验调用者是该房间的房主/管理员，只暴露属于该房间的录制目录。

var recordingNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type recordingEntry struct {
	Dir       string   `json:"dir"`
	StartedAt string   `json:"started_at,omitempty"`
	Files     []string `json:"files"`
}

type sfuRecordingsList struct {
	Enabled bool `json:"enabled"`
	Rooms   []struct {
		Dir   string   `json:"dir"`
		Files []string `json:"files"`
	} `json:"rooms"`
}

func (h *Handler) requireRoomManager(c *gin.Context) (uint, bool) {
	userID := GetUserID(c)
	roomID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return 0, false
	}
	role, err := h.roomService.GetMemberRole(uint(roomID), userID)
	if err != nil || (role != models.RoleOwner && role != models.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return 0, false
	}
	return uint(roomID), true
}

func (h *Handler) sfuRequest(method, path string, query url.Values) (*http.Request, error) {
	target := strings.TrimRight(h.sfuHTTPURL, "/") + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		return nil, err
	}
	if h.sfuCallbackToken != "" {
		req.Header.Set("X-SFU-Callback-Token", h.sfuCallbackToken)
	}
	return req, nil
}

// ListRecordings 列出某房间的录制（owner/admin）。
func (h *Handler) ListRecordings(c *gin.Context) {
	roomID, ok := h.requireRoomManager(c)
	if !ok {
		return
	}

	req, err := h.sfuRequest(http.MethodGet, "/recordings/list", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sfu unreachable"})
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.logger.Warn("SFU recordings list failed", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "sfu unreachable"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("sfu returned %d", resp.StatusCode)})
		return
	}
	var list sfuRecordingsList
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid sfu response"})
		return
	}

	prefix := fmt.Sprintf("room_%d_", roomID)
	entries := make([]recordingEntry, 0)
	for _, r := range list.Rooms {
		if !strings.HasPrefix(r.Dir, prefix) {
			continue
		}
		entries = append(entries, recordingEntry{
			Dir:       r.Dir,
			StartedAt: parseRecordingStart(strings.TrimPrefix(r.Dir, prefix)),
			Files:     r.Files,
		})
	}
	c.JSON(http.StatusOK, gin.H{"enabled": list.Enabled, "recordings": entries})
}

// DownloadRecording 代理下载某房间的一个录制文件（owner/admin）。
func (h *Handler) DownloadRecording(c *gin.Context) {
	roomID, ok := h.requireRoomManager(c)
	if !ok {
		return
	}
	dir := c.Param("dir")
	file := c.Param("file")
	if !recordingNameRE.MatchString(dir) || !recordingNameRE.MatchString(file) || strings.Contains(dir, "..") || strings.Contains(file, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recording name"})
		return
	}
	// 只允许访问属于本房间的目录
	if !strings.HasPrefix(dir, fmt.Sprintf("room_%d_", roomID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "recording does not belong to this room"})
		return
	}

	query := url.Values{"room": {dir}, "file": {file}}
	req, err := h.sfuRequest(http.MethodGet, "/recordings/download", query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sfu unreachable"})
		return
	}
	req = req.WithContext(c.Request.Context())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.logger.Warn("SFU recording download failed", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "sfu unreachable"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording not found"})
		return
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", `attachment; filename="`+file+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		c.Header("Content-Length", cl)
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// parseRecordingStart 把录制目录名里的时间戳（20060102_150405）转成 RFC3339；解析失败返回空串。
func parseRecordingStart(suffix string) string {
	t, err := time.ParseInLocation("20060102_150405", suffix, time.Local)
	if err != nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
