package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildICEServersRESTCredentials(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cfg := ICEConfig{
		STUNURLs:   []string{"stun:stun.example.com:3478"},
		TURNURLs:   []string{"turn:turn.example.com:3478?transport=udp"},
		TURNSecret: "shared-secret",
		TURNTTL:    30 * time.Minute,
	}
	servers, ttl := buildICEServers(cfg, 42, now)
	if ttl != 1800 {
		t.Fatalf("ttl = %d, want 1800", ttl)
	}
	if len(servers) != 2 || servers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Fatalf("unexpected servers: %+v", servers)
	}
	turn := servers[1]
	expiry, userID, found := strings.Cut(turn.Username, ":")
	if !found || userID != "42" {
		t.Fatalf("username format = %q", turn.Username)
	}
	exp, _ := strconv.ParseInt(expiry, 10, 64)
	if exp != now.Add(30*time.Minute).Unix() {
		t.Fatalf("expiry = %d", exp)
	}
	mac := hmac.New(sha1.New, []byte("shared-secret"))
	mac.Write([]byte(turn.Username))
	if turn.Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("credential is not HMAC-SHA1(secret, username)")
	}
}

func TestBuildICEServersLegacyAndNone(t *testing.T) {
	legacy := ICEConfig{
		STUNURLs:           []string{"stun:a"},
		LegacyTURNURL:      "turn:legacy:3478",
		LegacyTURNUser:     "user",
		LegacyTURNPassword: "pass",
	}
	servers, ttl := buildICEServers(legacy, 1, time.Now())
	if ttl != 0 || len(servers) != 2 || servers[1].Username != "user" || servers[1].Credential != "pass" {
		t.Fatalf("legacy static credentials expected: %+v ttl=%d", servers, ttl)
	}

	servers, ttl = buildICEServers(ICEConfig{STUNURLs: []string{"stun:a"}}, 1, time.Now())
	if ttl != 0 || len(servers) != 1 {
		t.Fatalf("stun only expected: %+v", servers)
	}
}
