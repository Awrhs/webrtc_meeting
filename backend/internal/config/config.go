package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
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

func loadEnvFromFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			os.Setenv(key, value)
		}
	}
}

func Load() *Config {
	// 只有在开发环境（GO_ENV 不是 production）且 .env 文件存在时，才自动加载
	goEnv := getEnv("GO_ENV", "development")
	if goEnv != "production" {
		loadEnvFromFile(".env")
	}

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