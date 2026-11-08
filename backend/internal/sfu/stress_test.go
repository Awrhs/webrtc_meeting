package sfu

// 并发压力测试：多个模拟浏览器同时加入/离开同一房间，并让下一轮的加入与上一轮的离开重叠。
// 目的不是验证媒体内容，而是让 -race 有机会抓到 handleJoin / removePeer / deleteRoomIfEmpty /
// onTrack / autoSubscribeTrack 之间的并发问题（顺序执行的测试永远触发不了这些路径）。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

type stressPeer struct {
	client *wsSignaling
	pc     *webrtc.PeerConnection
	stop   chan struct{}
}

func TestConcurrentJoinLeaveStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	s := NewSFUServer(zap.NewNop(), ServerOptions{
		AllowUnauthenticatedDev: true,
		MessageRateLimit:        100000,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	const (
		roomID = uint(777)
		rounds = 3
		peers  = 5
	)

	// joinAll 并发完成 peers 个用户的入会与初始协商，返回成功的 peer。
	// WebSocket 拨号在主 goroutine 里完成（newWSClient 内有 t.Fatalf，不能放进子 goroutine）。
	joinAll := func(round int) []*stressPeer {
		t.Helper()
		result := make([]*stressPeer, peers)
		for i := 0; i < peers; i++ {
			pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			audio, err := webrtc.NewTrackLocalStaticSample(
				webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
				fmt.Sprintf("audio-%d-%d", round, i), fmt.Sprintf("stream-%d-%d", round, i))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pc.AddTrack(audio); err != nil {
				t.Fatal(err)
			}
			stop := make(chan struct{})
			go func() {
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-stop:
						return
					case <-ticker.C:
						_ = audio.WriteSample(sampleOpus20ms())
					}
				}
			}()
			result[i] = &stressPeer{client: newWSClient(t, wsURL, pc), pc: pc, stop: stop}
		}

		var wg sync.WaitGroup
		errs := make(chan error, peers)
		for i, p := range result {
			wg.Add(1)
			go func(i int, p *stressPeer) {
				defer wg.Done()
				if err := p.client.initialNegotiate(roomID, uint(i+1)); err != nil {
					errs <- fmt.Errorf("peer %d negotiate: %w", i, err)
					return
				}
				p.client.handleSFURenegotiation()
			}(i, p)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		return result
	}

	leaveAll := func(group []*stressPeer) {
		var wg sync.WaitGroup
		for _, p := range group {
			wg.Add(1)
			go func(p *stressPeer) {
				defer wg.Done()
				close(p.stop)
				p.client.close()
				_ = p.pc.Close()
			}(p)
		}
		wg.Wait()
	}

	// 第 0 轮：无重叠，确认房间人数正确
	current := joinAll(0)
	waitFor(t, "all peers registered in room", 5*time.Second, func() bool {
		room := s.getRoom(roomID)
		if room == nil {
			return false
		}
		room.mu.Lock()
		defer room.mu.Unlock()
		return len(room.Peers) == peers
	})

	// 后续轮次：上一轮离开与本轮加入重叠（同一批 userID 被复用，覆盖"顶替旧连接"路径）
	for round := 1; round < rounds; round++ {
		previous := current
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			leaveAll(previous)
		}()
		current = joinAll(round)
		wg.Wait()
	}

	// 全部离开后房间必须被删除（deleteRoomIfEmpty 在并发下仍然正确）
	leaveAll(current)
	waitFor(t, "room deleted after everyone left", 8*time.Second, func() bool {
		return s.getRoom(roomID) == nil
	})

	// RTCP 路由表与 QoS 状态不应残留已离开用户的条目
	s.rtcpRouter.mu.RLock()
	leftover := len(s.rtcpRouter.ssrcMap)
	s.rtcpRouter.mu.RUnlock()
	if leftover != 0 {
		t.Errorf("rtcp router still has %d entries after all peers left", leftover)
	}
}

// waitFor 轮询直到条件成立或超时。
func waitFor(t *testing.T, desc string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", desc)
}
