package repository_test

// 真实 Postgres 上的仓库层集成测试（CI 提供 service 容器；本地未设置 TEST_DATABASE_DSN 时跳过）。

import (
	"fmt"
	"testing"
	"time"

	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/testutil"

	"gorm.io/gorm"
)

func seedUser(t *testing.T, gdb *gorm.DB, name string) *models.User {
	t.Helper()
	u := &models.User{
		Username: name,
		Email:    name + "@example.com",
		Phone:    fmt.Sprintf("1%010d", hash(name)%10_000_000_000),
		Password: "hashed",
		Nickname: name,
		Status:   models.UserStatusActive,
	}
	if err := repository.NewUserRepository(gdb).Create(u); err != nil {
		t.Fatalf("seed user %s: %v", name, err)
	}
	return u
}

func seedRoom(t *testing.T, repo repository.RoomRepository, owner *models.User, roomID string, max int) *models.Room {
	t.Helper()
	room := &models.Room{RoomID: roomID, Name: "room-" + roomID, OwnerID: owner.ID, MaxMembers: max, AuthType: models.AuthTypeNone, InviteCode: roomID + "ic"}
	if err := repo.Create(room); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	return room
}

func hash(s string) int {
	h := 0
	for _, c := range s {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

func TestRoomMemberLifecycle(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	repo := repository.NewRoomRepository(gdb)
	owner := seedUser(t, gdb, "owner")
	alice := seedUser(t, gdb, "alice")
	room := seedRoom(t, repo, owner, "100001", 10)

	if err := repo.AddMember(&models.RoomMember{RoomID: room.ID, UserID: alice.ID, Role: models.RoleMember, JoinedAt: time.Now()}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := repo.GetMember(room.ID, alice.ID); err != nil {
		t.Fatalf("GetMember after add: %v", err)
	}
	if n, _ := repo.CountMembers(room.ID); n != 1 {
		t.Fatalf("CountMembers = %d, want 1", n)
	}

	// 离开：软删（left_at），活跃查询不可见，历史查询可见
	if err := repo.RemoveMember(room.ID, alice.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if _, err := repo.GetMember(room.ID, alice.ID); err == nil {
		t.Fatal("GetMember should fail after leave")
	}
	record, err := repo.GetAnyMember(room.ID, alice.ID)
	if err != nil || record.LeftAt == nil {
		t.Fatalf("GetAnyMember should return the left record, got %+v err=%v", record, err)
	}
	if n, _ := repo.CountMembers(room.ID); n != 0 {
		t.Fatalf("CountMembers after leave = %d, want 0", n)
	}

	// 重新加入：复用同一行
	record.LeftAt = nil
	if err := repo.UpdateMember(record); err != nil {
		t.Fatalf("UpdateMember rejoin: %v", err)
	}
	if n, _ := repo.CountMembers(room.ID); n != 1 {
		t.Fatalf("CountMembers after rejoin = %d, want 1", n)
	}
}

// 唯一索引：同一用户在同一房间不能有两条成员记录（并发入会的最后一道防线）。
func TestRoomMemberUniquePerRoom(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	repo := repository.NewRoomRepository(gdb)
	owner := seedUser(t, gdb, "owner")
	bob := seedUser(t, gdb, "bob")
	room := seedRoom(t, repo, owner, "100002", 10)

	first := &models.RoomMember{RoomID: room.ID, UserID: bob.ID, JoinedAt: time.Now()}
	if err := repo.AddMember(first); err != nil {
		t.Fatal(err)
	}
	second := &models.RoomMember{RoomID: room.ID, UserID: bob.ID, JoinedAt: time.Now()}
	if err := repo.AddMember(second); err == nil {
		t.Fatal("second AddMember for same (room, user) must violate the unique index")
	}
}

func TestKickBanExpiry(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	repo := repository.NewRoomRepository(gdb)
	owner := seedUser(t, gdb, "owner")
	carol := seedUser(t, gdb, "carol")
	room := seedRoom(t, repo, owner, "100003", 10)

	if err := repo.CreateKickBan(&models.KickBan{RoomID: room.ID, UserID: carol.ID, KickedAt: time.Now(), ExpiresAt: time.Now().Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if kicked, err := repo.IsKicked(room.ID, carol.ID); err != nil || !kicked {
		t.Fatalf("IsKicked = %v, %v; want true", kicked, err)
	}

	// 过期的封禁不再生效，并会被清理
	if err := gdb.Exec("UPDATE kick_bans SET expires_at = NOW() - INTERVAL '1 minute'").Error; err != nil {
		t.Fatal(err)
	}
	if kicked, _ := repo.IsKicked(room.ID, carol.ID); kicked {
		t.Fatal("expired ban must not block the user")
	}
	if err := repo.RemoveExpiredKicks(); err != nil {
		t.Fatal(err)
	}
	var remaining int64
	gdb.Model(&models.KickBan{}).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("expired bans not removed: %d left", remaining)
	}
}

func TestAuditLogsNewestFirst(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	repo := repository.NewRoomRepository(gdb)
	owner := seedUser(t, gdb, "owner")
	room := seedRoom(t, repo, owner, "100004", 10)

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 3; i++ {
		if err := repo.CreateAuditLog(&models.AuditLog{RoomID: room.ID, UserID: owner.ID, Action: fmt.Sprintf("a%d", i), CreatedAt: base.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := repo.GetAuditLogs(room.ID, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Action != "a2" || logs[1].Action != "a1" {
		t.Fatalf("GetAuditLogs order/limit wrong: %+v", logs)
	}
}

func TestRoomLockForUpdateAndCurrentSize(t *testing.T) {
	gdb := testutil.OpenTestDB(t, "repository")
	repo := repository.NewRoomRepository(gdb)
	owner := seedUser(t, gdb, "owner")
	room := seedRoom(t, repo, owner, "100005", 10)

	tx := repo.Begin()
	locked, err := tx.LockRoomForUpdate(room.ID)
	if err != nil {
		t.Fatalf("LockRoomForUpdate: %v", err)
	}
	if locked.ID != room.ID {
		t.Fatal("locked wrong room")
	}
	if err := tx.UpdateCurrentSize(room.ID, 7); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(room.ID)
	if got.CurrentSize != 7 {
		t.Fatalf("CurrentSize = %d, want 7", got.CurrentSize)
	}
	// 只更新了人数列：其它字段保持原样
	if got.Name != room.Name || got.MaxMembers != 10 {
		t.Fatal("UpdateCurrentSize must not touch other columns")
	}
}
