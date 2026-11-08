package main

import (
	"conference-sfu/backend/internal/auth"
	"conference-sfu/backend/internal/config"
	migrations "conference-sfu/backend/internal/db"
	"conference-sfu/backend/internal/handlers"
	"conference-sfu/backend/internal/mail"
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
	// 启动时应用内嵌的 SQL 迁移（幂等）。不再依赖 Postgres 容器首次初始化时挂载的 init.sql：
	// 那种方式只在数据卷为空时生效，之后任何改表都不会应用到已有库。
	if err := migrations.Migrate(db); err != nil {
		log.Fatal("Failed to run database migrations", zap.Error(err))
	}

	redisClient := repository.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, log)

	userRepo := repository.NewUserRepository(db)
	roomRepo := repository.NewRoomRepository(db)
	chatRepo := repository.NewChatRepository(db)

	// 邮件发送器：局域网/开发默认 log（验证码打到服务端日志），生产配置 SMTP
	var mailer mail.Mailer
	if cfg.MailProvider == "smtp" {
		mailer = &mail.SMTPMailer{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			TLSMode:  cfg.SMTPTLSMode,
		}
	} else {
		mailer = &mail.LogMailer{Logger: log}
	}
	userService := service.NewUserService(userRepo, redisClient, service.UserServiceConfig{
		JWTSecret:                cfg.JWTSecret,
		JWTExpiry:                time.Duration(cfg.JWTExpiryHours) * time.Hour,
		Mailer:                   mailer,
		RequireEmailVerification: cfg.RequireEmailVerification,
		AppName:                  cfg.AppName,
	}, log)
	// 登录 token 校验：签名 → Redis 会话仍有效 → token 版本未被改密码作废
	tokenValidator := &auth.Validator{Secret: cfg.JWTSecret, Sessions: redisClient, Versions: userService}
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
		api.POST("/auth/password/forgot", authLimiter.Gin(), handler.ForgotPassword)
		api.POST("/auth/password/reset", authLimiter.Gin(), handler.ResetPassword)
		api.POST("/auth/email/verify", authLimiter.Gin(), handler.VerifyEmail)
		api.POST("/auth/email/resend", authLimiter.Gin(), handler.ResendVerification)

		authed := api.Group("/")
		authed.Use(apiLimiter.Gin(), handlers.AuthMiddleware(tokenValidator))
		{
			authed.POST("/auth/logout", handler.Logout)
			authed.GET("/user/me", handler.GetMe)
			authed.PUT("/user/profile", handler.UpdateProfile)
			authed.PUT("/user/password", handler.UpdatePassword)
			authed.GET("/user/devices", handler.GetMyDevices)
			authed.DELETE("/user/devices/:id", handler.DeleteMyDevice)

			authed.POST("/rooms", handler.CreateRoom)
			authed.GET("/rooms", handler.GetMyRooms)
			authed.GET("/rooms/:id", handler.GetRoom)
			authed.GET("/rooms/number/:number", handler.GetRoomByNumber)
			authed.POST("/rooms/:id/join", handler.JoinRoom)
			authed.POST("/rooms/:id/sfu-token", handler.GetSFUToken)
			authed.DELETE("/rooms/:id", handler.DeleteRoom)
			authed.POST("/rooms/:id/lock", handler.LockRoom)
			authed.POST("/rooms/:id/unlock", handler.UnlockRoom)

			authed.POST("/rooms/:id/members/:userId/kick", handler.KickMember)
			authed.POST("/rooms/:id/members/:userId/mute", handler.MuteMember)
			authed.POST("/rooms/:id/members/:userId/unmute", handler.UnmuteMember)
			authed.POST("/rooms/:id/members/:userId/admin", handler.SetAdmin)
			authed.DELETE("/rooms/:id/members/:userId/admin", handler.RemoveAdmin)
			authed.POST("/rooms/:id/members/:userId/transfer", handler.TransferOwner)

			authed.GET("/rooms/:id/messages", handler.GetMessages)
			authed.POST("/rooms/:id/messages", handler.SendMessage)
			authed.GET("/rooms/:id/messages/private/:userId", handler.GetPrivateMessages)
			authed.POST("/rooms/:id/upload", handler.UploadFile)
			authed.GET("/rooms/:id/uploads/:name", handler.GetUploadedFile)

			authed.GET("/rooms/:id/audit-logs", handler.GetAuditLogs)

			authed.PUT("/rooms/:id/media-state", handler.UpdateMediaState)

			// 离开会议与实时在线状态
			authed.POST("/rooms/:id/leave", handler.LeaveRoom)
			authed.GET("/rooms/:id/presence", handler.GetPresence)

			// E2EE 端到端加密密钥交换
			authed.POST("/e2ee/public-key", handler.UploadPublicKey)
			authed.GET("/e2ee/public-key/:user_id", handler.GetPublicKey)
			authed.DELETE("/e2ee/public-key", handler.DeletePublicKey)
		}
		// Callback for SFU to notify peer connection failed
		api.POST("/sfu/callback/:roomId/:userId/connection-failed", handler.NotifyConnectionFailed)
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// 兼容旧消息里的 /uploads/<name> 链接（按房间分目录之前上传的文件）：仅登录可访问。
	// 新上传文件走 /api/v1/rooms/:id/uploads/:name，额外校验房间成员身份。
	r.GET("/uploads/:name", handlers.AuthMiddleware(tokenValidator), func(c *gin.Context) {
		name := c.Param("name")
		if !handlers.IsSafeUploadName(name) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file name"})
			return
		}
		handlers.ServeUpload(c, filepath.Join(cfg.UploadDir, name), name)
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

		claims, err := tokenValidator.Validate(token)
		if err != nil {
			c.Writer.WriteHeader(401)
			return
		}

		signaling.HandleWebSocket(hub, c.Writer, c.Request, claims.UserID, log)
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
