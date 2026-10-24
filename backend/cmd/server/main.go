package main

import (
	"conference-sfu/backend/internal/config"
	"conference-sfu/backend/internal/handlers"
	"conference-sfu/backend/internal/middleware"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/service"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/internal/signaling"
	"conference-sfu/backend/pkg/logger"
	"context"
	"net/http"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg := config.Load()
	log := logger.NewLogger()

	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database", zap.Error(err))
	}

	redisClient := repository.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, log)

	userRepo := repository.NewUserRepository(db)
	roomRepo := repository.NewRoomRepository(db)
	chatRepo := repository.NewChatRepository(db)

	userService := service.NewUserService(userRepo, redisClient, cfg.JWTSecret, log)
	roomService := service.NewRoomService(roomRepo, userRepo, redisClient, cfg.SFUJWTSecret, cfg.SFUTokenExpiryMinutes, log)
	chatService := service.NewChatService(chatRepo, roomRepo)

	// Connect to SFU gRPC control server（默认实例；集群模式下按房间路由到各实例）
	var defaultSFUClient *sfu.ControlClient
	defaultSFUClient, err = sfu.NewControlClient(cfg.SFUAddr, cfg.SFUControlToken, log)
	if err != nil {
		log.Warn("Failed to connect to SFU gRPC server, running without SFU control", zap.Error(err))
	} else {
		log.Info("Connected to SFU gRPC server successfully", zap.String("addr", cfg.SFUAddr))
	}
	sfuRouter := sfu.NewControlRouter(redisClient, defaultSFUClient, cfg.SFUControlToken, log)
	defer sfuRouter.Close()

	hub := signaling.NewHubWithGrace(roomService, chatService, cfg.AllowedOrigins, time.Duration(cfg.PresenceGraceSeconds)*time.Second, log)
	go hub.Run()

	r := gin.Default()
	authLimiter := middleware.NewIPLimiter(cfg.RateLimitAuthPerMinute, time.Minute)
	apiLimiter := middleware.NewIPLimiter(cfg.RateLimitAPIPerMinute, time.Minute)
	wsLimiter := middleware.NewIPLimiter(cfg.RateLimitWSPerMinute, time.Minute)
	defer authLimiter.Stop()
	defer apiLimiter.Stop()
	defer wsLimiter.Stop()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	handler := handlers.NewHandler(userService, userRepo, roomService, chatService, hub, sfuRouter, cfg.SFUCallbackToken, cfg.UploadDir, int64(cfg.UploadMaxBytes), log)

	api := r.Group("/api/v1")
	{
		api.POST("/auth/register", authLimiter.Gin(), handler.Register)
		api.POST("/auth/login", authLimiter.Gin(), handler.Login)

		auth := api.Group("/")
		auth.Use(apiLimiter.Gin(), handlers.AuthMiddleware(cfg.JWTSecret))
		{
			auth.GET("/user/me", handler.GetMe)
			auth.PUT("/user/profile", handler.UpdateProfile)
			auth.PUT("/user/password", handler.UpdatePassword)
			auth.GET("/user/devices", handler.GetMyDevices)
			auth.DELETE("/user/devices/:id", handler.DeleteMyDevice)

			auth.POST("/rooms", handler.CreateRoom)
			auth.GET("/rooms", handler.GetMyRooms)
			auth.GET("/rooms/:id", handler.GetRoom)
			auth.GET("/rooms/number/:number", handler.GetRoomByNumber)
			auth.POST("/rooms/:id/join", handler.JoinRoom)
			auth.POST("/rooms/:id/sfu-token", handler.GetSFUToken)
			auth.DELETE("/rooms/:id", handler.DeleteRoom)
			auth.POST("/rooms/:id/lock", handler.LockRoom)
			auth.POST("/rooms/:id/unlock", handler.UnlockRoom)

			auth.POST("/rooms/:id/members/:userId/kick", handler.KickMember)
			auth.POST("/rooms/:id/members/:userId/mute", handler.MuteMember)
			auth.POST("/rooms/:id/members/:userId/unmute", handler.UnmuteMember)
			auth.POST("/rooms/:id/members/:userId/admin", handler.SetAdmin)
			auth.DELETE("/rooms/:id/members/:userId/admin", handler.RemoveAdmin)
			auth.POST("/rooms/:id/members/:userId/transfer", handler.TransferOwner)

			auth.GET("/rooms/:id/messages", handler.GetMessages)
			auth.POST("/rooms/:id/messages", handler.SendMessage)
			auth.GET("/rooms/:id/messages/private/:userId", handler.GetPrivateMessages)
			auth.POST("/rooms/:id/upload", handler.UploadFile)

			auth.GET("/rooms/:id/audit-logs", handler.GetAuditLogs)

			auth.PUT("/rooms/:id/media-state", handler.UpdateMediaState)

			// 离开会议与实时在线状态
			auth.POST("/rooms/:id/leave", handler.LeaveRoom)
			auth.GET("/rooms/:id/presence", handler.GetPresence)

			// E2EE 端到端加密密钥交换
			auth.POST("/e2ee/public-key", handler.UploadPublicKey)
			auth.GET("/e2ee/public-key/:user_id", handler.GetPublicKey)
			auth.DELETE("/e2ee/public-key", handler.DeletePublicKey)
		}
		// Callback for SFU to notify peer connection failed
		api.POST("/sfu/callback/:roomId/:userId/connection-failed", handler.NotifyConnectionFailed)
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// 上传文件需要登录才能访问（鉴权后按需返回）；
	// 聊天图片/文件内容只对登录用户开放，防止目录被任意抓取。
	r.GET("/uploads/:name", handlers.AuthMiddleware(cfg.JWTSecret), func(c *gin.Context) {
		name := c.Param("name")
		// 只允许纯文件名，防目录穿越
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file name"})
			return
		}
		c.File(filepath.Join(cfg.UploadDir, name))
	})
	r.GET("/readyz", func(c *gin.Context) {
		sqlDB, err := db.DB()
		if err != nil || sqlDB.Ping() != nil || redisClient.Ping() != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ready": true})
	})
	r.GET("/metrics.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/ws", wsLimiter.Gin(), func(c *gin.Context) {
		if !originAllowed(c.Request, cfg.AllowedOrigins) {
			c.Writer.WriteHeader(http.StatusForbidden)
			return
		}
		token := c.Query("token")
		if token == "" {
			c.Writer.WriteHeader(401)
			return
		}

		userID, err := handlers.ValidateToken(token, cfg.JWTSecret)
		if err != nil {
			c.Writer.WriteHeader(401)
			return
		}

		signaling.HandleWebSocket(hub, c.Writer, c.Request, userID, log)
	})

	server := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.HTTPReadTimeoutSeconds) * time.Second,
		WriteTimeout: time.Duration(cfg.HTTPWriteTimeoutSeconds) * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 定期清理过期的踢出封禁记录，避免 kick_bans 表无限增长
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := roomRepo.RemoveExpiredKicks(); err != nil {
					log.Warn("RemoveExpiredKicks failed", zap.Error(err))
				}
			}
		}
	}()

	go func() {
		<-ctx.Done()
		// 停机前通知所有在线客户端（前端提示后停止重连）
		hub.NotifyShutdown("server is shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("Failed to shutdown API server", zap.Error(err))
		}
	}()

	log.Info("Server starting on port " + cfg.ServerPort)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("Failed to start server", zap.Error(err))
	}
}

func originAllowed(r *http.Request, allowedOrigins []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range allowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}
