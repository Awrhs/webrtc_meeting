package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 聚合了后端服务（API 服务器 + SFU 服务器）所需的全部配置项
//
// 预期正确逻辑：
//
//	极简测试页面只依赖 SFU 相关的配置（SFUPort、CoturnURL 等），
//	但 Config 结构体同时承载了 API 服务器的配置，因为两个服务共用同一套配置加载逻辑。
//	开发环境下会自动加载 .env 文件，生产环境则依赖容器注入的环境变量。
type Config struct {
	GOEnv                      string
	ServerPort                 string
	SFUPort                    string
	SFUGRPCPort                string
	SFUAddr                    string
	SFUInstanceID              string
	SFUGRPCAddr                string
	SFUPublicWSURL             string
	SFURecordingDir            string
	DatabaseDSN                string
	RedisAddr                  string
	RedisPassword              string
	JWTSecret                  string
	SFUJWTSecret               string
	SFUControlToken            string
	SFUCallbackToken           string
	JWTExpiryHours             int
	SFUTokenExpiryMinutes      int
	CoturnURL                  string
	CoturnUser                 string
	CoturnPassword             string
	AllowedOrigins             []string
	SFUAllowUnauthenticatedDev bool
	HTTPReadTimeoutSeconds     int
	HTTPWriteTimeoutSeconds    int
	ShutdownTimeoutSeconds     int
	RateLimitAuthPerMinute     int
	RateLimitAPIPerMinute      int
	RateLimitWSPerMinute       int
	RateLimitSFUWSPerMinute    int
	RateLimitSFUMsgPerMinute   int
	UploadDir                  string
	UploadMaxBytes             int
	PresenceGraceSeconds       int
	// 邮件发送：MailProvider 取 log（打到日志，开发/局域网默认）或 smtp；
	// SMTPTLSMode 取 starttls（587）、implicit（465）或 none（仅本机调试）
	MailProvider             string
	SMTPHost                 string
	SMTPPort                 int
	SMTPUsername             string
	SMTPPassword             string
	SMTPFrom                 string
	SMTPTLSMode              string
	RequireEmailVerification bool
	AppName                  string
	// SFUHTTPURL：API 服务访问 SFU HTTP 接口（录制列表/下载代理）的内网地址
	SFUHTTPURL string
	// MetricsToken：非空时 /metrics 需要 Bearer token（Prometheus bearer_token）
	MetricsToken string
	// 下发给浏览器的 ICE 配置：STUN 列表、TURN 列表，以及 coturn --use-auth-secret 的共享密钥
	// （API 据此签发时限 TURN 凭据，不再把固定账号密码打进前端）
	STUNURLs                 []string
	TURNURLs                 []string
	TURNStaticAuthSecret     string
	TURNCredentialTTLSeconds int
	// SFUPublicIPs：SFU 对外可达的 IP（云主机的弹性公网 IP、局域网部署时的宿主机 IP），
	// 以 NAT 1:1 的方式写进 ICE host 候选；容器内只有私网地址时必须配置
	SFUPublicIPs []string
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
//  1. 非生产环境时尝试加载当前目录或父目录的 .env 文件
//  2. 从环境变量读取各配置项，若未设置则使用默认值
//  3. 构建 PostgreSQL DSN 连接字符串
//     对极简测试页面而言，最关键的默认值是 SFUPort=8081（SFU WebSocket 监听端口）
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

	jwtSecret := getEnv("JWT_SECRET", "your-secret-key-change-in-production")
	sfuJWTSecret := getEnv("SFU_JWT_SECRET", jwtSecret)
	cfg := &Config{
		GOEnv:                      goEnv,
		ServerPort:                 port,
		SFUPort:                    sfuPort,
		SFUGRPCPort:                getEnv("SFU_GRPC_PORT", "50051"),
		SFUAddr:                    getEnv("SFU_ADDR", "localhost:50051"),
		SFUInstanceID:              getEnv("SFU_INSTANCE_ID", ""),
		SFUGRPCAddr:                getEnv("SFU_GRPC_ADDR", ""),
		SFUPublicWSURL:             getEnv("SFU_PUBLIC_WS_URL", "ws://localhost:"+sfuPort),
		SFURecordingDir:            getEnv("SFU_RECORDING_DIR", ""),
		DatabaseDSN:                dsn,
		RedisAddr:                  getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:              getEnv("REDIS_PASSWORD", ""),
		JWTSecret:                  jwtSecret,
		SFUJWTSecret:               sfuJWTSecret,
		SFUControlToken:            getEnv("SFU_CONTROL_TOKEN", "dev-sfu-control-token"),
		SFUCallbackToken:           getEnv("SFU_CALLBACK_TOKEN", "dev-sfu-callback-token"),
		JWTExpiryHours:             getEnvAsInt("JWT_EXPIRY_HOURS", 168),
		SFUTokenExpiryMinutes:      getEnvAsInt("SFU_TOKEN_EXPIRY_MINUTES", 5),
		CoturnURL:                  getEnv("COTURN_URL", "turn:localhost:3478"),
		CoturnUser:                 getEnv("COTURN_USER", "username"),
		CoturnPassword:             getEnv("COTURN_PASSWORD", "password"),
		AllowedOrigins:             getEnvList("ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000,http://localhost:5173,http://127.0.0.1:5173"),
		SFUAllowUnauthenticatedDev: getEnvAsBool("SFU_ALLOW_UNAUTHENTICATED_DEV", false),
		HTTPReadTimeoutSeconds:     getEnvAsInt("HTTP_READ_TIMEOUT_SECONDS", 10),
		HTTPWriteTimeoutSeconds:    getEnvAsInt("HTTP_WRITE_TIMEOUT_SECONDS", 10),
		ShutdownTimeoutSeconds:     getEnvAsInt("SHUTDOWN_TIMEOUT_SECONDS", 15),
		RateLimitAuthPerMinute:     getEnvAsInt("RATE_LIMIT_AUTH_PER_MINUTE", 5),
		RateLimitAPIPerMinute:      getEnvAsInt("RATE_LIMIT_API_PER_MINUTE", 120),
		RateLimitWSPerMinute:       getEnvAsInt("RATE_LIMIT_WS_PER_MINUTE", 20),
		RateLimitSFUWSPerMinute:    getEnvAsInt("RATE_LIMIT_SFU_WS_PER_MINUTE", 20),
		RateLimitSFUMsgPerMinute:   getEnvAsInt("RATE_LIMIT_SFU_MSG_PER_MINUTE", 600),
		UploadDir:                  getEnv("UPLOAD_DIR", "./uploads"),
		UploadMaxBytes:             getEnvAsInt("UPLOAD_MAX_BYTES", 20<<20),
		PresenceGraceSeconds:       getEnvAsInt("PRESENCE_GRACE_SECONDS", 90),
		MailProvider:               strings.ToLower(getEnv("MAIL_PROVIDER", "log")),
		SMTPHost:                   getEnv("SMTP_HOST", ""),
		SMTPPort:                   getEnvAsInt("SMTP_PORT", 587),
		SMTPUsername:               getEnv("SMTP_USERNAME", ""),
		SMTPPassword:               getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:                   getEnv("SMTP_FROM", ""),
		SMTPTLSMode:                strings.ToLower(getEnv("SMTP_TLS_MODE", "starttls")),
		RequireEmailVerification:   getEnvAsBool("REQUIRE_EMAIL_VERIFICATION", false),
		AppName:                    getEnv("APP_NAME", "SFU会议"),
		SFUHTTPURL:                 getEnv("SFU_HTTP_URL", "http://localhost:8081"),
		MetricsToken:               getEnv("METRICS_TOKEN", ""),
		STUNURLs:                   stunURLsFromEnv(),
		TURNURLs:                   getEnvList("TURN_URLS", ""),
		TURNStaticAuthSecret:       getEnv("TURN_STATIC_AUTH_SECRET", ""),
		TURNCredentialTTLSeconds:   getEnvAsInt("TURN_CREDENTIAL_TTL_SECONDS", 3600),
		SFUPublicIPs:               getEnvList("SFU_PUBLIC_IP", ""),
	}
	if err := cfg.Validate(); err != nil {
		panic(err)
	}
	return cfg
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

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := strings.ToLower(getEnv(key, ""))
	switch valueStr {
	case "true", "1", "yes", "y":
		return true
	case "false", "0", "no", "n":
		return false
	default:
		return defaultValue
	}
}

// stunURLsFromEnv 解析 STUN_URLS：未设置用国内默认；设为 none 表示不配置 STUN
// （纯内网/无外网部署时 STUN 不可达只会让 ICE gathering 白等几秒，host 候选已够用）。
func stunURLsFromEnv() []string {
	list := getEnvList("STUN_URLS", "stun:stun.qq.com:3478,stun:stun.miwifi.com:3478")
	if len(list) == 1 && strings.EqualFold(list[0], "none") {
		return []string{}
	}
	return list
}

func getEnvList(key string, defaultValue string) []string {
	value := getEnv(key, defaultValue)
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (c *Config) Validate() error {
	// 零值视为默认（测试里直接构造 Config{} 也应通过校验）
	if c.MailProvider == "" {
		c.MailProvider = "log"
	}
	if c.SMTPTLSMode == "" {
		c.SMTPTLSMode = "starttls"
	}
	switch c.MailProvider {
	case "log", "smtp":
	default:
		return fmt.Errorf("MAIL_PROVIDER must be log or smtp, got %q", c.MailProvider)
	}
	if c.MailProvider == "smtp" && (c.SMTPHost == "" || c.SMTPFrom == "") {
		return fmt.Errorf("SMTP_HOST and SMTP_FROM are required when MAIL_PROVIDER=smtp")
	}
	switch c.SMTPTLSMode {
	case "starttls", "implicit", "none":
	default:
		return fmt.Errorf("SMTP_TLS_MODE must be starttls, implicit or none, got %q", c.SMTPTLSMode)
	}
	if c.GOEnv != "production" {
		return nil
	}
	if c.RequireEmailVerification && c.MailProvider != "smtp" {
		return fmt.Errorf("REQUIRE_EMAIL_VERIFICATION needs MAIL_PROVIDER=smtp in production (codes would only go to the log)")
	}
	if c.JWTSecret == "your-secret-key-change-in-production" || len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be set to a strong value in production")
	}
	if len(c.SFUJWTSecret) < 32 {
		return fmt.Errorf("SFU_JWT_SECRET must be at least 32 bytes in production")
	}
	if len(c.AllowedOrigins) == 0 {
		return fmt.Errorf("ALLOWED_ORIGINS must be set in production")
	}
	for _, origin := range c.AllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("ALLOWED_ORIGINS cannot contain * in production")
		}
	}
	if c.SFUAllowUnauthenticatedDev {
		return fmt.Errorf("SFU_ALLOW_UNAUTHENTICATED_DEV cannot be enabled in production")
	}
	if c.SFUControlToken == "" || c.SFUControlToken == "dev-sfu-control-token" {
		return fmt.Errorf("SFU_CONTROL_TOKEN must be set in production")
	}
	if c.SFUCallbackToken == "" || c.SFUCallbackToken == "dev-sfu-callback-token" {
		return fmt.Errorf("SFU_CALLBACK_TOKEN must be set in production")
	}
	return nil
}
