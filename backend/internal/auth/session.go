package auth

import (
	"errors"
	"fmt"
)

// SessionStore 查询登录会话是否仍然有效（Redis 实现见 repository.RedisClient）。
type SessionStore interface {
	// SessionUserID 返回会话所属用户；会话不存在/已吊销/已过期时 ok=false。
	SessionUserID(jti string) (userID uint, ok bool, err error)
}

// VersionSource 返回用户当前的 token_version（改密码后递增）。
type VersionSource interface {
	TokenVersion(userID uint) (int, error)
}

var (
	ErrLegacyToken     = errors.New("token has no session, please login again")
	ErrSessionRevoked  = errors.New("session revoked or expired, please login again")
	ErrTokenSuperseded = errors.New("token invalidated by password change, please login again")
)

// Validator 完整校验一个登录 token：签名/过期 → 会话仍存在 → token 版本与用户当前版本一致。
// 任何一步不满足都返回错误；Sessions/Versions 为 nil 时跳过对应检查（仅限测试）。
type Validator struct {
	Secret   string
	Sessions SessionStore
	Versions VersionSource
}

func (v *Validator) Validate(tokenStr string) (*UserClaims, error) {
	claims, err := ParseUserClaims(tokenStr, v.Secret)
	if err != nil {
		return nil, err
	}
	if claims.JTI == "" {
		return nil, ErrLegacyToken
	}
	if v.Sessions != nil {
		userID, ok, err := v.Sessions.SessionUserID(claims.JTI)
		if err != nil {
			return nil, fmt.Errorf("session lookup failed: %w", err)
		}
		if !ok || userID != claims.UserID {
			return nil, ErrSessionRevoked
		}
	}
	if v.Versions != nil {
		current, err := v.Versions.TokenVersion(claims.UserID)
		if err != nil {
			return nil, fmt.Errorf("token version lookup failed: %w", err)
		}
		if current != claims.Version {
			return nil, ErrTokenSuperseded
		}
	}
	return claims, nil
}
