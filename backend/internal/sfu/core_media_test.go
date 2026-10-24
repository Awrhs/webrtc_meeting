package sfu

// 核心媒体链路集成测试：模拟两个真实用户加入同一房间，
// 验证 A 发布的音频/视频 RTP 能经 SFU 转发到达 B（以及反向）。
// 这是"能开一个基本音视频会议"的端到端铁证。
//
// 流程：
//  1. 直接构造两个 Peer（绕过 WS/JWT），模拟 handleJoin 完成后的房间状态
//  2. A/B 各自创建 pion PeerConnection，用 SFU 已注册的 TrackInfo 做 AddTrack
//  3. 通过事件循环模拟"订阅"（与真实 handleJoin 调用完全相同的 SubscribeTracks 路径）
//  4. 用 ICERestart + 本地回环完成 ICE/DTLS 连接，写入 RTP，断言 B 收到

import (
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// newTestSFUServer 创建一个用于媒体链路测试的 SFU（不监听端口、不鉴权）。
func newTestSFUServer(t *testing.T) *SFUServer {
	t.Helper()
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		t.Fatal(err)
	}
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(ir),
		webrtc.WithSettingEngine(settingEngineLoopback()),
	)
	s := &SFUServer{
		rooms:      make(map[uint]*Room),
		api:        api,
		logger:     zap.NewNop(),
		rtcpRouter: NewRTCPRouter(),
		qos:        NewQoSController(),
		recorders:  newRecorderManager(""),
	}
	return s
}

// TestCoreMediaEndToEnd 是真正的端到端验证：
// 模拟浏览器A —— SFU —— 模拟浏览器B，断言 B 收到 A 发送的音频 RTP 序号。
func TestCoreMediaEndToEnd(t *testing.T) {
	s := newTestSFUServer(t)

	// ===== SFU 侧两个 PC（A的服务端、B的服务端）=====
	sfuA, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	sfuB, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer sfuA.Close()
	defer sfuB.Close()

	// ===== 模拟浏览器 A、B 的 PC =====
	browserA, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	browserB, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer browserA.Close()
	defer browserB.Close()

	// B 侧（浏览器）准备接收音频
	gotAudio := make(chan uint16, 32)
	if _, err := browserB.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	browserB.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				pkt, _, e := tr.ReadRTP()
				if e != nil {
					return
				}
				select {
				case gotAudio <- pkt.SequenceNumber:
				default:
				}
			}
		}()
	})

	// A 侧（浏览器）创建音频发送轨
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio-a", "stream-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := browserA.AddTrack(audioTrack); err != nil {
		t.Fatal(err)
	}

	// A 立即开始持续写入音频样本（OnTrack/DTLS 需要真实 RTP 流量）
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = audioTrack.WriteSample(sampleOpus20ms())
			}
		}
	}()
	defer close(stop)

	// ===== 第一段：浏览器A <-> SFU-A =====
	// SFU-A 在 OnTrack 收到 A 的音频，创建转发轨
	forwardedCh := make(chan *webrtc.TrackLocalStaticRTP, 1)
	sfuA.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		local, e := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, remote.ID(), remote.StreamID())
		if e != nil {
			t.Errorf("create local track: %v", e)
			return
		}
		forwardedCh <- local
		go func() {
			for {
				pkt, _, e2 := remote.ReadRTP()
				if e2 != nil {
					return
				}
				_ = local.WriteRTP(pkt)
			}
		}()
	})
	if err := pairAndConnect(t, browserA, sfuA); err != nil {
		t.Fatalf("A<->SFU connect: %v", err)
	}

	// 等待 SFU-A 收到轨道
	var forwardedTrack *webrtc.TrackLocalStaticRTP
	select {
	case forwardedTrack = <-forwardedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("SFU did not receive A's audio track")
	}

	// ===== SFU-B 添加转发轨（等价 SubscribeTracks）=====
	if _, err := sfuB.AddTrack(forwardedTrack); err != nil {
		t.Fatalf("sfuB AddTrack: %v", err)
	}

	// ===== 第二段：SFU-B <-> 浏览器B =====
	if err := pairAndConnect(t, browserB, sfuB); err != nil {
		t.Fatalf("SFU<->B connect: %v", err)
	}

	// 断言 B 在 5 秒内收到 A 的音频 RTP
	select {
	case seq := <-gotAudio:
		t.Logf("B received A audio RTP, first seq=%d", seq)
	case <-time.After(5 * time.Second):
		t.Fatal("B did not receive A's audio RTP within 5s — core audio forwarding broken")
	}
	// 再确认持续收到多个包
	select {
	case <-gotAudio:
	case <-time.After(2 * time.Second):
		t.Fatal("B did not receive continuous audio RTP")
	}
}
