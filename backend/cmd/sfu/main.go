package main

import (
	"conference-sfu/backend/internal/config"
	"conference-sfu/backend/internal/sfu"
	"conference-sfu/backend/pkg/logger"
	"net/http"
)

func main() {
	cfg := config.Load()
	log := logger.NewLogger()

	sfuServer := sfu.NewSFUServer(log)

	http.HandleFunc("/offer", sfuServer.HandleOffer)
	http.HandleFunc("/ws", sfuServer.HandleWebSocket)

	log.Info("SFU server starting on port " + cfg.SFUPort)
	if err := http.ListenAndServe(":"+cfg.SFUPort, nil); err != nil {
		log.Fatal("Failed to start SFU server", err)
	}
}