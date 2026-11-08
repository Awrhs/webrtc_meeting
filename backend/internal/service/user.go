package service

import (
	"conference-sfu/backend/internal/auth"
	"conference-sfu/backend/internal/mail"
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// DeviceInfo 登录设备信息：设备指纹（X-Device-Id 或 UA 哈希）、UA、IP。
type DeviceInfo struct {
	ID        string
	UserAgent string
	IP        string
}

// UserServiceConfig 用户服务配置。
type UserServiceConfig struct {
	JWTSecret                string
	JWTExpiry                time.Duration
	Mailer                   mail.Mailer
	RequireEmailVerification bool
	AppName                  string
}

type UserService interface {
	// Register 返回新用户以及"是否需要先验证邮箱才能登录"
	Register(username, email, phone, password string) (*models.User, bool, error)
	Login(account, password string, device DeviceInfo) (string, *models.User, error)
	Logout(userID uint, jti string) error
	GetUserByID(id uint) (*models.User, error)
	UpdateProfile(userID uint, nickname, avatar string) error
	// UpdatePassword 使所有旧 token 失效，并为当前设备签发新 token
	UpdatePassword(userID uint, oldPassword, newPassword string, device DeviceInfo) (string, error)
	// RevokeDevice 删除登录设备记录并吊销该设备上的全部会话
	RevokeDevice(userID uint, deviceRowID uint) error
	// TokenVersion 实现 auth.VersionSource
	TokenVersion(userID uint) (int, error)
	RequestPasswordReset(account string) error
	ResetPassword(account, code, newPassword string) error
	SendEmailVerification(email string) error
	VerifyEmail(email, code string) error
}

type userService struct {
	repo   repository.UserRepository
	redis  *repository.RedisClient
	cfg    UserServiceConfig
	logger *zap.Logger
}

var (
	ErrInvalidCode      = errors.New("invalid or expired verification code")
	ErrTooManyAttempts  = errors.New("too many attempts, please request a new code")
	ErrEmailNotVerified = errors.New("email not verified, please verify your email first")
)

const (
	codeKindPasswordReset  = "pwreset"
	codeKindEmailVerify    = "verify"
	passwordResetCodeTTL   = 15 * time.Minute
	emailVerifyCodeTTL     = 24 * time.Hour
	maxCodeAttempts        = 5
	tokenVersionCacheTTL   = 10 * time.Minute
	defaultSessionDuration = 168 * time.Hour
)

func NewUserService(repo repository.UserRepository, redis *repository.RedisClient, cfg UserServiceConfig, logger *zap.Logger) UserService {
	if cfg.JWTExpiry <= 0 {
		cfg.JWTExpiry = defaultSessionDuration
	}
	if cfg.Mailer == nil {
		cfg.Mailer = &mail.LogMailer{Logger: logger}
	}
	if cfg.AppName == "" {
		cfg.AppName = "SFU会议"
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &userService{repo: repo, redis: redis, cfg: cfg, logger: logger}
}

func (s *userService) Register(username, email, phone, password string) (*models.User, bool, error) {
	if existing, err := s.repo.FindByEmail(email); err == nil && existing != nil {
		return nil, false, errors.New("email already registered")
	}
	if existing, err := s.repo.FindByPhone(phone); err == nil && existing != nil {
		return nil, false, errors.New("phone already registered")
	}
	if existing, err := s.repo.FindByUsername(username); err == nil && existing != nil {
		return nil, false, errors.New("username already taken")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, false, err
	}

	user := &models.User{
		Username: username,
		Email:    email,
		Phone:    phone,
		Password: string(hashedPassword),
		Nickname: username,
		Status:   models.UserStatusActive,
	}
	if err := s.repo.Create(user); err != nil {
		return nil, false, err
	}

	if s.cfg.RequireEmailVerification {
		if err := s.SendEmailVerification(user.Email); err != nil {
			s.logger.Warn("send verification mail failed", zap.Uint("userID", user.ID), zap.Error(err))
		}
		return user, true, nil
	}
	return user, false, nil
}

func (s *userService) Login(account, password string, device DeviceInfo) (string, *models.User, error) {
	user, err := s.findByAccount(account)
	if err != nil || user == nil {
		return "", nil, errors.New("account not found")
	}
	if user.Status != models.UserStatusActive {
		return "", nil, errors.New("account is inactive or banned")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", nil, errors.New("invalid password")
	}
	if s.cfg.RequireEmailVerification && !user.EmailVerified {
		return "", nil, ErrEmailNotVerified
	}

	token, err := s.issueSession(user, device)
	if err != nil {
		return "", nil, err
	}
	_ = s.redis.SetOnline(user.ID, "online", 24*time.Hour)
	return token, user, nil
}

// issueSession 创建一个 Redis 会话并签发绑定它的 token。
func (s *userService) issueSession(user *models.User, device DeviceInfo) (string, error) {
	jti := uuid.NewString()
	if err := s.redis.CreateSession(jti, user.ID, device.ID, s.cfg.JWTExpiry); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return auth.GenerateSessionToken(user.ID, user.TokenVersion, jti, device.ID, s.cfg.JWTSecret, s.cfg.JWTExpiry)
}

func (s *userService) Logout(userID uint, jti string) error {
	if jti == "" {
		return nil
	}
	return s.redis.RevokeSession(userID, jti)
}

func (s *userService) GetUserByID(id uint) (*models.User, error) {
	// Try cache first
	if cached, err := s.redis.GetUser(id); err == nil && cached != nil {
		return cached, nil
	}

	// Cache miss, fetch from DB
	user, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	// Cache the result
	_ = s.redis.SetUser(user)
	return user, nil
}

func (s *userService) UpdateProfile(userID uint, nickname, avatar string) error {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return err
	}

	user.Nickname = nickname
	if avatar != "" {
		user.Avatar = avatar
	}

	if err := s.repo.Update(user); err != nil {
		return err
	}

	// Invalidate cache after update
	_ = s.redis.DeleteUser(userID)
	return nil
}

func (s *userService) UpdatePassword(userID uint, oldPassword, newPassword string, device DeviceInfo) (string, error) {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return "", errors.New("old password incorrect")
	}
	if err := s.setPasswordAndRevokeAll(user, newPassword); err != nil {
		return "", err
	}
	// 当前设备不应被登出：签发一个新会话（携带新的 token 版本）
	return s.issueSession(user, device)
}

// setPasswordAndRevokeAll 写入新密码、token_version+1，并吊销该用户的全部会话。
func (s *userService) setPasswordAndRevokeAll(user *models.User, newPassword string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	version := user.TokenVersion + 1
	if err := s.repo.SetPassword(user.ID, string(hashed), version); err != nil {
		return err
	}
	user.Password = string(hashed)
	user.TokenVersion = version
	_ = s.redis.SetTokenVersion(user.ID, version, tokenVersionCacheTTL)
	if err := s.redis.RevokeAllSessions(user.ID); err != nil {
		s.logger.Warn("revoke sessions failed", zap.Uint("userID", user.ID), zap.Error(err))
	}
	_ = s.redis.DeleteUser(user.ID)
	return nil
}

func (s *userService) RevokeDevice(userID uint, deviceRowID uint) error {
	device, err := s.repo.GetDeviceByID(deviceRowID, userID)
	if err != nil {
		return errors.New("device not found")
	}
	if _, err := s.redis.RevokeDeviceSessions(userID, device.DeviceID); err != nil {
		return err
	}
	return s.repo.DeleteDevice(deviceRowID, userID)
}

// TokenVersion 实现 auth.VersionSource：优先读短期缓存，未命中回源数据库。
func (s *userService) TokenVersion(userID uint) (int, error) {
	if v, ok, err := s.redis.GetTokenVersion(userID); err == nil && ok {
		return v, nil
	}
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return 0, err
	}
	_ = s.redis.SetTokenVersion(userID, user.TokenVersion, tokenVersionCacheTTL)
	return user.TokenVersion, nil
}

// ==================== 找回密码 ====================

// RequestPasswordReset 向账号绑定的邮箱发送重置验证码。账号不存在时同样返回成功，不泄露账号是否存在。
func (s *userService) RequestPasswordReset(account string) error {
	user, err := s.findByAccount(account)
	if err != nil || user == nil {
		return nil
	}
	code, err := generateCode()
	if err != nil {
		return err
	}
	if err := s.redis.StoreCode(codeKindPasswordReset, user.ID, hashCode(code), passwordResetCodeTTL); err != nil {
		return err
	}
	return s.cfg.Mailer.Send(context.Background(), mail.Message{
		To:      user.Email,
		Subject: fmt.Sprintf("【%s】重置密码验证码", s.cfg.AppName),
		Body:    fmt.Sprintf("您正在重置 %s 的登录密码，验证码：%s（%d 分钟内有效）。\n如非本人操作，请忽略本邮件。", s.cfg.AppName, code, int(passwordResetCodeTTL.Minutes())),
	})
}

// ResetPassword 校验验证码并设置新密码；成功后该用户所有会话失效。
func (s *userService) ResetPassword(account, code, newPassword string) error {
	user, err := s.findByAccount(account)
	if err != nil || user == nil {
		return ErrInvalidCode
	}
	if err := s.consumeCode(codeKindPasswordReset, user.ID, code); err != nil {
		return err
	}
	return s.setPasswordAndRevokeAll(user, newPassword)
}

// ==================== 邮箱验证 ====================

func (s *userService) SendEmailVerification(email string) error {
	user, err := s.repo.FindByEmail(email)
	if err != nil || user == nil {
		return nil // 不泄露邮箱是否注册
	}
	if user.EmailVerified {
		return nil
	}
	code, err := generateCode()
	if err != nil {
		return err
	}
	if err := s.redis.StoreCode(codeKindEmailVerify, user.ID, hashCode(code), emailVerifyCodeTTL); err != nil {
		return err
	}
	return s.cfg.Mailer.Send(context.Background(), mail.Message{
		To:      user.Email,
		Subject: fmt.Sprintf("【%s】邮箱验证码", s.cfg.AppName),
		Body:    fmt.Sprintf("欢迎注册 %s，您的邮箱验证码：%s（%d 小时内有效）。", s.cfg.AppName, code, int(emailVerifyCodeTTL.Hours())),
	})
}

func (s *userService) VerifyEmail(email, code string) error {
	user, err := s.repo.FindByEmail(email)
	if err != nil || user == nil {
		return ErrInvalidCode
	}
	if user.EmailVerified {
		return nil
	}
	if err := s.consumeCode(codeKindEmailVerify, user.ID, code); err != nil {
		return err
	}
	if err := s.repo.SetEmailVerified(user.ID); err != nil {
		return err
	}
	_ = s.redis.DeleteUser(user.ID)
	return nil
}

// consumeCode 校验一次性验证码：超过最大尝试次数即作废；校验成功立即删除。
func (s *userService) consumeCode(kind string, userID uint, code string) error {
	stored, ok, err := s.redis.GetCode(kind, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCode
	}
	attempts, err := s.redis.IncrCodeAttempts(kind, userID, emailVerifyCodeTTL)
	if err != nil {
		return err
	}
	if attempts > maxCodeAttempts {
		_ = s.redis.DeleteCode(kind, userID)
		return ErrTooManyAttempts
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(hashCode(code))) != 1 {
		return ErrInvalidCode
	}
	return s.redis.DeleteCode(kind, userID)
}

// findByAccount 按首字符判断账号类型：数字开头为手机号，含 @ 为邮箱，否则为用户名。
func (s *userService) findByAccount(account string) (*models.User, error) {
	if account == "" {
		return nil, errors.New("empty account")
	}
	switch {
	case account[0] >= '0' && account[0] <= '9':
		return s.repo.FindByPhone(account)
	case contains(account, '@'):
		return s.repo.FindByEmail(account)
	default:
		return s.repo.FindByUsername(account)
	}
}

// generateCode 生成 6 位数字验证码（crypto/rand）。
func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func contains(s string, c rune) bool {
	for _, ch := range s {
		if ch == c {
			return true
		}
	}
	return false
}
