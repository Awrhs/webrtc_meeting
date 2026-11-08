package auth

import (
	"errors"
	"testing"
	"time"
)

type fakeSessions map[string]uint

func (f fakeSessions) SessionUserID(jti string) (uint, bool, error) {
	uid, ok := f[jti]
	return uid, ok, nil
}

type fakeVersions map[uint]int

func (f fakeVersions) TokenVersion(userID uint) (int, error) {
	return f[userID], nil
}

func TestSessionTokenRoundTrip(t *testing.T) {
	token, err := GenerateSessionToken(42, 3, "jti-1", "dev-abc", "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ParseUserClaims(token, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 42 || claims.Version != 3 || claims.JTI != "jti-1" || claims.DeviceID != "dev-abc" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if time.Until(claims.ExpiresAt) < 50*time.Minute {
		t.Fatalf("exp too early: %v", claims.ExpiresAt)
	}
}

func TestValidator(t *testing.T) {
	sessions := fakeSessions{"live": 7}
	versions := fakeVersions{7: 2}
	v := &Validator{Secret: "s", Sessions: sessions, Versions: versions}

	good, _ := GenerateSessionToken(7, 2, "live", "d", "s", time.Hour)
	if _, err := v.Validate(good); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}

	revoked, _ := GenerateSessionToken(7, 2, "gone", "d", "s", time.Hour)
	if _, err := v.Validate(revoked); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("revoked session: got %v", err)
	}

	stale, _ := GenerateSessionToken(7, 1, "live", "d", "s", time.Hour)
	if _, err := v.Validate(stale); !errors.Is(err, ErrTokenSuperseded) {
		t.Fatalf("old token version: got %v", err)
	}

	legacy, _ := GenerateUserToken(7, "s", time.Hour)
	if _, err := v.Validate(legacy); !errors.Is(err, ErrLegacyToken) {
		t.Fatalf("legacy token without jti: got %v", err)
	}

	// 会话属于别的用户（jti 被挪用）
	stolen, _ := GenerateSessionToken(8, 0, "live", "d", "s", time.Hour)
	if _, err := v.Validate(stolen); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("session/user mismatch: got %v", err)
	}

	if _, err := v.Validate(good + "x"); err == nil {
		t.Fatal("tampered token must be rejected")
	}
}
