package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger 创建生产环境用的结构化 JSON 日志器
//
// 预期正确逻辑：
//
//	生产环境使用 JSON 格式输出，便于日志收集系统（如 ELK/Loki）解析。
//	时间戳使用 ISO8601 格式，保证时区一致性。
//	极简测试页面在调试时可通过 SFU 服务的 JSON 日志观察 ICE 候选生成、轨道订阅等事件。
func NewLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	return logger
}

// NewDevelopmentLogger 创建开发环境用的彩色控制台日志器
//
// 预期正确逻辑：
//
//	开发环境使用人类可读的彩色控制台输出，日志级别用大写颜色区分，便于本地调试时快速定位问题。
func NewDevelopmentLogger() *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	return logger
}
