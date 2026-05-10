package main

import (
	"conference-sfu/backend/internal/config"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/pkg/logger"
	"net/http"

	"github.com/rs/cors"
	"go.uber.org/zap"
)

// main 是 SFU 媒体服务器的入口函数
//
// 预期正确逻辑：
//   SFU（Selective Forwarding Unit）是独立的媒体服务器进程，专门负责音视频流的接收与转发。
//   它不依赖 API 服务器的数据库或业务逻辑，只需要：
//   1) 加载配置（端口、ICE 服务器地址等）
//   2) 创建 SFUServer（内含 Pion WebRTC API 和动态 ICE 配置）
//   3) 启动 gRPC 控制服务器（供 API 服务器远程调用 kick/mute/stats 等管理命令）
//   4) 启动 WebSocket HTTP 服务器，处理前端发来的信令消息（join/offer/ice-candidate/answer）
//
//   极简测试页面 sfu-test.html 直接连接到此服务的 WebSocket 端口（默认 8081），
//   不经过 API 服务器，因此只要此进程正常运行，测试页面即可进行 WebRTC 协商和媒体转发验证。
func main() {
	cfg := config.Load()
	log := logger.NewLogger()

	// 创建 SFU 核心服务器：初始化 Pion WebRTC API、ICE 配置、房间/Peer 管理
	sfuServer := sfu.NewSFUServer(log)

	// 启动 gRPC 控制服务器（保存引用用于优雅关闭）
	// 注意：极简测试页面不直接调用 gRPC，这是给 API 服务器用的管理通道
	grpcServer, err := sfu.StartGRPCServer(sfuServer, cfg.SFUGRPCPort, log)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}
	defer grpcServer.GracefulStop()

	// 注册 WebSocket 处理器：前端测试页面通过此端点与 SFU 建立信令连接
	http.HandleFunc("/ws", sfuServer.HandleWebSocket)

	// 添加 CORS 中间件：允许前端页面（localhost:3000）跨域连接
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	handler := c.Handler(http.DefaultServeMux)

	log.Info("SFU server starting on port " + cfg.SFUPort)
	if err := http.ListenAndServe(":"+cfg.SFUPort, handler); err != nil {
		log.Fatal("Failed to start SFU server", zap.Error(err))
	}
}
