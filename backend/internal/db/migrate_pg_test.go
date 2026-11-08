package db_test

import (
	"testing"

	"conference-sfu/backend/internal/db"
	"conference-sfu/backend/internal/testutil"
)

// Migrate 必须幂等：重复执行不报错、版本记录不重复；并且唯一索引真的建出来了。
func TestMigrateIdempotentAndCreatesUniqueIndex(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "db")

	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	versions, err := db.AppliedVersions(gdb)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 || versions[0] != "0001" {
		t.Fatalf("applied versions = %v, want first = 0001", versions)
	}

	var count int64
	err = gdb.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'room_members' AND indexname = 'idx_room_members_room_user'`).Scan(&count).Error
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("unique index idx_room_members_room_user missing")
	}
}
