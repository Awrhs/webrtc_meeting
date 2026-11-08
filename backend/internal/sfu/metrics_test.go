package sfu

import (
	"strings"
	"testing"

	"conference-sfu/backend/pkg/metrics"

	"go.uber.org/zap"
)

func TestRegisterMetricsExposesCountsAndCounters(t *testing.T) {
	s := &SFUServer{rooms: make(map[uint]*Room), logger: zap.NewNop(), rtcpRouter: NewRTCPRouter(), recorders: newRecorderManager("")}
	s.rooms[1] = &Room{ID: 1, Peers: map[uint]*Peer{7: {UserID: 7, Tracks: map[string]*TrackInfo{"audio-x": {}}}}}
	s.stats.peersJoined.Add(3)
	s.stats.rtpBytesIn.Add(1024)
	s.rtcpRouter.pliForwarded.Add(2)

	reg := metrics.NewRegistry()
	s.RegisterMetrics(reg)
	var b strings.Builder
	reg.Write(&b)
	out := b.String()
	for _, want := range []string{
		"sfu_rooms 1\n",
		"sfu_peers 1\n",
		"sfu_tracks 1\n",
		"# TYPE sfu_peers_joined_total counter\nsfu_peers_joined_total 3\n",
		"sfu_rtp_bytes_in_total 1024\n",
		"sfu_pli_forwarded_total 2\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics missing %q:\n%s", want, out)
		}
	}
}

func TestIsSafeRecordingName(t *testing.T) {
	ok := []string{"room_1_20261108_173000", "173000_video_abc_high.ivf", "manifest.txt", "a-b.ogg"}
	bad := []string{"", "..", "../x", "a/b", "a\\b", "name with space", "中文"}
	for _, n := range ok {
		if !isSafeRecordingName(n) {
			t.Errorf("%q should be accepted", n)
		}
	}
	for _, n := range bad {
		if isSafeRecordingName(n) {
			t.Errorf("%q should be rejected", n)
		}
	}
}
