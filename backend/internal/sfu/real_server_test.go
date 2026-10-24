package sfu

// 真实服务器端到端测试：
//   - 启动真实 SFUServer + httptest WebSocket 服务（开发逃生阀免鉴权）
//   - 用 pion 模拟两个浏览器客户端，走真实信令流程：
//     join → (SFU answer) → onTrack → (SFU renegotiate offer) → answer → 媒体互通
//   - 断言 B 真实收到 A 的音频/视频 RTP 包
//
// 该测试覆盖 handleJoin/handleOffer/handleAnswer/handleIceCandidate/onTrack/
// autoSubscribeTrack/handleSubscribe/doRenegotiate 的完整真实链路。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"go.uber.org/zap"
)

// wsSignaling 最小化浏览器 WebSocket 信令客户端。
type wsSignaling struct {
	t    *testing.T
	conn *websocket.Conn
	pc   *webrtc.PeerConnection
	mu   sync.Mutex

	joined       chan struct{}
	remoteAnswer chan string
	remoteOffer  chan string

	iceCandidates chan webrtc.ICECandidateInit
	writeMu       sync.Mutex // gorilla websocket 不允许并发写（测试客户端侧）

	closedOnce sync.Once
}

func newWSClient(t *testing.T, url string, pc *webrtc.PeerConnection) *wsSignaling {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	c := &wsSignaling{
		t:             t,
		conn:          ws,
		pc:            pc,
		joined:        make(chan struct{}),
		remoteAnswer:  make(chan string, 8),
		remoteOffer:   make(chan string, 8),
		iceCandidates: make(chan webrtc.ICECandidateInit, 64),
	}
	go c.readLoop()
	return c
}

func (c *wsSignaling) readLoop() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg SignalingMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "joined":
			c.joinedOnce()
		case "answer":
			if msg.SDP != "" {
				c.remoteAnswer <- msg.SDP
			}
		case "offer":
			if msg.SDP != "" {
				c.remoteOffer <- msg.SDP
			}
		case "ice-candidate":
			cand := webrtc.ICECandidateInit{Candidate: msg.Candidate}
			c.iceCandidates <- cand
		case "ssrc_mapping", "muted", "error":
			// 基础测试不关心
		}
	}
}

func (c *wsSignaling) joinedOnce() {
	c.closedOnce.Do(func() { close(c.joined) })
}

func (c *wsSignaling) send(msg SignalingMessage) {
	data, _ := json.Marshal(msg)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *wsSignaling) close() {
	_ = c.conn.Close()
}

// initialNegotiate 浏览器侧初始协商：发 join → 等 joined → createOffer → 等 answer。
func (c *wsSignaling) initialNegotiate(roomID, userID uint) error {
	// 先发送 join，等待 SFU 确认
	c.send(SignalingMessage{Type: "join", RoomID: roomID, UserID: userID})
	select {
	case <-c.joined:
	case <-time.After(5 * time.Second):
		return errTimeout("waiting joined")
	}

	offer, err := c.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := c.pc.SetLocalDescription(offer); err != nil {
		return err
	}
	c.send(SignalingMessage{Type: "offer", RoomID: roomID, UserID: userID, SDP: offer.SDP})

	// 交换 ICE 候选（双向，后台持续）
	go func() {
		c.pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
			if cand != nil {
				c.send(SignalingMessage{
					Type: "ice-candidate", RoomID: roomID, UserID: userID,
					Candidate: cand.ToJSON().Candidate,
				})
			}
		})
	}()
	go func() {
		for cand := range c.iceCandidates {
			_ = c.pc.AddICECandidate(cand)
		}
	}()

	select {
	case answerSDP := <-c.remoteAnswer:
		if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer, SDP: answerSDP,
		}); err != nil {
			return err
		}
	case <-time.After(8 * time.Second):
		return errTimeout("waiting answer")
	}
	return nil
}

// handleSFURenegotiation 持续处理 SFU 发来的重协商 offer（浏览器回复 answer）。
// 必须在初始协商完成后启动。
func (c *wsSignaling) handleSFURenegotiation() {
	go func() {
		for {
			select {
			case sdp, ok := <-c.remoteOffer:
				if !ok {
					return
				}
				if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{
					Type: webrtc.SDPTypeOffer, SDP: sdp,
				}); err != nil {
					return
				}
				answer, err := c.pc.CreateAnswer(nil)
				if err != nil {
					return
				}
				if err := c.pc.SetLocalDescription(answer); err != nil {
					return
				}
				c.send(SignalingMessage{Type: "answer", SDP: answer.SDP})
			case <-time.After(30 * time.Second):
				return
			}
		}
	}()
}

type timeoutErr struct{ what string }

func (e timeoutErr) Error() string { return "timeout: " + e.what }

func errTimeout(what string) error { return timeoutErr{what} }

// TestRealServerAudioVideo 端到端验证真实 SFU 服务器的音频+视频转发。
func TestRealServerAudioVideo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// ===== 启动真实 SFU 服务器 =====
	s := NewSFUServer(zap.NewNop(), ServerOptions{
		AllowUnauthenticatedDev: true, // 测试逃生阀：跳过 token 鉴权
		MessageRateLimit:        1000,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	newBrowserPC := func() *webrtc.PeerConnection {
		pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		return pc
	}

	// ===== 浏览器 A：发送音频+视频 =====
	pcA := newBrowserPC()
	audioA, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio-a", "stream-a")
	videoA, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"video-a", "stream-a")
	if _, err := pcA.AddTrack(audioA); err != nil {
		t.Fatal(err)
	}
	if _, err := pcA.AddTrack(videoA); err != nil {
		t.Fatal(err)
	}
	clientA := newWSClient(t, wsURL, pcA)
	defer clientA.close()
	if err := clientA.initialNegotiate(1, 1); err != nil {
		t.Fatalf("A negotiate: %v", err)
	}
	clientA.handleSFURenegotiation()

	// A 准备接收 B 的音频（renegotiation 后 SFU 会把 B 的轨道推给 A）
	gotAudioAtA := make(chan struct{}, 64)
	pcA.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if tr.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		go func() {
			buf := make([]byte, 1500)
			for {
				if _, _, err := tr.Read(buf); err != nil {
					return
				}
				select {
				case gotAudioAtA <- struct{}{}:
				default:
				}
			}
		}()
	})

	// A 开始发媒体
	stopMedia := make(chan struct{})
	go pumpSamples(audioA, videoA, stopMedia)
	defer close(stopMedia)

	// 等待 A 与 SFU 媒体连接建立（SFU OnTrack 已触发）
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if pcA.ConnectionState() == webrtc.PeerConnectionStateConnected {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pcA.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatalf("A not connected: %s", pcA.ConnectionState())
	}
	// SFU 侧需要收到轨道（等待 internal tracks 注册）
	time.Sleep(500 * time.Millisecond)

	// ===== 浏览器 B：接收 + 发布自己的音频（验证反向链路/renegotiation）=====
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

	gotAudio := make(chan struct{}, 64)
	gotVideo := make(chan struct{}, 64)
	var mediaWG sync.WaitGroup
	pcB.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		kind := tr.Kind()
		mediaWG.Add(1)
		go func() {
			defer mediaWG.Done()
			buf := make([]byte, 1500)
			for {
				_, _, err := tr.Read(buf)
				if err != nil {
					return
				}
				if kind == webrtc.RTPCodecTypeAudio {
					select {
					case gotAudio <- struct{}{}:
					default:
					}
				} else {
					select {
					case gotVideo <- struct{}{}:
					default:
					}
				}
			}
		}()
	})

	clientB := newWSClient(t, wsURL, pcB)
	defer clientB.close()
	if err := clientB.initialNegotiate(1, 2); err != nil {
		t.Fatalf("B negotiate: %v", err)
	}
	clientB.handleSFURenegotiation()

	// B 请求订阅 A 的摄像头视频（带 RID 的 simulcast 需要显式订阅）
	// A 用的是单层视频（无 simulcast），会按非 RID 视频自动订阅；
	// 但为保险起见仍显式 subscribe 一次（幂等）。
	clientB.send(SignalingMessage{Type: "subscribe", TargetUserID: 1, Kind: "video", Quality: "high"})

	// 等待 SFU-B 与浏览器B连接
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if pcB.ConnectionState() == webrtc.PeerConnectionStateConnected {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pcB.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatalf("B not connected: %s", pcB.ConnectionState())
	}

	// B 发布音频（B 加入时 SFU 的 onTrack 会触发 autoSubscribeTrack
	// 把 B 的音频推给已协商的 A —— 触发 A 的 renegotiation）
	stopMediaB := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMediaB:
				return
			case <-ticker.C:
				_ = audioB.WriteSample(media.Sample{Data: []byte{0xf8, 0xff, 0xfe}, Duration: 20 * time.Millisecond})
			}
		}
	}()
	defer close(stopMediaB)

	// ===== 断言：B 收到 A 的音频 =====
	select {
	case <-gotAudio:
		t.Log("PASS: B received A's audio RTP")
	case <-time.After(8 * time.Second):
		t.Fatal("B did not receive A's audio RTP — basic audio broken")
	}

	// ===== 断言：B 收到 A 的视频 =====
	select {
	case <-gotVideo:
		t.Log("PASS: B received A's video RTP")
	case <-time.After(8 * time.Second):
		t.Fatal("B did not receive A's video RTP — basic video broken")
	}

	// ===== 断言：A 收到 B 的音频（renegotiation 把新用户轨道推给已在线用户）=====
	select {
	case <-gotAudioAtA:
		t.Log("PASS: A received B's audio RTP (renegotiation path works)")
	case <-time.After(10 * time.Second):
		t.Fatal("A did not receive B's audio RTP — renegotiation for existing users broken")
	}

	// ===== 回归：订阅侧路由条目必须填发布者原始 SSRC（PLI 转发改写依赖）=====
	s.rtcpRouter.mu.RLock()
	okTrackSSRC := false
	for _, target := range s.rtcpRouter.ssrcMap {
		if target.Subscriber != nil && target.Subscriber.UserID == 2 && target.FromUserID == 1 {
			if target.TrackSSRC == 0 {
				t.Errorf("B subscription route has zero TrackSSRC — PLI forwarding would fail")
			} else {
				okTrackSSRC = true
			}
		}
	}
	s.rtcpRouter.mu.RUnlock()
	if !okTrackSSRC {
		t.Fatal("no B-subscriber route entry for A found")
	}
}

// pumpSamples 持续向音频/视频轨道写入样本（模拟真实摄像头/麦克风）。
func pumpSamples(audio *webrtc.TrackLocalStaticSample, video *webrtc.TrackLocalStaticSample, stop <-chan struct{}) {
	audioTicker := time.NewTicker(20 * time.Millisecond)
	videoTicker := time.NewTicker(33 * time.Millisecond)
	defer audioTicker.Stop()
	defer videoTicker.Stop()
	var audioTS, videoTS uint32
	opus := []byte{0xf8, 0xff, 0xfe}
	for {
		select {
		case <-stop:
			return
		case <-audioTicker.C:
			audioTS += 960
			_ = audio.WriteSample(media.Sample{Data: opus, Duration: 20 * time.Millisecond})
		case <-videoTicker.C:
			videoTS += 3000
			// 最小合法 VP8 关键帧（宽高 1x1），SFU/浏览器不需要真实解码内容，
			// 测试只关心 RTP 包是否到达。
			_ = video.WriteSample(media.Sample{Data: vp8TestKeyframe(), Duration: 33 * time.Millisecond})
		}
	}
}

func vp8TestKeyframe() []byte {
	// S=1 描述符 + VP8 keyframe（tag 0x30 + start code 9d 01 2a + 1x1 尺寸）
	return []byte{
		0x10,
		0x30, 0x01, 0x00,
		0x9d, 0x01, 0x2a,
		0x01, 0x00,
		0x01, 0x00,
	}
}
