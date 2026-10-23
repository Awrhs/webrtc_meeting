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
	// 无扩展位的起始包：描述符长度 1
	payload := vp8Keyframe()
	got := vp8Depacketize(payload)
	if len(got) != len(payload)-1 {
		t.Fatalf("depacketized length = %d, want %d", len(got), len(payload)-1)
	}
	if got[0] != 0x30 {
		t.Fatalf("depacketized first byte = %#x, want VP8 frame tag", got[0])
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
