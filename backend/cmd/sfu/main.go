package main

import (
	"conference-sfu/backend/internal/config"
	"conference-sfu/backend/internal/middleware"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/pkg/logger"
	"context"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/cors"
	"go.uber.org/zap"
)

// main 是 SFU 媒体服务器的入口函数
//
// 预期正确逻辑：
//
//	SFU（Selective Forwarding Unit）是独立的媒体服务器进程，专门负责音视频流的接收与转发。
//	它不依赖 API 服务器的数据库或业务逻辑，只需要：
//	1) 加载配置（端口、ICE 服务器地址等）
//	2) 创建 SFUServer（内含 Pion WebRTC API 和动态 ICE 配置）
//	3) 启动 gRPC 控制服务器（供 API 服务器远程调用 kick/mute/stats 等管理命令）
//	4) 启动 WebSocket HTTP 服务器，处理前端发来的信令消息（join/offer/ice-candidate/answer）
//
//	极简测试页面 sfu-test.html 直接连接到此服务的 WebSocket 端口（默认 8081），
//	不经过 API 服务器，因此只要此进程正常运行，测试页面即可进行 WebRTC 协商和媒体转发验证。
func main() {
	cfg := config.Load()
	log := logger.NewLogger()

	// 创建 SFU 核心服务器：初始化 Pion WebRTC API、ICE 配置、房间/Peer 管理
	sfuServer := sfu.NewSFUServer(log, sfu.ServerOptions{
		SFUJWTSecret:            cfg.SFUJWTSecret,
		AllowUnauthenticatedDev: cfg.SFUAllowUnauthenticatedDev,
		AllowedOrigins:          cfg.AllowedOrigins,
		CallbackToken:           cfg.SFUCallbackToken,
		MessageRateLimit:        cfg.RateLimitSFUMsgPerMinute,
	})

	// 启动 gRPC 控制服务器（保存引用用于优雅关闭）
	// 注意：极简测试页面不直接调用 gRPC，这是给 API 服务器用的管理通道
	grpcServer, err := sfu.StartGRPCServer(sfuServer, cfg.SFUGRPCPort, cfg.SFUControlToken, log)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}

	mux := http.NewServeMux()
	wsLimiter := middleware.NewIPLimiter(cfg.RateLimitSFUWSPerMinute, time.Minute)
	defer wsLimiter.Stop()
	// 注册 WebSocket 处理器：前端测试页面通过此端点与 SFU 建立信令连接
	mux.Handle("/ws", wsLimiter.HTTP(http.HandlerFunc(sfuServer.HandleWebSocket)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/metrics.json", sfuServer.HandleMetrics)

	// 添加 CORS 中间件：允许前端页面跨域连接
	c := cors.New(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	server := &http.Server{
		Addr:         ":" + cfg.SFUPort,
		Handler:      c.Handler(mux),
		ReadTimeout:  time.Duration(cfg.HTTPReadTimeoutSeconds) * time.Second,
		WriteTimeout: time.Duration(cfg.HTTPWriteTimeoutSeconds) * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()
		grpcServer.GracefulStop()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("Failed to shutdown SFU server", zap.Error(err))
		}
	}()

	log.Info("SFU server starting on port " + cfg.SFUPort)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("Failed to start SFU server", zap.Error(err))
	}
}
