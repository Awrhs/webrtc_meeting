package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config 聚合了后端服务（API 服务器 + SFU 服务器）所需的全部配置项
//
// 预期正确逻辑：
//   极简测试页面只依赖 SFU 相关的配置（SFUPort、CoturnURL 等），
//   但 Config 结构体同时承载了 API 服务器的配置，因为两个服务共用同一套配置加载逻辑。
//   开发环境下会自动加载 .env 文件，生产环境则依赖容器注入的环境变量。
type Config struct {
	ServerPort     string
	SFUPort        string
	SFUGRPCPort    string
	SFUAddr        string
	DatabaseDSN    string
	RedisAddr      string
	RedisPassword  string
	JWTSecret      string
	JWTExpiryHours int
	CoturnURL      string
	CoturnUser     string
	CoturnPassword string
}

func loadEnvFromFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
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
	return nil
}

// Load 从环境变量加载配置，开发环境会自动读取 .env 文件
//
// 预期正确逻辑：
//   1) 非生产环境时尝试加载当前目录或父目录的 .env 文件
//   2) 从环境变量读取各配置项，若未设置则使用默认值
//   3) 构建 PostgreSQL DSN 连接字符串
//   对极简测试页面而言，最关键的默认值是 SFUPort=8081（SFU WebSocket 监听端口）
func Load() *Config {
	// 只有在开发环境（GO_ENV 不是 production）且 .env 文件存在时，才自动加载
	goEnv := getEnv("GO_ENV", "development")
	if goEnv != "production" {
		// 尝试当前目录，再尝试父目录（支持 go run 在 backend 目录运行）
		if err := loadEnvFromFile(".env"); err != nil {
			loadEnvFromFile("../.env")
		}
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
		SFUGRPCPort:    getEnv("SFU_GRPC_PORT", "50051"),
		SFUAddr:        getEnv("SFU_ADDR", "localhost:50051"),
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
