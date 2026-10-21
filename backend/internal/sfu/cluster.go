package sfu

import (
	"os"
	"sync"
	"time"

	"conference-sfu/backend/internal/repository"

	"go.uber.org/zap"
)

// ==================== SFU 集群：实例注册与控制面路由 ====================
//
// 集群模式（房间亲和性分片）：
//   - 每个 SFU 实例启动后向共享 Redis 心跳注册（TTL 30s，10s 续期），
//     注明自己的 gRPC 控制地址和面向浏览器的 WebSocket 地址；
//   - API 服务器为每个房间固定选择一个实例（房间亲和），并把该实例的
//     WS 地址（sfu_url）随 join 响应下发给前端；
//   - 房间内所有成员都落在同一实例上，跨实例订阅同步因此不是必需项；
//     不同房间分布在不同实例，实现水平扩展。
//
// 未注册任何实例时（单机部署），API 自动回退到 SFU_ADDR 配置的默认实例，
// 行为与旧版本完全兼容。

// DefaultInstanceID 生成本实例的唯一标识（hostname:grpcPort）。
func DefaultInstanceID(grpcPort string) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "sfu"
	}
	return host + ":" + grpcPort
}

// StartInstanceRegistration 启动实例心跳注册，返回停止函数（停机时摘除实例）。
func StartInstanceRegistration(redis *repository.RedisClient, inst repository.SFUInstance, logger *zap.Logger) (stop func()) {
	// 立即注册一次
	if err := redis.RegisterSFUInstance(inst, 30*time.Second); err != nil {
		logger.Warn("SFU instance registration failed (cluster disabled)", zap.Error(err))
	} else {
		logger.Info("SFU instance registered for cluster", zap.String("id", inst.ID), zap.String("ws_url", inst.WSURL))
	}

	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := redis.RegisterSFUInstance(inst, 30*time.Second); err != nil {
					logger.Warn("SFU instance heartbeat failed", zap.Error(err))
				}
			case <-done:
				if err := redis.UnregisterSFUInstance(inst.ID); err != nil {
					logger.Warn("SFU instance deregistration failed", zap.Error(err))
				}
				return
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
	}
}

// ControlRouter 在 API 服务器侧把 gRPC 控制命令路由到房间所在的 SFU 实例。
type ControlRouter struct {
	mu            sync.Mutex
	redis         *repository.RedisClient
	defaultClient *ControlClient // Redis 不可用/无实例注册时的回退客户端
	clients       map[string]*ControlClient
	logger        *zap.Logger
	controlToken  string
}

// NewControlRouter 创建控制面路由。defaultClient 可为 nil（无默认实例时控制命令仅在有实例时可用）。
func NewControlRouter(redis *repository.RedisClient, defaultClient *ControlClient, controlToken string, logger *zap.Logger) *ControlRouter {
	return &ControlRouter{
		redis:         redis,
		defaultClient: defaultClient,
		clients:       make(map[string]*ControlClient),
		logger:        logger,
		controlToken:  controlToken,
	}
}

// Close 关闭全部客户端连接。
func (r *ControlRouter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defaultClient != nil {
		_ = r.defaultClient.Close()
	}
	for id, c := range r.clients {
		_ = c.Close()
		delete(r.clients, id)
	}
}

// clientForRoom 返回房间所在实例的客户端（无映射时回退默认客户端）。
func (r *ControlRouter) clientForRoom(roomID uint) *ControlClient {
	if r.redis == nil {
		return r.defaultClient
	}
	instanceID, err := r.redis.GetRoomSFUInstance(roomID)
	if err != nil || instanceID == "" {
		return r.defaultClient
	}
	inst, err := r.redis.GetSFUInstanceByID(instanceID)
	if err != nil {
		return r.defaultClient
	}

	r.mu.Lock()
	client, ok := r.clients[inst.ID]
	r.mu.Unlock()
	if ok {
		return client
	}

	client, err = NewControlClient(inst.GRPCAddr, r.controlToken, r.logger)
	if err != nil {
		r.logger.Warn("Failed to dial SFU instance, falling back to default",
			zap.String("instance", inst.ID), zap.String("grpc_addr", inst.GRPCAddr), zap.Error(err))
		return r.defaultClient
	}
	r.mu.Lock()
	r.clients[inst.ID] = client
	r.mu.Unlock()
	return client
}

// KickUser 把某房间的成员从所在实例上踢出。
func (r *ControlRouter) KickUser(roomID, userID uint, reason string) error {
	if c := r.clientForRoom(roomID); c != nil {
		return c.KickUser(roomID, userID, reason)
	}
	return nil
}

// MuteUser 静音/取消静音。
func (r *ControlRouter) MuteUser(roomID, userID uint, isMuted bool) error {
	if c := r.clientForRoom(roomID); c != nil {
		return c.MuteUser(roomID, userID, isMuted)
	}
	return nil
}

// SetVideoEnabled 启用/禁用视频转发。
func (r *ControlRouter) SetVideoEnabled(roomID, userID uint, enabled bool) error {
	if c := r.clientForRoom(roomID); c != nil {
		return c.SetVideoEnabled(roomID, userID, enabled)
	}
	return nil
}
