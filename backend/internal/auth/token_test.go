package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestUserTokenRoundTrip(t *testing.T) {
	secret := "test-secret"
	token, err := GenerateUserToken(42, secret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateUserToken failed: %v", err)
	}

	userID, err := ValidateUserToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateUserToken failed: %v", err)
	}
	if userID != 42 {
		t.Fatalf("expected user id 42, got %d", userID)
	}
}

func TestSFUJoinTokenRoundTrip(t *testing.T) {
	secret := "test-secret"
	token, err := GenerateSFUJoinToken(7, 11, secret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateSFUJoinToken failed: %v", err)
	}

	claims, err := ValidateSFUJoinToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateSFUJoinToken failed: %v", err)
	}
	if claims.UserID != 7 || claims.RoomID != 11 || claims.Purpose != PurposeSFUJoin {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestSFUJoinTokenRejectsWrongPurpose(t *testing.T) {
	secret := "test-secret"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(7),
		"room_id": float64(11),
		"purpose": "app_login",
		"exp":     time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	if _, err := ValidateSFUJoinToken(token, secret); err == nil {
		t.Fatal("expected wrong purpose token to be rejected")
	}
}

func TestTokenRejectsUnexpectedSigningMethod(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"user_id": float64(1),
		"exp":     time.Now().Add(time.Hour).Unix(),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	if _, err := ValidateUserToken(token, "test-secret"); err == nil {
		t.Fatal("expected token with unexpected signing method to be rejected")
	}
}
