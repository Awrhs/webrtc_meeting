package signaling

import (
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/service"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// stubGraceService 仅实现宽限期逻辑需要的 LeaveRoom；
// 嵌入接口以满足 RoomService 签名，其余方法在本测试中不会被调用。
type stubGraceService struct {
	service.RoomService
	service.ChatService
	mu          sync.Mutex
	leaveCalled bool
	roomID      uint
	userID      uint
}

func (s *stubGraceService) LeaveRoom(roomID uint, userID uint) error {
	s.mu.Lock()
	s.leaveCalled = true
	s.roomID = roomID
	s.userID = userID
	s.mu.Unlock()
	return nil
}

func (s *stubGraceService) left() (bool, uint, uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leaveCalled, s.roomID, s.userID
}

// GetMemberRole 返回 owner 表示“仍是房间成员”，宽限期逻辑会继续调用 LeaveRoom
func (s *stubGraceService) GetMemberRole(roomID uint, userID uint) (models.Role, error) {
	return models.RoleOwner, nil
}

// TestHubGracePeriodLeaveWired：断线后宽限期内未重连，
// 应通过 checkLeave 通道调用 roomService.LeaveRoom（防止“只补接收端、没有发送端”回归）。
func TestHubGracePeriodLeaveWired(t *testing.T) {
	svc := &stubGraceService{}
	hub := NewHubWithGrace(svc, nil, nil, 200*time.Millisecond, zap.NewNop())
	go hub.Run()
	// 注：Hub.Run 在生产中随进程存活；本测试结束后 Run goroutine 随测试进程退出

	client := &Client{
		userID:  42,
		send:    make(chan []byte, 8),
		conn:    nil,
		hub:     hub,
		roomIDs: map[uint]bool{7: true},
	}
	hub.Register(client)
	hub.AddClientToRoom(client, 7)

	// 模拟断线注销（removeClientNoLock 应调度宽限期检查）
	hub.Unregister(client)
	// 等待 Unregister 被 Run 循环消费（removeClientNoLock 才会调度宽限期）
	time.Sleep(100 * time.Millisecond)

	// 等待宽限期 + 处理时间
	deadline := time.Now().Add(2 * time.Second)
	var called bool
	var gotRoom, gotUser uint
	for time.Now().Before(deadline) {
		called, gotRoom, gotUser = svc.left()
		if called {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !called {
		t.Fatal("grace-period leave not wired: roomService.LeaveRoom was never called after disconnect")
	}
	if gotRoom != 7 || gotUser != 42 {
		t.Fatalf("LeaveRoom called with (%d,%d), want (7,42)", gotRoom, gotUser)
	}
}
