package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const PurposeSFUJoin = "sfu_join"

type SFUClaims struct {
	UserID    uint
	RoomID    uint
	Purpose   string
	JTI       string
	ExpiresAt time.Time
}

// UserClaims 登录 token（会话版）的声明。
type UserClaims struct {
	UserID    uint
	Version   int    // 用户的 token_version：改密码后递增，旧 token 全部失效
	JTI       string // 会话 ID：登出 / 踢设备时按它吊销
	DeviceID  string // 登录设备指纹
	ExpiresAt time.Time
}

// GenerateUserToken 生成不带会话信息的旧式 token（仅供兼容/测试）。
// 生产登录请使用 GenerateSessionToken：没有 jti 的 token 会被 Validator 拒绝。
func GenerateUserToken(userID uint, secret string, expiresIn time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": float64(userID),
		"exp":     time.Now().Add(expiresIn).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// GenerateSessionToken 生成绑定会话（jti）、token 版本与设备的登录 token。
func GenerateSessionToken(userID uint, version int, jti, deviceID, secret string, expiresIn time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": float64(userID),
		"ver":     float64(version),
		"jti":     jti,
		"dev":     deviceID,
		"exp":     time.Now().Add(expiresIn).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseUserClaims 校验签名与过期时间并解析登录 token 的声明（不检查会话是否仍有效）。
func ParseUserClaims(tokenStr, secret string) (*UserClaims, error) {
	claims, err := parseMapClaims(tokenStr, secret)
	if err != nil {
		return nil, err
	}
	userID, ok := numberClaim(claims, "user_id")
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	version, _ := numberClaim(claims, "ver")
	jti, _ := claims["jti"].(string)
	deviceID, _ := claims["dev"].(string)
	exp, _ := numberClaim(claims, "exp")
	return &UserClaims{
		UserID:    uint(userID),
		Version:   int(version),
		JTI:       jti,
		DeviceID:  deviceID,
		ExpiresAt: time.Unix(int64(exp), 0),
	}, nil
}

func GenerateSFUJoinToken(userID, roomID uint, secret string, expiresIn time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": float64(userID),
		"room_id": float64(roomID),
		"purpose": PurposeSFUJoin,
		"jti":     uuid.NewString(),
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
	jti, _ := claims["jti"].(string)
	exp, _ := numberClaim(claims, "exp")
	return &SFUClaims{
		UserID:    uint(userID),
		RoomID:    uint(roomID),
		Purpose:   purpose,
		JTI:       jti,
		ExpiresAt: time.Unix(int64(exp), 0),
	}, nil
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
