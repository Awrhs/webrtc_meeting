package service

import (
	"conference-sfu/backend/internal/auth"
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"errors"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type UserService interface {
	Register(username, email, phone, password string) (*models.User, error)
	Login(account, password string) (string, *models.User, error)
	GetUserByID(id uint) (*models.User, error)
	UpdateProfile(userID uint, nickname, avatar string) error
	UpdatePassword(userID uint, oldPassword, newPassword string) error
	ValidateToken(tokenStr string) (uint, error)
}

type userService struct {
	repo      repository.UserRepository
	redis     *repository.RedisClient
	jwtSecret string
	logger    *zap.Logger
}

func NewUserService(repo repository.UserRepository, redis *repository.RedisClient, jwtSecret string, logger *zap.Logger) UserService {
	return &userService{repo: repo, redis: redis, jwtSecret: jwtSecret, logger: logger}
}

func (s *userService) Register(username, email, phone, password string) (*models.User, error) {
	if existing, err := s.repo.FindByEmail(email); err != nil {
		s.logger.Warn("FindByEmail failed", zap.String("email", email), zap.Error(err))
	} else if existing != nil {
		return nil, errors.New("email already registered")
	}
	if existing, err := s.repo.FindByPhone(phone); err != nil {
		s.logger.Warn("FindByPhone failed", zap.String("phone", phone), zap.Error(err))
	} else if existing != nil {
		return nil, errors.New("phone already registered")
	}
	if existing, err := s.repo.FindByUsername(username); err != nil {
		s.logger.Warn("FindByUsername failed", zap.String("username", username), zap.Error(err))
	} else if existing != nil {
		return nil, errors.New("username already taken")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		Username: username,
		Email:    email,
		Phone:    phone,
		Password: string(hashedPassword),
		Nickname: username,
		Status:   models.UserStatusActive,
	}

	err = s.repo.Create(user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *userService) Login(account, password string) (string, *models.User, error) {
	var user *models.User
	var err error

	if len(account) > 0 && account[0] >= '0' && account[0] <= '9' {
		user, err = s.repo.FindByPhone(account)
	} else if len(account) > 0 && (account[0] < '0' || account[0] > '9') {
		if contains(account, '@') {
			user, err = s.repo.FindByEmail(account)
		} else {
			user, err = s.repo.FindByUsername(account)
		}
	}

	if err != nil || user == nil {
		return "", nil, errors.New("account not found")
	}

	if user.Status != models.UserStatusActive {
		return "", nil, errors.New("account is inactive or banned")
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return "", nil, errors.New("invalid password")
	}

	token, err := s.generateToken(user.ID)
	if err != nil {
		return "", nil, err
	}

	s.redis.SetOnline(user.ID, "online", 24*time.Hour)

	return token, user, nil
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
	s.redis.SetUser(user)
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
	s.redis.DeleteUser(userID)
	return nil
}

func (s *userService) UpdatePassword(userID uint, oldPassword, newPassword string) error {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword))
	if err != nil {
		return errors.New("old password incorrect")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.Password = string(hashedPassword)
	return s.repo.Update(user)
}

func (s *userService) ValidateToken(tokenStr string) (uint, error) {
	return auth.ValidateUserToken(tokenStr, s.jwtSecret)
}

func (s *userService) generateToken(userID uint) (string, error) {
	return auth.GenerateUserToken(userID, s.jwtSecret, 168*time.Hour)
}

func contains(s string, c rune) bool {
	for _, ch := range s {
		if ch == c {
			return true
		}
	}
	return false
}
