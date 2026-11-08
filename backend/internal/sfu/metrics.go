package sfu

import "conference-sfu/backend/pkg/metrics"

// RegisterMetrics 把 SFU 的运行指标注册到 Prometheus 注册表（/metrics）。
//
//	sfu_rooms / sfu_peers / sfu_tracks            当前活跃房间、成员、上行轨道数
//	sfu_peers_joined_total / sfu_peers_removed_total  累计加入/移除的成员数
//	sfu_renegotiations_total                      SFU 发起的重协商次数
//	sfu_connection_failures_total                 PeerConnection 进入 failed 的次数
//	sfu_rtp_bytes_in_total / sfu_rtp_bytes_out_total  收到/转发出去的 RTP 字节数
//	sfu_pli_forwarded_total                       改写 SSRC 后转发给发布者的关键帧请求数
func (s *SFUServer) RegisterMetrics(reg *metrics.Registry) {
	reg.NewGaugeFunc("sfu_rooms", "Active rooms on this SFU instance.", func() float64 {
		rooms, _, _ := s.snapshotCounts()
		return float64(rooms)
	})
	reg.NewGaugeFunc("sfu_peers", "Connected peers on this SFU instance.", func() float64 {
		_, peers, _ := s.snapshotCounts()
		return float64(peers)
	})
	reg.NewGaugeFunc("sfu_tracks", "Published (upstream) tracks on this SFU instance.", func() float64 {
		_, _, tracks := s.snapshotCounts()
		return float64(tracks)
	})
	reg.NewCounterFunc("sfu_peers_joined_total", "Peers that joined since start.", func() float64 {
		return float64(s.stats.peersJoined.Load())
	})
	reg.NewCounterFunc("sfu_peers_removed_total", "Peers removed (left, kicked, replaced or failed) since start.", func() float64 {
		return float64(s.stats.peersRemoved.Load())
	})
	reg.NewCounterFunc("sfu_renegotiations_total", "Renegotiation offers sent by the SFU.", func() float64 {
		return float64(s.stats.renegotiations.Load())
	})
	reg.NewCounterFunc("sfu_connection_failures_total", "Peer connections that entered the failed state.", func() float64 {
		return float64(s.stats.connectionFailures.Load())
	})
	reg.NewCounterFunc("sfu_rtp_bytes_in_total", "RTP payload bytes received from publishers.", func() float64 {
		return float64(s.stats.rtpBytesIn.Load())
	})
	reg.NewCounterFunc("sfu_rtp_bytes_out_total", "RTP payload bytes forwarded to subscribers.", func() float64 {
		return float64(s.stats.rtpBytesOut.Load())
	})
	reg.NewCounterFunc("sfu_pli_forwarded_total", "Keyframe requests (PLI) forwarded to publishers.", func() float64 {
		return float64(s.rtcpRouter.pliForwarded.Load())
	})
}
