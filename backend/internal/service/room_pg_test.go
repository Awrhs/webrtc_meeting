package service_test

// roomService 在真实 Postgres + Redis 上的集成测试：
// 覆盖入会容量的并发正确性、同一用户并发重复入会、踢人封禁、转让房主、离开计数。

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/service"
	"conference-sfu/backend/internal/testutil"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type pgEnv struct {
	gdb      *gorm.DB
	roomRepo repository.RoomRepository
	userRepo repository.UserRepository
	rooms    service.RoomService
}

func newPGEnv(t *testing.T) *pgEnv {
	t.Helper()
	gdb := testutil.OpenTestDB(t, "service")
	rc := testutil.OpenTestRedis(t)
	roomRepo := repository.NewRoomRepository(gdb)
	userRepo := repository.NewUserRepository(gdb)
	svc := service.NewRoomService(roomRepo, userRepo, rc, "test-sfu-secret-at-least-32-bytes-long!", 5, zap.NewNop())
	return &pgEnv{gdb: gdb, roomRepo: roomRepo, userRepo: userRepo, rooms: svc}
}

func (e *pgEnv) user(t *testing.T, name string) *models.User {
	t.Helper()
	u := &models.User{Username: name, Email: name + "@example.com", Phone: phoneFor(name), Password: "x", Nickname: name, Status: models.UserStatusActive}
	if err := e.userRepo.Create(u); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return u
}

func phoneFor(name string) string {
	h := 0
	for _, c := range name {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return fmt.Sprintf("1%010d", h%10_000_000_000)
}

func (e *pgEnv) room(t *testing.T, owner *models.User, max int, auth models.AuthType, password string) *models.Room {
	t.Helper()
	room, err := e.rooms.CreateRoom(owner.ID, &models.CreateRoomRequest{Name: "room", MaxMembers: max, AuthType: auth, Password: password})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	return room
}

func roomKey(room *models.Room) string { return strconv.Itoa(int(room.ID)) }

// 10 个用户同时抢 2 个空位（上限 3，房主占 1）：恰好 2 个成功，人数不超员。
func TestJoinRoom_CapacityIsEnforcedUnderConcurrency(t *testing.T) {
	e := newPGEnv(t)
	owner := e.user(t, "owner")
	room := e.room(t, owner, 3, models.AuthTypeNone, "")

	users := make([]*models.User, 10)
	for i := range users {
		users[i] = e.user(t, fmt.Sprintf("joiner%d", i))
	}

	var wg sync.WaitGroup
	results := make(chan error, len(users))
	for _, u := range users {
		wg.Add(1)
		go func(id uint) {
			defer wg.Done()
			_, err := e.rooms.JoinRoom(id, roomKey(room), "")
			results <- err
		}(u.ID)
	}
	wg.Wait()
	close(results)

	success, full := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case err.Error() == "room is full":
			full++
		default:
			t.Fatalf("unexpected JoinRoom error: %v", err)
		}
	}
	if success != 2 || full != 8 {
		t.Fatalf("success=%d full=%d; want 2 and 8", success, full)
	}
	count, _ := e.roomRepo.CountMembers(room.ID)
	if count != 3 {
		t.Fatalf("active members = %d, want 3 (owner + 2)", count)
	}
	saved, _ := e.roomRepo.FindByID(room.ID)
	if saved.CurrentSize != 3 {
		t.Fatalf("CurrentSize = %d, want 3", saved.CurrentSize)
	}
}

// 同一用户并发连点"加入"：全部成功（幂等），成员表只有一行。
func TestJoinRoom_SameUserConcurrentIsIdempotent(t *testing.T) {
	e := newPGEnv(t)
	owner := e.user(t, "owner")
	room := e.room(t, owner, 50, models.AuthTypeNone, "")
	dup := e.user(t, "dup")

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.rooms.JoinRoom(dup.ID, roomKey(room), "")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent same-user join failed: %v", err)
		}
	}
	var rows int64
	e.gdb.Model(&models.RoomMember{}).Where("room_id = ? AND user_id = ?", room.ID, dup.ID).Count(&rows)
	if rows != 1 {
		t.Fatalf("member rows for user = %d, want exactly 1", rows)
	}
	count, _ := e.roomRepo.CountMembers(room.ID)
	if count != 2 {
		t.Fatalf("active members = %d, want 2", count)
	}
}

func TestJoinRoom_PasswordAndLock(t *testing.T) {
	e := newPGEnv(t)
	owner := e.user(t, "owner")
	guest := e.user(t, "guest")
	room := e.room(t, owner, 10, models.AuthTypePassword, "secret12")

	if _, err := e.rooms.JoinRoom(guest.ID, roomKey(room), ""); err == nil || err.Error() != "password required" {
		t.Fatalf("empty password: %v", err)
	}
	if _, err := e.rooms.JoinRoom(guest.ID, roomKey(room), "wrong"); err == nil || err.Error() != "invalid password" {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := e.rooms.JoinRoom(guest.ID, roomKey(room), "secret12"); err != nil {
		t.Fatalf("correct password: %v", err)
	}

	// 也可以用 6 位 room_id 入会
	if _, err := e.rooms.JoinRoom(guest.ID, room.RoomID, "secret12"); err != nil {
		t.Fatalf("join by 6-digit room_id: %v", err)
	}

	if err := e.rooms.LockRoom(room.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	outsider := e.user(t, "outsider")
	if _, err := e.rooms.JoinRoom(outsider.ID, roomKey(room), "secret12"); err == nil || err.Error() != "room is locked" {
		t.Fatalf("locked room must reject newcomers: %v", err)
	}
	// 已在房间的成员刷新重进不受锁房影响
	if _, err := e.rooms.JoinRoom(guest.ID, roomKey(room), "secret12"); err != nil {
		t.Fatalf("existing member must be able to re-join a locked room: %v", err)
	}
}

func TestKickMember_BansAndBlocksRejoin(t *testing.T) {
	e := newPGEnv(t)
	owner := e.user(t, "owner")
	victim := e.user(t, "victim")
	room := e.room(t, owner, 10, models.AuthTypeNone, "")
	if _, err := e.rooms.JoinRoom(victim.ID, roomKey(room), ""); err != nil {
		t.Fatal(err)
	}

	if err := e.rooms.KickMember(room.ID, owner.ID, victim.ID); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	if _, err := e.rooms.GetMemberRole(room.ID, victim.ID); err == nil {
		t.Fatal("kicked user must no longer be an active member")
	}
	if _, err := e.rooms.JoinRoom(victim.ID, roomKey(room), ""); err == nil {
		t.Fatal("kicked user must not be able to rejoin while banned")
	}
	saved, _ := e.roomRepo.FindByID(room.ID)
	if saved.CurrentSize != 1 {
		t.Fatalf("CurrentSize after kick = %d, want 1", saved.CurrentSize)
	}

	// 封禁过期后可以重新加入
	if err := e.gdb.Exec("UPDATE kick_bans SET expires_at = NOW() - INTERVAL '1 minute'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.rooms.JoinRoom(victim.ID, roomKey(room), ""); err != nil {
		t.Fatalf("rejoin after ban expiry: %v", err)
	}

	// 普通成员不能踢人；管理员不能踢房主
	if err := e.rooms.KickMember(room.ID, victim.ID, owner.ID); err == nil {
		t.Fatal("member kicking owner must be denied")
	}
}

func TestTransferOwner_SwapsRoles(t *testing.T) {
	e := newPGEnv(t)
	owner := e.user(t, "owner")
	heir := e.user(t, "heir")
	room := e.room(t, owner, 10, models.AuthTypeNone, "")
	if _, err := e.rooms.JoinRoom(heir.ID, roomKey(room), ""); err != nil {
		t.Fatal(err)
	}

	if err := e.rooms.TransferOwner(room.ID, heir.ID, owner.ID); err == nil {
		t.Fatal("non-owner must not transfer ownership")
	}
	if err := e.rooms.TransferOwner(room.ID, owner.ID, heir.ID); err != nil {
		t.Fatalf("TransferOwner: %v", err)
	}
	if role, _ := e.rooms.GetMemberRole(room.ID, heir.ID); role != models.RoleOwner {
		t.Fatalf("heir role = %s, want owner", role)
	}
	if role, _ := e.rooms.GetMemberRole(room.ID, owner.ID); role != models.RoleMember {
		t.Fatalf("old owner role = %s, want member", role)
	}
	saved, _ := e.roomRepo.FindByID(room.ID)
	if saved.OwnerID != heir.ID {
		t.Fatal("rooms.owner_id not updated")
	}
}

func TestLeaveRoom_UpdatesCountWithoutClobberingLockState(t *testing.T) {
	e := newPGEnv(t)
	owner := e.user(t, "owner")
	guest := e.user(t, "guest")
	room := e.room(t, owner, 10, models.AuthTypeNone, "")
	if _, err := e.rooms.JoinRoom(guest.ID, roomKey(room), ""); err != nil {
		t.Fatal(err)
	}
	if err := e.rooms.LockRoom(room.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.rooms.LeaveRoom(room.ID, guest.ID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	saved, _ := e.roomRepo.FindByID(room.ID)
	if saved.CurrentSize != 1 {
		t.Fatalf("CurrentSize after leave = %d, want 1", saved.CurrentSize)
	}
	if !saved.IsLocked {
		t.Fatal("LeaveRoom must not reset is_locked (whole-row Save regression)")
	}
}
