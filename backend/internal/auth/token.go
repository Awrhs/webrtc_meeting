package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const PurposeSFUJoin = "sfu_join"

type SFUClaims struct {
	UserID  uint
	RoomID  uint
	Purpose string
}

func GenerateUserToken(userID uint, secret string, expiresIn time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": float64(userID),
		"exp":     time.Now().Add(expiresIn).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func GenerateSFUJoinToken(userID, roomID uint, secret string, expiresIn time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": float64(userID),
		"room_id": float64(roomID),
		"purpose": PurposeSFUJoin,
		"exp":     time.Now().Add(expiresIn).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ValidateUserToken(tokenStr, secret string) (uint, error) {
	claims, err := parseMapClaims(tokenStr, secret)
	if err != nil {
		return 0, err
	}
	userID, ok := numberClaim(claims, "user_id")
	if !ok {
		return 0, errors.New("invalid token claims")
	}
	return uint(userID), nil
}

func ValidateSFUJoinToken(tokenStr, secret string) (*SFUClaims, error) {
	claims, err := parseMapClaims(tokenStr, secret)
	if err != nil {
		return nil, err
	}
	purpose, ok := claims["purpose"].(string)
	if !ok || purpose != PurposeSFUJoin {
		return nil, errors.New("invalid token purpose")
	}
	userID, ok := numberClaim(claims, "user_id")
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	roomID, ok := numberClaim(claims, "room_id")
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	return &SFUClaims{UserID: uint(userID), RoomID: uint(roomID), Purpose: purpose}, nil
}

func parseMapClaims(tokenStr, secret string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

func numberClaim(claims jwt.MapClaims, key string) (float64, bool) {
	value, ok := claims[key].(float64)
	return value, ok
}
