package repository

import (
	"conference-sfu/backend/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type UserRepository interface {
	Create(user *models.User) error
	FindByID(id uint) (*models.User, error)
	FindByIDs(ids []uint) ([]*models.User, error)
	FindByEmail(email string) (*models.User, error)
	FindByPhone(phone string) (*models.User, error)
	FindByUsername(username string) (*models.User, error)
	Update(user *models.User) error
	CreateDevice(device *models.Device) error
	UpsertDevice(device *models.Device) error
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

func (r *userRepository) FindByIDs(ids []uint) ([]*models.User, error) {
	if len(ids) == 0 {
		return []*models.User{}, nil
	}
	var users []*models.User
	err := r.db.Where("id IN ?", ids).Find(&users).Error
	return users, err
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

// UpsertDevice 按 (user_id, device_id) 找到则更新，否则创建。
func (r *userRepository) UpsertDevice(device *models.Device) error {
	var existing models.Device
	err := r.db.Where("user_id = ? AND device_id = ?", device.UserID, device.DeviceID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.Create(device).Error
	}
	if err != nil {
		return err
	}
	existing.UserAgent = device.UserAgent
	existing.IP = device.IP
	existing.LastActive = device.LastActive
	if device.DeviceName != "" {
		existing.DeviceName = device.DeviceName
	}
	if device.DeviceType != "" {
		existing.DeviceType = device.DeviceType
	}
	return r.db.Save(&existing).Error
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
	logger *zap.Logger
}

func NewRedisClient(addr, password string, logger *zap.Logger) *RedisClient {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	return &RedisClient{client: rdb, logger: logger}
}

func (r *RedisClient) SetOnline(userID uint, status string, expiration time.Duration) error {
	ctx := context.Background()
	return r.client.Set(ctx, "online:"+strconv.Itoa(int(userID)), status, expiration).Err()
}

// MarkTokenUsed 原子地标记 jti 为已使用。
// 返回 (true, nil) 表示首次使用；(false, nil) 表示已使用过（重放）。
func (r *RedisClient) MarkTokenUsed(jti string, ttl time.Duration) (bool, error) {
	if jti == "" {
		// 没有 jti 的旧 token：放行（向后兼容），不记录
		return true, nil
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	ctx := context.Background()
	ok, err := r.client.SetNX(ctx, "sfu_jti:"+jti, "1", ttl).Result()
	return ok, err
}

func (r *RedisClient) GetOnline(userID uint) (string, error) {
	ctx := context.Background()
	return r.client.Get(ctx, "online:"+strconv.Itoa(int(userID))).Result()
}

func (r *RedisClient) AddToRoom(roomID uint, userID uint) error {
	ctx := context.Background()
	return r.client.SAdd(ctx, "room:"+strconv.Itoa(int(roomID)), userID).Err()
}

func (r *RedisClient) RemoveFromRoom(roomID uint, userID uint) error {
	ctx := context.Background()
	return r.client.SRem(ctx, "room:"+strconv.Itoa(int(roomID)), userID).Err()
}

func (r *RedisClient) GetRoomMembers(roomID uint) ([]uint, error) {
	ctx := context.Background()
	result, err := r.client.SMembers(ctx, "room:"+strconv.Itoa(int(roomID))).Result()
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

// ==================== SFU 集群实例注册表 ====================

// SFUInstance 描述一个在线的 SFU 实例（集群模式）。
type SFUInstance struct {
	ID        string `json:"id"`         // 实例标识（唯一，建议 hostname:grpcPort）
	GRPCAddr  string `json:"grpc_addr"`  // gRPC 控制面地址（API 服务器可达）
	WSURL     string `json:"ws_url"`     // 对浏览器公开的 WebSocket 地址
	StartedAt int64  `json:"started_at"` // 实例启动时间
}

const (
	sfuInstanceKeyPrefix = "sfu:instance:"
	sfuRoomInstanceKey   = "sfu:room_instance:"
	sfuInstanceRoomsKey  = "sfu:instance_rooms"
)

// RegisterSFUInstance 注册/心跳一个 SFU 实例（TTL 内未续期视为下线）。
func (r *RedisClient) RegisterSFUInstance(inst SFUInstance, ttl time.Duration) error {
	ctx := context.Background()
	data, err := json.Marshal(inst)
	if err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, sfuInstanceKeyPrefix+inst.ID, data, ttl)
	pipe.ZIncrBy(ctx, sfuInstanceRoomsKey, 0, inst.ID) // 确保出现在负载排序集中
	_, err = pipe.Exec(ctx)
	return err
}

// UnregisterSFUInstance 摘除一个 SFU 实例。
func (r *RedisClient) UnregisterSFUInstance(instanceID string) error {
	ctx := context.Background()
	r.client.ZRem(ctx, sfuInstanceRoomsKey, instanceID)
	return r.client.Del(ctx, sfuInstanceKeyPrefix+instanceID).Err()
}

// ListSFUInstances 返回全部在线实例（TTL 内有心跳的）。
func (r *RedisClient) ListSFUInstances() ([]SFUInstance, error) {
	ctx := context.Background()
	var cursor uint64
	var result []SFUInstance
	for {
		keys, next, err := r.client.Scan(ctx, cursor, sfuInstanceKeyPrefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			data, err := r.client.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var inst SFUInstance
			if json.Unmarshal(data, &inst) == nil {
				result = append(result, inst)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return result, nil
}

// GetSFUInstanceByID 查找指定实例。
func (r *RedisClient) GetSFUInstanceByID(instanceID string) (*SFUInstance, error) {
	ctx := context.Background()
	data, err := r.client.Get(ctx, sfuInstanceKeyPrefix+instanceID).Bytes()
	if err != nil {
		return nil, err
	}
	var inst SFUInstance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// GetRoomSFUInstance 读取房间的实例亲和映射。
func (r *RedisClient) GetRoomSFUInstance(roomID uint) (string, error) {
	ctx := context.Background()
	return r.client.Get(ctx, fmt.Sprintf("%s%d", sfuRoomInstanceKey, roomID)).Result()
}

// SetRoomSFUInstance 写入房间的实例亲和映射（房间固定路由到同一实例）。
func (r *RedisClient) SetRoomSFUInstance(roomID uint, instanceID string) error {
	ctx := context.Background()
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, fmt.Sprintf("%s%d", sfuRoomInstanceKey, roomID), instanceID, 24*time.Hour)
	pipe.ZIncrBy(ctx, sfuInstanceRoomsKey, 1, instanceID)
	_, err := pipe.Exec(ctx)
	return err
}

// PickLeastLoadedSFU 返回当前承载房间数最少的在线实例（简单负载均衡）。
func (r *RedisClient) PickLeastLoadedSFU() (*SFUInstance, error) {
	instances, err := r.ListSFUInstances()
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, redis.Nil
	}
	ctx := context.Background()
	scores, err := r.client.ZRangeByScoreWithScores(ctx, sfuInstanceRoomsKey, &redis.ZRangeBy{
		Min: "-inf", Max: "+inf",
	}).Result()
	if err != nil || len(scores) == 0 {
		// 排序集不可用时退化为第一个实例
		inst := instances[0]
		return &inst, nil
	}
	scoreOf := func(id string) float64 {
		for _, z := range scores {
			if z.Member == id {
				return z.Score
			}
		}
		return 0
	}
	best := instances[0]
	bestScore := scoreOf(best.ID)
	for _, inst := range instances[1:] {
		if s := scoreOf(inst.ID); s < bestScore {
			best, bestScore = inst, s
		}
	}
	return &best, nil
}

func (r *RedisClient) Ping() error {
	ctx := context.Background()
	return r.client.Ping(ctx).Err()
}

// ==================== User Cache Methods ====================

const (
	userCacheKeyPrefix   = "user:"
	userCacheTTL         = 15 * time.Minute
	roomCacheKeyPrefix   = "room:info:"
	roomCacheTTL         = 10 * time.Minute
	userRoomsCachePrefix = "user:rooms:"
	userRoomsCacheTTL    = 5 * time.Minute
)

func (r *RedisClient) SetUser(user *models.User) error {
	ctx := context.Background()
	data, err := json.Marshal(user)
	if err != nil {
		r.logger.Error("Failed to marshal user for cache", zap.Error(err), zap.Uint("userID", user.ID))
		return err
	}
	key := fmt.Sprintf("%s%d", userCacheKeyPrefix, user.ID)
	if err := r.client.Set(ctx, key, data, userCacheTTL).Err(); err != nil {
		r.logger.Error("Failed to set user cache", zap.Error(err), zap.Uint("userID", user.ID))
		return err
	}
	return nil
}

func (r *RedisClient) GetUser(userID uint) (*models.User, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userCacheKeyPrefix, userID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		r.logger.Warn("Failed to get user from cache", zap.Error(err), zap.Uint("userID", userID))
		return nil, err
	}
	if err == redis.Nil {
		return nil, err
	}

	var user models.User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		r.logger.Error("Failed to unmarshal user from cache", zap.Error(err), zap.Uint("userID", userID))
		return nil, err
	}
	return &user, nil
}

func (r *RedisClient) DeleteUser(userID uint) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userCacheKeyPrefix, userID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		r.logger.Warn("Failed to delete user cache", zap.Error(err), zap.Uint("userID", userID))
		return err
	}
	return nil
}

// ==================== Room Cache Methods ====================

func (r *RedisClient) SetRoom(room *models.Room) error {
	ctx := context.Background()
	data, err := json.Marshal(room)
	if err != nil {
		r.logger.Error("Failed to marshal room for cache", zap.Error(err), zap.Uint("roomID", room.ID))
		return err
	}
	key := fmt.Sprintf("%s%d", roomCacheKeyPrefix, room.ID)
	if err := r.client.Set(ctx, key, data, roomCacheTTL).Err(); err != nil {
		r.logger.Error("Failed to set room cache", zap.Error(err), zap.Uint("roomID", room.ID))
		return err
	}
	return nil
}

func (r *RedisClient) GetRoom(roomID uint) (*models.Room, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", roomCacheKeyPrefix, roomID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		r.logger.Warn("Failed to get room from cache", zap.Error(err), zap.Uint("roomID", roomID))
		return nil, err
	}
	if err == redis.Nil {
		return nil, err
	}

	var room models.Room
	if err := json.Unmarshal([]byte(data), &room); err != nil {
		r.logger.Error("Failed to unmarshal room from cache", zap.Error(err), zap.Uint("roomID", roomID))
		return nil, err
	}
	return &room, nil
}

func (r *RedisClient) DeleteRoom(roomID uint) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", roomCacheKeyPrefix, roomID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		r.logger.Warn("Failed to delete room cache", zap.Error(err), zap.Uint("roomID", roomID))
		return err
	}
	return nil
}

// ==================== User Rooms Cache Methods ====================

func (r *RedisClient) SetUserRooms(userID uint, rooms []*models.Room) error {
	ctx := context.Background()
	data, err := json.Marshal(rooms)
	if err != nil {
		r.logger.Error("Failed to marshal user rooms for cache", zap.Error(err), zap.Uint("userID", userID))
		return err
	}
	key := fmt.Sprintf("%s%d", userRoomsCachePrefix, userID)
	if err := r.client.Set(ctx, key, data, userRoomsCacheTTL).Err(); err != nil {
		r.logger.Error("Failed to set user rooms cache", zap.Error(err), zap.Uint("userID", userID))
		return err
	}
	return nil
}

func (r *RedisClient) GetUserRooms(userID uint) ([]*models.Room, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userRoomsCachePrefix, userID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		r.logger.Warn("Failed to get user rooms from cache", zap.Error(err), zap.Uint("userID", userID))
		return nil, err
	}
	if err == redis.Nil {
		return nil, err
	}

	var rooms []*models.Room
	if err := json.Unmarshal([]byte(data), &rooms); err != nil {
		r.logger.Error("Failed to unmarshal user rooms from cache", zap.Error(err), zap.Uint("userID", userID))
		return nil, err
	}
	return rooms, nil
}

func (r *RedisClient) DeleteUserRooms(userID uint) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%d", userRoomsCachePrefix, userID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		r.logger.Warn("Failed to delete user rooms cache", zap.Error(err), zap.Uint("userID", userID))
		return err
	}
	return nil
}
