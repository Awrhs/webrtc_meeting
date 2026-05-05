package main

import (
	"conference-sfu/backend/internal/config"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/pkg/logger"
	"net/http"

	"github.com/rs/cors"
	"go.uber.org/zap"
)

func main() {
	cfg := config.Load()
	log := logger.NewLogger()

	sfuServer := sfu.NewSFUServer(log)

	// Start gRPC control server
	_, err := sfu.StartGRPCServer(sfuServer, cfg.SFUGRPCPort, log)
	if err != nil {
		log.Fatal("Failed to start gRPC server", zap.Error(err))
	}

	http.HandleFunc("/offer", sfuServer.HandleOffer)
	http.HandleFunc("/ws", sfuServer.HandleWebSocket)

	// Add CORS middleware
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