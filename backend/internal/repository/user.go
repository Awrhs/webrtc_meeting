package repository

import (
	"conference-sfu/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type UserRepository interface {
	Create(user *models.User) error
	FindByID(id uint) (*models.User, error)
	FindByEmail(email string) (*models.User, error)
	FindByPhone(phone string) (*models.User, error)
	FindByUsername(username string) (*models.User, error)
	Update(user *models.User) error
	CreateDevice(device *models.Device) error
	GetDevicesByUserID(userID uint) ([]*models.Device, error)
	DeleteDevice(deviceID uint, userID uint) error
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(user *models.User) error {
	return r.db.Create(user).Error
}

func (r *userRepository) FindByID(id uint) (*models.User, error) {
	var user models.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByPhone(phone string) (*models.User, error) {
	var user models.User
	err := r.db.Where("phone = ?", phone).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByUsername(username string) (*models.User, error) {
	var user models.User
	err := r.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Update(user *models.User) error {
	return r.db.Save(user).Error
}

func (r *userRepository) CreateDevice(device *models.Device) error {
	return r.db.Create(device).Error
}

func (r *userRepository) GetDevicesByUserID(userID uint) ([]*models.Device, error) {
	var devices []*models.Device
	err := r.db.Where("user_id = ?", userID).Order("last_active desc").Find(&devices).Error
	return devices, err
}

func (r *userRepository) DeleteDevice(deviceID uint, userID uint) error {
	return r.db.Where("id = ? AND user_id = ?", deviceID, userID).Delete(&models.Device{}).Error
}

type RedisClient struct {
	client *redis.Client
}

func NewRedisClient(addr, password string) *RedisClient {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	return &RedisClient{client: rdb}
}

func (r *RedisClient) SetOnline(userID uint, status string, expiration time.Duration) error {
	ctx := context.Background()
	return r.client.Set(ctx, "online:"+string(rune(userID)), status, expiration).Err()
}

func (r *RedisClient) GetOnline(userID uint) (string, error) {
	ctx := context.Background()
	return r.client.Get(ctx, "online:"+string(rune(userID))).Result()
}

func (r *RedisClient) AddToRoom(roomID uint, userID uint) error {
	ctx := context.Background()
	return r.client.SAdd(ctx, "room:"+string(rune(roomID)), userID).Err()
}

func (r *RedisClient) RemoveFromRoom(roomID uint, userID uint) error {
	ctx := context.Background()
	return r.client.SRem(ctx, "room:"+string(rune(roomID)), userID).Err()
}

func (r *RedisClient) GetRoomMembers(roomID uint) ([]uint, error) {
	ctx := context.Background()
	result, err := r.client.SMembers(ctx, "room:"+string(rune(roomID))).Result()
	if err != nil {
		return nil, err
	}
	members := make([]uint, len(result))
	for i, idStr := range result {
		id, _ := strconv.Atoi(idStr)
		members[i] = uint(id)
	}
	return members, nil
}

func (r *RedisClient) Close() error {
	return r.client.Close()
}

// ==================== User Cache Methods ====================

const (
	userCacheKeyPrefix    = "user:"
	userCacheTTL          = 15 * time.Minute
	roomCacheKeyPrefix    = "room:info:"
	roomCacheTTL          = 10 * time.Minute
	userRoomsCachePrefix  = "user:rooms:"
	userRoomsCacheTTL     = 5 * time.Minute
)

func (r *RedisClient) SetUser(user *models.User) error {
	ctx := context.Background()
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%d", userCacheKeyPrefix, user.ID)
	return r.client.Set(ctx, key, data, userCacheTTL).Err()
}

func (r *RedisClient) GetUser(userID uint) (*models.User, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userCacheKeyPrefix, userID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var user models.User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *RedisClient) DeleteUser(userID uint) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userCacheKeyPrefix, userID)
	return r.client.Del(ctx, key).Err()
}

// ==================== Room Cache Methods ====================

func (r *RedisClient) SetRoom(room *models.Room) error {
	ctx := context.Background()
	data, err := json.Marshal(room)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%d", roomCacheKeyPrefix, room.ID)
	return r.client.Set(ctx, key, data, roomCacheTTL).Err()
}

func (r *RedisClient) GetRoom(roomID uint) (*models.Room, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", roomCacheKeyPrefix, roomID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var room models.Room
	if err := json.Unmarshal([]byte(data), &room); err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *RedisClient) DeleteRoom(roomID uint) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", roomCacheKeyPrefix, roomID)
	return r.client.Del(ctx, key).Err()
}

// ==================== User Rooms Cache Methods ====================

func (r *RedisClient) SetUserRooms(userID uint, rooms []*models.Room) error {
	ctx := context.Background()
	data, err := json.Marshal(rooms)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s%d", userRoomsCachePrefix, userID)
	return r.client.Set(ctx, key, data, userRoomsCacheTTL).Err()
}

func (r *RedisClient) GetUserRooms(userID uint) ([]*models.Room, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userRoomsCachePrefix, userID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var rooms []*models.Room
	if err := json.Unmarshal([]byte(data), &rooms); err != nil {
		return nil, err
	}
	return rooms, nil
}

func (r *RedisClient) DeleteUserRooms(userID uint) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userRoomsCachePrefix, userID)
	return r.client.Del(ctx, key).Err()
}