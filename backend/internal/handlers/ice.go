package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"time"
)

// ICEConfig 下发给浏览器的 ICE 服务器配置。
//
// TURN 凭据采用 coturn 的 REST 时限模式（--use-auth-secret）：
// username = "<过期时间戳>:<用户ID>"，credential = base64(HMAC-SHA1(secret, username))。
// 凭据只在 TTL 内有效，泄露后无法长期盗用中继带宽；旧方案把固定账号密码打进前端 JS 包，
// 任何人都能拿去白嫖 TURN 流量。
type ICEConfig struct {
	STUNURLs   []string
	TURNURLs   []string
	TURNSecret string
	TURNTTL    time.Duration
	// 兼容：未配置共享密钥时退回固定账号（coturn --lt-cred-mech）
	LegacyTURNURL      string
	LegacyTURNUser     string
	LegacyTURNPassword string
}

// ICEServer 对应浏览器 RTCIceServer。
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// SetICEConfig 注入 ICE 配置（join / sfu-token 响应会携带 ice_servers）。
func (h *Handler) SetICEConfig(cfg ICEConfig) {
	h.ice = cfg
}

// iceServersFor 为某用户生成 ICE 服务器列表；返回 TURN 凭据的有效秒数（无 TURN 时为 0）。
func (h *Handler) iceServersFor(userID uint, now time.Time) ([]ICEServer, int) {
	return buildICEServers(h.ice, userID, now)
}

func buildICEServers(cfg ICEConfig, userID uint, now time.Time) ([]ICEServer, int) {
	servers := make([]ICEServer, 0, 2)
	if len(cfg.STUNURLs) > 0 {
		servers = append(servers, ICEServer{URLs: append([]string(nil), cfg.STUNURLs...)})
	}

	ttl := 0
	switch {
	case len(cfg.TURNURLs) > 0 && cfg.TURNSecret != "":
		d := cfg.TURNTTL
		if d <= 0 {
			d = time.Hour
		}
		username, credential := turnRESTCredential(cfg.TURNSecret, userID, now.Add(d))
		servers = append(servers, ICEServer{URLs: append([]string(nil), cfg.TURNURLs...), Username: username, Credential: credential})
		ttl = int(d.Seconds())
	case len(cfg.TURNURLs) > 0 && cfg.LegacyTURNUser != "":
		servers = append(servers, ICEServer{URLs: append([]string(nil), cfg.TURNURLs...), Username: cfg.LegacyTURNUser, Credential: cfg.LegacyTURNPassword})
	case cfg.LegacyTURNURL != "" && cfg.LegacyTURNUser != "":
		servers = append(servers, ICEServer{URLs: []string{cfg.LegacyTURNURL}, Username: cfg.LegacyTURNUser, Credential: cfg.LegacyTURNPassword})
	}
	return servers, ttl
}

// turnRESTCredential 生成 coturn REST 时限凭据。
func turnRESTCredential(secret string, userID uint, expiresAt time.Time) (username, credential string) {
	username = fmt.Sprintf("%d:%d", expiresAt.Unix(), userID)
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	credential = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return username, credential
}
