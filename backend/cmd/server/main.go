package main

import (
	"conference-sfu/backend/internal/config"
	"conference-sfu/backend/internal/handlers"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/service"
	"conference-sfu/backend/internal/signaling"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/pkg/logger"

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

	redisClient := repository.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword)

	userRepo := repository.NewUserRepository(db)
	roomRepo := repository.NewRoomRepository(db)
	chatRepo := repository.NewChatRepository(db)

	userService := service.NewUserService(userRepo, redisClient, cfg.JWTSecret)
	roomService := service.NewRoomService(roomRepo, userRepo, redisClient)
	chatService := service.NewChatService(chatRepo, roomRepo)

	// Connect to SFU gRPC control server
	sfuClient, err := sfu.NewControlClient(cfg.SFUAddr, log)
	if err != nil {
		log.Warn("Failed to connect to SFU gRPC server, running without SFU control", zap.Error(err))
	} else {
		defer sfuClient.Close()
		log.Info("Connected to SFU gRPC server successfully", zap.String("addr", cfg.SFUAddr))
	}

	hub := signaling.NewHub(roomService, chatService, log)
	go hub.Run()

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	handler := handlers.NewHandler(userService, roomService, chatService, hub, sfuClient, log)

	api := r.Group("/api/v1")
	{
		api.POST("/auth/register", handler.Register)
		api.POST("/auth/login", handler.Login)

		auth := api.Group("/")
		auth.Use(handlers.AuthMiddleware(cfg.JWTSecret))
		{
			auth.GET("/user/me", handler.GetMe)
			auth.PUT("/user/profile", handler.UpdateProfile)
			auth.PUT("/user/password", handler.UpdatePassword)

			auth.POST("/rooms", handler.CreateRoom)
			auth.GET("/rooms", handler.GetMyRooms)
			auth.GET("/rooms/:id", handler.GetRoom)
			auth.POST("/rooms/:id/join", handler.JoinRoom)
			auth.DELETE("/rooms/:id", handler.DeleteRoom)
			auth.POST("/rooms/:id/lock", handler.LockRoom)

			auth.POST("/rooms/:id/members/:userId/kick", handler.KickMember)
			auth.POST("/rooms/:id/members/:userId/mute", handler.MuteMember)
			auth.POST("/rooms/:id/members/:userId/unmute", handler.UnmuteMember)
			auth.POST("/rooms/:id/members/:userId/admin", handler.SetAdmin)
			auth.DELETE("/rooms/:id/members/:userId/admin", handler.RemoveAdmin)
			auth.POST("/rooms/:id/members/:userId/transfer", handler.TransferOwner)

			auth.GET("/rooms/:id/messages", handler.GetMessages)
			auth.POST("/rooms/:id/messages", handler.SendMessage)
		}
		// Callback for SFU to notify peer connection failed
		api.POST("/sfu/callback/:roomId/:userId/connection-failed", handler.NotifyConnectionFailed)
	}

	r.GET("/ws", func(c *gin.Context) {
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

	log.Info("Server starting on port " + cfg.ServerPort)
	if err = r.Run(":" + cfg.ServerPort); err != nil {
		log.Fatal("Failed to start server", zap.Error(err))
	}
}