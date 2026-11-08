package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"conference-sfu/backend/internal/auth"

	"github.com/gin-gonic/gin"
)

type stubSessions map[string]uint

func (s stubSessions) SessionUserID(jti string) (uint, bool, error) {
	uid, ok := s[jti]
	return uid, ok, nil
}

type stubVersions map[uint]int

func (v stubVersions) TokenVersion(userID uint) (int, error) { return v[userID], nil }

func newAuthTestRouter(validator *auth.Validator) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/me", AuthMiddleware(validator), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": GetUserID(c), "jti": GetJTI(c), "device_id": GetDeviceID(c)})
	})
	return r
}

func doGet(r http.Handler, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware(t *testing.T) {
	sessions := stubSessions{"live": 5}
	versions := stubVersions{5: 1}
	validator := &auth.Validator{Secret: "s", Sessions: sessions, Versions: versions}
	r := newAuthTestRouter(validator)

	good, _ := auth.GenerateSessionToken(5, 1, "live", "dev-1", "s", time.Hour)
	if w := doGet(r, "Bearer "+good); w.Code != http.StatusOK {
		t.Fatalf("valid token: %d %s", w.Code, w.Body.String())
	} else if body := w.Body.String(); !strings.Contains(body, `"user_id":5`) || !strings.Contains(body, `"jti":"live"`) || !strings.Contains(body, `"device_id":"dev-1"`) {
		t.Fatalf("context values not propagated: %s", body)
	}

	if w := doGet(r, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing header: %d", w.Code)
	}
	if w := doGet(r, "Token "+good); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong scheme: %d", w.Code)
	}

	revoked, _ := auth.GenerateSessionToken(5, 1, "gone", "dev-1", "s", time.Hour)
	if w := doGet(r, "Bearer "+revoked); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session must be rejected: %d", w.Code)
	}

	stale, _ := auth.GenerateSessionToken(5, 0, "live", "dev-1", "s", time.Hour)
	if w := doGet(r, "Bearer "+stale); w.Code != http.StatusUnauthorized {
		t.Fatalf("stale token version must be rejected: %d", w.Code)
	}

	legacy, _ := auth.GenerateUserToken(5, "s", time.Hour)
	if w := doGet(r, "Bearer "+legacy); w.Code != http.StatusUnauthorized {
		t.Fatalf("legacy token without session must be rejected: %d", w.Code)
	}
}
