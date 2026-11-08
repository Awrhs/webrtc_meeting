package sfu

// 回归：浏览器对某发布者摄像头的 subscribe 往往先于发布者的摄像头轨道到达 SFU
// （点"开启摄像头"→ API 广播 media_state → 其他人立刻 subscribe，而发布者侧
// addTrack → renegotiation → SFU OnTrack 要晚几百毫秒到几秒；CI 的完整 E2E 里
// Bob 的 subscribe 甚至在 Alice 入会时就发出了）。旧实现在发布者还没有视频轨道时
// 直接丢弃 subscribe，之后轨道到了也没人再订阅，远端永远停在"等待视频流..."。
// 现在 SFU 记住订阅意图，摄像头层到达时主动推给已声明兴趣的订阅者，并在先到的层
// 不是期望层时切到期望层。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

type latePublishEnv struct {
	s        *SFUServer
	peerA    *Peer // 发布者（SFU 侧）
	peerB    *Peer // 订阅者（SFU 侧）
	clientB  *wsSignaling
	gotVideo chan struct{}
	cleanup  func()
}

// setupLatePublish 启动真实 SFU，A 只发音频入会，B 带 recvonly 视频入会并在 A 还没有
// 摄像头轨道时就 subscribe A 的视频。返回时双方媒体连接均已建立。
func setupLatePublish(t *testing.T) *latePublishEnv {
	t.Helper()
	s := NewSFUServer(zap.NewNop(), ServerOptions{
		AllowUnauthenticatedDev: true,
		MessageRateLimit:        1000,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.HandleWebSocket)
	server := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	newBrowserPC := func() *webrtc.PeerConnection {
		pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		return pc
	}
	waitConnectedPC := func(pc *webrtc.PeerConnection, who string) {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s not connected: %s", who, pc.ConnectionState())
	}

	// ===== A：只有音频 =====
	pcA := newBrowserPC()
	audioA, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio-a", "stream-a")
	if _, err := pcA.AddTrack(audioA); err != nil {
		t.Fatal(err)
	}
	clientA := newWSClient(t, wsURL, pcA)
	if err := clientA.initialNegotiate(1, 1); err != nil {
		t.Fatalf("A negotiate: %v", err)
	}
	clientA.handleSFURenegotiation()
	stopAudio := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopAudio:
				return
			case <-ticker.C:
				_ = audioA.WriteSample(sampleOpus20ms())
			}
		}
	}()
	waitConnectedPC(pcA, "A")

	// ===== B：收 A 的视频 =====
	pcB := newBrowserPC()
	audioB, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio-b", "stream-b")
	if _, err := pcB.AddTrack(audioB); err != nil {
		t.Fatal(err)
	}
	if _, err := pcB.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	gotVideo := make(chan struct{}, 64)
	pcB.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if tr.Kind() != webrtc.RTPCodecTypeVideo {
			return
		}
		go func() {
			buf := make([]byte, 1500)
			for {
				if _, _, err := tr.Read(buf); err != nil {
					return
				}
				select {
				case gotVideo <- struct{}{}:
				default:
				}
			}
		}()
	})
	clientB := newWSClient(t, wsURL, pcB)
	if err := clientB.initialNegotiate(1, 2); err != nil {
		t.Fatalf("B negotiate: %v", err)
	}
	clientB.handleSFURenegotiation()
	waitConnectedPC(pcB, "B")

	// B 在 A 还没有摄像头轨道时就订阅 A 的视频
	clientB.send(SignalingMessage{Type: "subscribe", TargetUserID: 1, Kind: "video"})

	room := s.getRoom(1)
	if room == nil {
		t.Fatal("room 1 not found")
	}
	room.mu.Lock()
	peerA, peerB := room.Peers[1], room.Peers[2]
	room.mu.Unlock()
	if peerA == nil || peerB == nil {
		t.Fatal("peers not registered")
	}
	waitFor(t, "B's video interest recorded", 3*time.Second, func() bool { return peerB.wantsVideoFrom(1) })
	time.Sleep(300 * time.Millisecond)
	if peerB.hasSubscriptionFor(1, "video", "stream-a") {
		t.Fatal("A has no video yet; nothing should have been subscribed")
	}

	return &latePublishEnv{
		s:        s,
		peerA:    peerA,
		peerB:    peerB,
		clientB:  clientB,
		gotVideo: gotVideo,
		cleanup: func() {
			close(stopAudio)
			clientA.close()
			clientB.close()
			pcA.Close()
			pcB.Close()
			server.Close()
		},
	}
}

// publishCameraLayer 模拟发布者的一层摄像头轨道到达 SFU（与 OnTrack 中的构造一致），
// 并持续写入 VP8 关键帧 RTP（订阅者侧 OnTrack 需要真实包）。返回停止写入的函数。
func (e *latePublishEnv) publishCameraLayer(t *testing.T, rid string, ssrc uint32) func() {
	t.Helper()
	local, err := webrtc.NewTrackLocalStaticRTP(webrtcCodecVP8().RTPCodecCapability, "video-a"+rid, "stream-a")
	if err != nil {
		t.Fatal(err)
	}
	ti := &TrackInfo{
		Track:    local,
		SSRC:     ssrc,
		Kind:     "video",
		StreamID: "stream-a",
		RID:      rid,
		Codec:    webrtcCodecVP8(),
	}
	resp := e.s.sendPeerEventWait(e.peerA, peerEvent{
		typ:  peerEvtTrackAdded,
		data: trackAddedEvent{trackInfo: ti, fromPeer: e.peerA},
	})
	if resp.err != nil {
		t.Fatalf("track added event: %v", resp.err)
	}

	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(33 * time.Millisecond)
		defer ticker.Stop()
		var seq uint16
		var ts uint32
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				seq++
				ts += 3000
				_ = local.WriteRTP(&rtp.Packet{
					Header:  rtp.Header{Version: 2, Marker: true, PayloadType: 96, SequenceNumber: seq, Timestamp: ts, SSRC: ssrc},
					Payload: vp8TestKeyframe(),
				})
			}
		}
	}()
	return func() { close(stop) }
}

func (e *latePublishEnv) layerSubscribedByB() string {
	return e.peerB.subscribedCameraLayer(1, "stream-a")
}

// 订阅意图先于轨道：摄像头层到达后 B 必须被订阅上、收到 ssrc_mapping 与视频 RTP。
func TestLatePublish_SubscribeBeforeCameraTrackIsHonored(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	e := setupLatePublish(t)
	defer e.cleanup()

	stop := e.publishCameraLayer(t, "high", 0x1001)
	defer stop()

	waitFor(t, "B subscribed to A's camera high layer", 8*time.Second, func() bool {
		return e.layerSubscribedByB() == "high"
	})

	// B 入会时已收到过 A 音频的 ssrc_mapping，这里要一直读到摄像头流的映射为止
	deadline := time.After(5 * time.Second)
	for found := false; !found; {
		select {
		case m := <-e.clientB.ssrcMappings:
			for _, entry := range m {
				if entry.UserID == 1 && entry.Kind == "video" && entry.StreamID == "stream-a" && entry.RID == "high" {
					found = true
				}
			}
		case <-deadline:
			t.Fatal("B did not receive ssrc_mapping for the pushed camera track")
		}
	}

	select {
	case <-e.gotVideo:
	case <-time.After(10 * time.Second):
		t.Fatal("B did not receive A's video RTP after the late camera publish")
	}
}

// 两层先后到达：low 先到被暂用；期望层 high 随后到达时必须切过去，且同一条流只保留一层。
func TestLatePublish_LowLayerFirstThenSwitchesToHigh(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	e := setupLatePublish(t)
	defer e.cleanup()

	stopLow := e.publishCameraLayer(t, "low", 0x2001)
	defer stopLow()
	waitFor(t, "B temporarily on low layer", 8*time.Second, func() bool {
		return e.layerSubscribedByB() == "low"
	})

	stopHigh := e.publishCameraLayer(t, "high", 0x2002)
	defer stopHigh()
	waitFor(t, "B switched to high layer", 8*time.Second, func() bool {
		return e.layerSubscribedByB() == "high"
	})

	// 同一条流不得同时订阅两层
	time.Sleep(300 * time.Millisecond)
	e.peerB.mu.Lock()
	layers := 0
	for key := range e.peerB.addedTracks {
		if strings.HasPrefix(key, "1|video-stream-a/") {
			layers++
		}
	}
	e.peerB.mu.Unlock()
	if layers != 1 {
		t.Fatalf("B subscribes %d layers of A's camera stream, want exactly 1", layers)
	}

	select {
	case <-e.gotVideo:
	case <-time.After(10 * time.Second):
		t.Fatal("B did not receive video RTP after layer switch")
	}
}

// 退订即撤回兴趣：之后发布者再开摄像头不再推给该订阅者。
func TestLatePublish_UnsubscribeWithdrawsInterest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	e := setupLatePublish(t)
	defer e.cleanup()

	e.clientB.send(SignalingMessage{Type: "unsubscribe", TargetUserID: 1, Kind: "video"})
	waitFor(t, "B's video interest withdrawn", 3*time.Second, func() bool { return !e.peerB.wantsVideoFrom(1) })

	stop := e.publishCameraLayer(t, "high", 0x3001)
	defer stop()
	time.Sleep(1500 * time.Millisecond)
	if e.layerSubscribedByB() != "" {
		t.Fatal("B unsubscribed before the camera appeared; it must not be auto-subscribed")
	}
}
