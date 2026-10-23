package sfu

import (
	"bytes"
	"os"
	"testing"

	"github.com/pion/rtp"
)

// 构造一个 VP8 关键帧：描述符字节 S=1（单包帧，无扩展）+ 关键帧未压缩数据头
func vp8Keyframe() []byte {
	payload := []byte{0x10} // S=1（起始包）
	// VP8 帧 tag(3 bytes) + start code(3 bytes) + width/height 16:14
	payload = append(payload,
		0x30, 0x01, 0x00, // frame tag（keyframe, show）
		0x9d, 0x01, 0x2a, // start code
		0x00, 0x01, // width=1
		0x00, 0x01, // height=1
	)
	return payload
}

func TestVP8Depacketize(t *testing.T) {
	// 起始包（S=1，无扩展位）：描述符长度 1
	payload := vp8Keyframe()
	got, isStart := vp8Depacketize(payload)
	if len(got) != len(payload)-1 {
		t.Fatalf("depacketized length = %d, want %d", len(got), len(payload)-1)
	}
	if !isStart {
		t.Fatal("expected S bit set on start packet")
	}
	if got[0] != 0x30 {
		t.Fatalf("depacketized first byte = %#x, want VP8 frame tag", got[0])
	}

	// 续包（S=0）：仍剥离 1 字节描述符，isStart 为 false
	cont := append([]byte{0x00}, 0xaa, 0xbb)
	got2, isStart2 := vp8Depacketize(cont)
	if isStart2 || len(got2) != 2 || got2[0] != 0xaa {
		t.Fatalf("continuation depacketize = (%v,%v)", got2, isStart2)
	}
}

func TestIVFWriter(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.ivf"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := newIVFWriter(f, 640, 480)
	if err != nil {
		t.Fatal(err)
	}
	frame := []byte{0xde, 0xad, 0xbe, 0xef}
	if err := w.writeFrame(frame, 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("DKIF")) {
		t.Fatal("IVF header missing DKIF magic")
	}
	if !bytes.Contains(data, []byte("VP80")) {
		t.Fatal("IVF header missing VP80 fourcc")
	}
	if !bytes.Contains(data, frame) {
		t.Fatal("frame payload not written")
	}
}

func TestOGGWriter(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.ogg"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := newOGGWriter(f, 2, 48000, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.writePacket([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("OggS")) {
		t.Fatal("OGG page capture pattern missing")
	}
	if !bytes.Contains(data, []byte("OpusHead")) {
		t.Fatal("OpusHead BOS packet missing")
	}
	if !bytes.Contains(data, []byte("OpusTags")) {
		t.Fatal("OpusTags packet missing")
	}
}

func TestTrackRecorder_IVFKeyframeFirst(t *testing.T) {
	dir := t.TempDir()
	rec, err := newTrackRecorder(dir, "camera-video", webrtcCodecVP8(), "video", "stream")
	if err != nil {
		t.Fatal(err)
	}
	payload := vp8Keyframe()
	pkt := &rtp.Packet{
		Header:  rtp.Header{Marker: true, Timestamp: 0, PayloadType: 96, SequenceNumber: 1},
		Payload: payload,
	}
	rec.writeRTP(pkt)
	rec.close()

	data, err := os.ReadFile(rec.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("DKIF")) {
		t.Fatalf("recorder output is not IVF, first bytes: % x", data[:min(8, len(data))])
	}
}

func TestTrackRecorder_IVFReassemblyAndDeltaWait(t *testing.T) {
	dir := t.TempDir()
	rec, err := newTrackRecorder(dir, "cam", webrtcCodecVP8(), "video", "stream")
	if err != nil {
		t.Fatal(err)
	}

	// 先到一个 P 帧（起始标记但非关键帧）：必须被丢弃，不能初始化 IVF
	delta := append([]byte{0x11}, 0x00, 0x00, 0x00) // S=1, P=1
	rec.writeRTP(&rtp.Packet{Header: rtp.Header{Marker: true, Timestamp: 9000, PayloadType: 96}, Payload: delta})
	rec.mu.Lock()
	deltaWrote := rec.ivf != nil
	rec.mu.Unlock()
	if deltaWrote {
		t.Fatal("IVF must not be initialized from a non-keyframe")
	}

	// 分片的关键帧（两包）：start + continuation，验证 S 位重组
	keyPayload, _ := vp8Depacketize(vp8Keyframe())
	part1 := append([]byte{0x10}, keyPayload[:3]...) // S=1, no marker
	part2 := append([]byte{0x00}, keyPayload[3:]...) // continuation, marker
	rec.writeRTP(&rtp.Packet{Header: rtp.Header{Marker: false, Timestamp: 0, PayloadType: 96, SequenceNumber: 10}, Payload: part1})
	rec.writeRTP(&rtp.Packet{Header: rtp.Header{Marker: true, Timestamp: 0, PayloadType: 96, SequenceNumber: 11}, Payload: part2})
	rec.close()

	data, err := os.ReadFile(rec.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("DKIF")) {
		t.Fatal("IVF should be initialized after keyframe")
	}
	// 重组后的帧数据应包含完整 start code
	if !bytes.Contains(data, []byte{0x9d, 0x01, 0x2a}) {
		t.Fatal("reassembled VP8 keyframe payload missing")
	}
}

func TestTrackRecorder_OGG(t *testing.T) {
	dir := t.TempDir()
	rec, err := newTrackRecorder(dir, "mic-audio", webrtcCodecOpus(), "audio", "stream")
	if err != nil {
		t.Fatal(err)
	}
	pkt := &rtp.Packet{
		Header:  rtp.Header{Marker: true, Timestamp: 0, PayloadType: 111, SequenceNumber: 1},
		Payload: []byte{0xf8, 0xff, 0xfe},
	}
	rec.writeRTP(pkt)
	rec.close()

	data, err := os.ReadFile(rec.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("OggS")) {
		t.Fatal("recorder output is not OGG")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
