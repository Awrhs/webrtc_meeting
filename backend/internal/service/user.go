package service

import (
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
}

func NewUserService(repo repository.UserRepository, redis *repository.RedisClient, jwtSecret string) UserService {
	return &userService{repo: repo, redis: redis, jwtSecret: jwtSecret}
}

func (s *userService) Register(username, email, phone, password string) (*models.User, error) {
	if existing, _ := s.repo.FindByEmail(email); existing != nil {
		return nil, errors.New("email already registered")
	}
	if existing, _ := s.repo.FindByPhone(phone); existing != nil {
		return nil, errors.New("phone already registered")
	}
	if existing, _ := s.repo.FindByUsername(username); existing != nil {
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
	return s.repo.FindByID(id)
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

	return s.repo.Update(user)
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
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.jwtSecret), nil
	})

	if err != nil || !token.Valid {
		return 0, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errors.New("invalid token claims")
	}

	userID := uint(claims["user_id"].(float64))
	return userID, nil
}

func (s *userService) generateToken(userID uint) (string, error) {
	claims := jwt.MapClaims{
		"user_id": float64(userID),
		"exp":     time.Now().Add(168 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

func contains(s string, c rune) bool {
	for _, ch := range s {
		if ch == c {
			return true
		}
	}
	return false
}