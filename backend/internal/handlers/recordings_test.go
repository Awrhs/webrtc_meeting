package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// roleStub 只实现 GetMemberRole：房间 1 里用户 1 是房主、用户 2 是普通成员
type roleStub struct {
	service.RoomService
}

func (roleStub) GetMemberRole(roomID uint, userID uint) (models.Role, error) {
	switch {
	case roomID == 1 && userID == 1:
		return models.RoleOwner, nil
	case roomID == 1 && userID == 2:
		return models.RoleMember, nil
	}
	return "", http.ErrNoLocation
}

// fakeSFU 模拟 SFU 的录制接口：校验 callback token，返回两个房间的录制目录
func fakeSFU(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/recordings/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-SFU-Callback-Token") != "cb-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": true,
			"rooms": []map[string]interface{}{
				{"dir": "room_1_20261108_173000", "files": []string{"173001_audio_x.ogg", "manifest.txt"}},
				{"dir": "room_2_20261108_180000", "files": []string{"180001_video_y.ivf"}},
			},
		})
	})
	mux.HandleFunc("/recordings/download", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("room") == "room_1_20261108_173000" && r.URL.Query().Get("file") == "173001_audio_x.ogg" {
			_, _ = w.Write([]byte("OggS-bytes"))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
	return httptest.NewServer(mux)
}

func newRecordingsRouter(sfuURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{roomService: roleStub{}, logger: zap.NewNop(), sfuCallbackToken: "cb-token", sfuHTTPURL: sfuURL}
	r := gin.New()
	// 用 user 查询参数模拟已认证用户
	r.Use(func(c *gin.Context) {
		if u := c.Query("user"); u == "1" {
			c.Set("user_id", uint(1))
		} else if u == "2" {
			c.Set("user_id", uint(2))
		}
	})
	r.GET("/rooms/:id/recordings", h.ListRecordings)
	r.GET("/rooms/:id/recordings/:dir/:file", h.DownloadRecording)
	return r
}

func TestListRecordingsFiltersByRoomAndRole(t *testing.T) {
	sfu := fakeSFU(t)
	defer sfu.Close()
	r := newRecordingsRouter(sfu.URL)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms/1/recordings?user=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("owner list: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Enabled    bool             `json:"enabled"`
		Recordings []recordingEntry `json:"recordings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled || len(body.Recordings) != 1 || body.Recordings[0].Dir != "room_1_20261108_173000" {
		t.Fatalf("expected only room 1 recordings: %+v", body)
	}
	if body.Recordings[0].StartedAt == "" {
		t.Fatal("started_at should be parsed from the directory name")
	}

	// 普通成员不能看
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms/1/recordings?user=2", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member list: %d", w.Code)
	}
}

func TestDownloadRecordingProxiesAndGuardsRoom(t *testing.T) {
	sfu := fakeSFU(t)
	defer sfu.Close()
	r := newRecordingsRouter(sfu.URL)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms/1/recordings/room_1_20261108_173000/173001_audio_x.ogg?user=1", nil))
	if w.Code != http.StatusOK || w.Body.String() != "OggS-bytes" {
		t.Fatalf("download: %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q", cd)
	}

	// 别的房间的目录：拒绝
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms/1/recordings/room_2_20261108_180000/180001_video_y.ivf?user=1", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-room download must be forbidden: %d", w.Code)
	}

	// 非法文件名：拒绝
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms/1/recordings/room_1_20261108_173000/..%2Fsecret?user=1", nil))
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Fatalf("path traversal attempt must be rejected: %d", w.Code)
	}
}
