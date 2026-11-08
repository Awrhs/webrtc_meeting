package db

import (
	"strings"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	sql := `
-- 开头注释
CREATE TABLE a (
    id SERIAL PRIMARY KEY -- 行尾注释
);

CREATE INDEX IF NOT EXISTS idx_a ON a(id);
-- 只有注释的行
`
	got := SplitStatements(sql)
	if len(got) != 2 {
		t.Fatalf("want 2 statements, got %d: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE TABLE a") || strings.Contains(got[0], "--") {
		t.Errorf("statement 0 malformed: %q", got[0])
	}
	if got[1] != "CREATE INDEX IF NOT EXISTS idx_a ON a(id)" {
		t.Errorf("statement 1 = %q", got[1])
	}
}

func TestVersionOf(t *testing.T) {
	cases := map[string]string{
		"0001_init.up.sql": "0001",
		"0002_x.sql":       "0002",
		"0003.sql":         "0003",
	}
	for name, want := range cases {
		if got := versionOf(name); got != want {
			t.Errorf("versionOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// 内嵌的迁移文件必须存在且按版本排序；每个文件都能被拆成至少一条语句。
func TestEmbeddedMigrationsWellFormed(t *testing.T) {
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no embedded migrations found")
	}
	prev := ""
	for _, name := range names {
		if versionOf(name) <= prev {
			t.Errorf("migration %s not strictly increasing after %s", name, prev)
		}
		prev = versionOf(name)
		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if len(SplitStatements(string(content))) == 0 {
			t.Errorf("migration %s has no statements", name)
		}
	}
}
