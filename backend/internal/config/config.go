package config

import (
	"os"
	"strconv"
)

type Config struct {
	ServerPort     string
	SFUPort        string
	DatabaseDSN    string
	RedisAddr      string
	RedisPassword  string
	JWTSecret      string
	JWTExpiryHours int
	CoturnURL      string
	CoturnUser     string
	CoturnPassword string
}

func Load() *Config {
	port := getEnv("SERVER_PORT", "8080")
	sfuPort := getEnv("SFU_PORT", "8081")

	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPass := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "conference")

	dsn := "host=" + dbHost + " user=" + dbUser + " password=" + dbPass + " dbname=" + dbName + " port=" + dbPort + " sslmode=disable"

	return &Config{
		ServerPort:     port,
		SFUPort:        sfuPort,
		DatabaseDSN:    dsn,
		RedisAddr:      getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:  getEnv("REDIS_PASSWORD", ""),
		JWTSecret:      getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		JWTExpiryHours: getEnvAsInt("JWT_EXPIRY_HOURS", 168),
		CoturnURL:      getEnv("COTURN_URL", "turn:localhost:3478"),
		CoturnUser:     getEnv("COTURN_USER", "username"),
		CoturnPassword: getEnv("COTURN_PASSWORD", "password"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}