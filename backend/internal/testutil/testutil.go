// Package testutil 为集成测试提供真实的 Postgres / Redis 连接。
//
// 依赖环境变量（CI 由 service 容器提供；本地未设置时测试自动跳过）：
//
//	TEST_DATABASE_DSN   不含 dbname 的 libpq 连接串，如 "host=localhost port=5432 user=postgres password=postgres sslmode=disable"
//	TEST_REDIS_ADDR     如 "localhost:6379"
//
// go test ./... 会并行运行多个包，若它们共用一个数据库会互相清表。
// 因此每个调用方传入自己的名字，OpenTestDB 为其创建独立的数据库 conference_test_<name>。
package testutil

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"conference-sfu/backend/internal/db"
	"conference-sfu/backend/internal/repository"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenTestDB 连接（必要时创建）名为 conference_test_<name> 的数据库，应用迁移并清空全部业务表。
func OpenTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("TEST_DATABASE_DSN")
	if baseDSN == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping database integration test")
	}
	if strings.Contains(baseDSN, "dbname=") {
		t.Fatal("TEST_DATABASE_DSN must not contain dbname= (a database is created per test package)")
	}
	dbName := "conference_test_" + sanitizeIdent(name)

	admin, err := gorm.Open(postgres.Open(baseDSN+" dbname=postgres"), silentConfig())
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	// CREATE DATABASE 不支持 IF NOT EXISTS：先查再建
	var exists int64
	if err := admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", dbName).Scan(&exists).Error; err != nil {
		t.Fatalf("check database: %v", err)
	}
	if exists == 0 {
		if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil && !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("create database %s: %v", dbName, err)
		}
	}
	if sqlDB, err := admin.DB(); err == nil {
		_ = sqlDB.Close()
	}

	gdb, err := gorm.Open(postgres.Open(baseDSN+" dbname="+dbName), silentConfig())
	if err != nil {
		t.Fatalf("connect %s: %v", dbName, err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate %s: %v", dbName, err)
	}
	TruncateAll(t, gdb)
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

// TruncateAll 清空全部业务表并重置自增序列（schema_migrations 保留）。
func TruncateAll(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	err := gdb.Exec(`TRUNCATE TABLE audit_logs, e2ee_public_keys, messages, kick_bans, room_members, rooms, devices, users RESTART IDENTITY CASCADE`).Error
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

// OpenTestRedis 连接 TEST_REDIS_ADDR 指向的 Redis 并清空当前库。
func OpenTestRedis(t *testing.T) *repository.RedisClient {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set; skipping redis-dependent integration test")
	}
	rc := repository.NewRedisClient(addr, os.Getenv("TEST_REDIS_PASSWORD"), zap.NewNop())
	if err := rc.Ping(); err != nil {
		t.Fatalf("redis ping %s: %v", addr, err)
	}
	if err := rc.FlushDB(); err != nil {
		t.Fatalf("redis flushdb: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return rc
}

func silentConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

func sanitizeIdent(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return fmt.Sprintf("pkg_%d", os.Getpid())
	}
	return b.String()
}
