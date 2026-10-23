package sfu

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// sampleOpus20ms 生成 20ms 静音 Opus 帧（TOC=0xf8 静音帧，浏览器可正常解码）。
func sampleOpus20ms() media.Sample {
	return media.Sample{Data: []byte{0xf8, 0xff, 0xfe}, Duration: 20 * time.Millisecond}
}

// contextWithCancel 测试辅助
func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// settingEngineLoopback 创建仅使用 host/loopback 候选的 SettingEngine，
// 避免测试环境访问外网 STUN，并强制 ICE 在本地快速连通。
func settingEngineLoopback() webrtc.SettingEngine {
	var se webrtc.SettingEngine
	se.SetICETimeouts(5*time.Second, 5*time.Second, time.Second)
	return se
}

// pairAndConnect 让两个 PeerConnection 完成 offer/answer + ICE 连接。
// offerer 是浏览器角色，answerer 是 SFU 角色。
func pairAndConnect(t *testing.T, offerer, answerer *webrtc.PeerConnection) error {
	t.Helper()

	// 互相转发 ICE 候选
	exchangeCandidates(offerer, answerer)

	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		return err
	}
	if err := answerer.SetRemoteDescription(offer); err != nil {
		return err
	}
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		return err
	}
	if err := offerer.SetRemoteDescription(answer); err != nil {
		return err
	}

	// 等待 ICE 连接
	return waitConnected(t, offerer, answerer, 8*time.Second)
}

func exchangeCandidates(pc1, pc2 *webrtc.PeerConnection) {
	pc1.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			_ = pc2.AddICECandidate(c.ToJSON())
		}
	})
	pc2.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			_ = pc1.AddICECandidate(c.ToJSON())
		}
	})
}

func waitConnected(t *testing.T, a, b *webrtc.PeerConnection, timeout time.Duration) error {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return context.DeadlineExceeded
		case <-ticker.C:
			if a.ConnectionState() == webrtc.PeerConnectionStateConnected &&
				b.ConnectionState() == webrtc.PeerConnectionStateConnected {
				return nil
			}
		}
	}
}
