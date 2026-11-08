package sfu

import (
	"testing"

	"go.uber.org/zap"
)

func newLifecycleTestServer() *SFUServer {
	return &SFUServer{
		rooms:     make(map[uint]*Room),
		logger:    zap.NewNop(),
		recorders: newRecorderManager(""),
	}
}

// deleteRoomIfEmpty 必须把被删除的房间标记为 closed：handleJoin 拿到房间指针后
// 靠这个标记识别"房间已被并发删除"的孤儿房间情形并重新获取。
func TestDeleteRoomIfEmpty_MarksClosedAndRemoves(t *testing.T) {
	s := newLifecycleTestServer()
	room := &Room{ID: 7, Peers: make(map[uint]*Peer)}
	s.rooms[7] = room

	s.deleteRoomIfEmpty(7)

	if _, ok := s.rooms[7]; ok {
		t.Fatal("empty room should be removed from s.rooms")
	}
	room.mu.Lock()
	closed := room.closed
	room.mu.Unlock()
	if !closed {
		t.Fatal("deleted room must be marked closed")
	}
}

// 房间里还有成员时不得删除，也不得标记 closed。
func TestDeleteRoomIfEmpty_KeepsNonEmptyRoom(t *testing.T) {
	s := newLifecycleTestServer()
	room := &Room{ID: 8, Peers: map[uint]*Peer{1: {UserID: 1}}}
	s.rooms[8] = room

	s.deleteRoomIfEmpty(8)

	if _, ok := s.rooms[8]; !ok {
		t.Fatal("non-empty room must not be removed")
	}
	room.mu.Lock()
	closed := room.closed
	room.mu.Unlock()
	if closed {
		t.Fatal("non-empty room must not be marked closed")
	}
}

// 未知房间 ID 应当是 no-op，不能 panic。
func TestDeleteRoomIfEmpty_UnknownRoomIsNoop(t *testing.T) {
	s := newLifecycleTestServer()
	s.deleteRoomIfEmpty(42)
	if len(s.rooms) != 0 {
		t.Fatal("rooms map must stay empty")
	}
}
