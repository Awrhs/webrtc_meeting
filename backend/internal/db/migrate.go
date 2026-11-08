// Package db 提供内嵌 SQL 的数据库迁移器。
//
// 迁移文件放在 migrations/ 目录，文件名形如 0001_init.up.sql：
// 前缀数字是版本号，按文件名排序依次执行；已执行的版本记录在 schema_migrations 表里，
// 重复调用是幂等的。API 服务启动时调用 Migrate，不再依赖 Postgres 容器首次初始化时
// 挂载的 init.sql（那种方式对已有数据卷的后续改表完全无效）。
//
// 只用标准库 + gorm，不引入第三方迁移工具。
package db

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate 应用所有尚未执行的迁移。
func Migrate(gdb *gorm.DB) error {
	if err := gdb.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(64) PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		version := versionOf(name)
		var applied int64
		if err := gdb.Table("schema_migrations").Where("version = ?", version).Count(&applied).Error; err != nil {
			return fmt.Errorf("query schema_migrations: %w", err)
		}
		if applied > 0 {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		// 每个迁移文件在一个事务里执行：Postgres 的 DDL 是事务性的，失败整体回滚
		err = gdb.Transaction(func(tx *gorm.DB) error {
			for _, stmt := range SplitStatements(string(content)) {
				if err := tx.Exec(stmt).Error; err != nil {
					return fmt.Errorf("migration %s failed at statement %q: %w", name, firstLine(stmt), err)
				}
			}
			return tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version).Error
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// AppliedVersions 返回已执行的迁移版本（按版本号排序），供健康检查/测试使用。
func AppliedVersions(gdb *gorm.DB) ([]string, error) {
	var versions []string
	err := gdb.Table("schema_migrations").Order("version").Pluck("version", &versions).Error
	return versions, err
}

func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// versionOf 取文件名第一个下划线之前的部分作为版本号（0001_init.up.sql → 0001）。
func versionOf(name string) string {
	if idx := strings.Index(name, "_"); idx > 0 {
		return name[:idx]
	}
	return strings.TrimSuffix(name, ".sql")
}

// SplitStatements 把 SQL 文件拆成单条语句：去掉 -- 行注释后按分号切分。
// pgx 的扩展协议不允许一次 Exec 多条语句，必须逐条执行。
// 约定：迁移文件不使用函数体 / 美元引用 / 字符串中的分号。
func SplitStatements(sql string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	var statements []string
	for _, part := range strings.Split(cleaned.String(), ";") {
		if stmt := strings.TrimSpace(part); stmt != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}
