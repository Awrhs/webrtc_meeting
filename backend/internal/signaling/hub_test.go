package signaling

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

// newTestHub 创建一个运行中的 Hub。roomService/chatService 传 nil：
// 本测试只驱动 Register/Unregister/Broadcast 等不触碰业务层的路径。
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	hub := NewHub(nil, nil, nil, zap.NewNop())
	go hub.Run()
	t.Cleanup(func() {
		// Hub.Run 没有退出机制，测试进程结束时 goroutine 随之结束
	})
	return hub
}

func newTestClient(userID uint, sendBuffer int) *Client {
	return &Client{
		hub:     nil,
		conn:    nil,
		send:    make(chan []byte, sendBuffer),
		userID:  userID,
		roomIDs: make(map[uint]bool),
	}
}

// waitFor 轮询直到 cond 为真或超时
func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", desc)
}

func recvNonBlocking(ch chan []byte) ([]byte, bool) {
	select {
	case msg := <-ch:
		return msg, true
	default:
		return nil, false
	}
}

func isClosed(ch chan []byte) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TestHubSlowClientRemovedFromAllRooms 回归测试：
// 慢客户端（send 队列满）在一个房间广播中被清理后，必须从所有房间移除。
// 旧实现只从当前房间移除，客户端残留在其他房间映射中，
// 后续对其他房间广播会向已关闭的 send 通道发送，导致整个 Hub panic。
func TestHubSlowClientRemovedFromAllRooms(t *testing.T) {
	hub := newTestHub(t)

	client := newTestClient(1, 1)
	// 预先填满 send 队列，使下一次投递失败
	client.send <- []byte("filler")

	hub.Register(client)
	hub.AddClientToRoom(client, 100)
	hub.AddClientToRoom(client, 200)

	waitFor(t, "client joined rooms", func() bool {
		return client.isRoomJoined(100) && client.isRoomJoined(200)
	})

	// 广播到房间 100：client 队列满 → 触发完整移除
	hub.BroadcastToRoom(100, []byte("hello"), 0)

	waitFor(t, "slow client send channel closed", func() bool {
		return isClosed(client.send)
	})

	// 关键回归点：向房间 200 广播不得 panic（旧实现会 send on closed channel）
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hub panicked on broadcast to other room: %v", r)
		}
	}()
	hub.BroadcastToRoom(200, []byte("hello2"), 0)
	time.Sleep(50 * time.Millisecond)

	// client 已从两个房间移除
	if client.isRoomJoined(100) || client.isRoomJoined(200) {
		t.Errorf("client should be removed from all rooms after slow-client cleanup")
	}
}

// TestHubDuplicateRegisterEvictsOldClient 回归测试：
// 同一 userID 二次注册时应踢掉旧连接；旧连接随后断开触发 Unregister
// 不得误删新连接的注册状态（旧实现按 userID 删除，会把新连接踢下线）。
func TestHubDuplicateRegisterEvictsOldClient(t *testing.T) {
	hub := newTestHub(t)

	oldClient := newTestClient(7, 8)
	hub.Register(oldClient)
	hub.AddClientToRoom(oldClient, 300)
	waitFor(t, "old client joined room", func() bool {
		return oldClient.isRoomJoined(300)
	})

	newClient := newTestClient(7, 8)
	hub.Register(newClient)
	waitFor(t, "old client evicted", func() bool {
		return isClosed(oldClient.send)
	})

	// 旧连接随后断开 → Unregister：不得影响新连接
	hub.Unregister(oldClient)
	time.Sleep(50 * time.Millisecond)

	// 新连接应仍能收到房间广播
	hub.AddClientToRoom(newClient, 300)
	waitFor(t, "new client joined room", func() bool {
		return newClient.isRoomJoined(300)
	})

	hub.BroadcastToRoom(300, []byte("still-alive"), 0)
	waitFor(t, "new client receives broadcast", func() bool {
		_, ok := recvNonBlocking(newClient.send)
		return ok
	})
}

// TestHubSlowClientOnSendToUser 慢客户端在单播路径被清理后，
// 再次注册同 userID 的新客户端应能正常接收单播。
func TestHubSlowClientOnSendToUser(t *testing.T) {
	hub := newTestHub(t)

	client := newTestClient(9, 1)
	client.send <- []byte("filler")
	hub.Register(client)
	waitFor(t, "client registered", func() bool {
		return true
	})

	hub.SendToUser(9, []byte("full"))
	waitFor(t, "send channel closed after unicast overflow", func() bool {
		return isClosed(client.send)
	})

	fresh := newTestClient(9, 8)
	hub.Register(fresh)
	hub.SendToUser(9, []byte("hello"))
	waitFor(t, "fresh client receives unicast", func() bool {
		msg, ok := recvNonBlocking(fresh.send)
		return ok && string(msg) == "hello"
	})
}
