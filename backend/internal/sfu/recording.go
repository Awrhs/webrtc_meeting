package sfu

// ==================== 会议录制（RTP 落盘） ====================
//
// 设置环境变量 SFU_RECORDING_DIR 后，SFU 会把房间内每一路收到的媒体轨道路由到：
//   <SFU_RECORDING_DIR>/<roomID>/<startTime>-<trackKey>.{ivf|ogg|rtp}
// VP8 视频封装为 IVF、Opus 音频封装为 OGG（均可直接用 ffmpeg/ffplay 打开），
// 其他编码（VP9/H264/AV1 等）回退为原始 RTP dump（配合同目录生成的 .sdp
// 可用 ffmpeg 的 rtp demuxer 解封装）。
//
// 注意：媒体 E2EE 开启时录下的是密文，无法回放（录制与 E2EE 天然互斥）。
// 录制文件通过 SFU 的 /recordings 管理接口（控制 token 鉴权）查询与下载。

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

// ==================== IVF（VP8） ====================

const ivfHeaderSize = 32
const ivfFrameHeaderSize = 12

type ivfWriter struct {
	f          *os.File
	frameCount uint64
}

func newIVFWriter(f *os.File, width, height uint16) (*ivfWriter, error) {
	header := make([]byte, ivfHeaderSize)
	copy(header[0:], "DKIF")
	binary.LittleEndian.PutUint16(header[8:], 0)   // version
	binary.LittleEndian.PutUint16(header[10:], 32) // header size
	copy(header[12:], "VP80")
	binary.LittleEndian.PutUint16(header[16:], width)
	binary.LittleEndian.PutUint16(header[18:], height)
	binary.LittleEndian.PutUint32(header[20:], 30) // 时基 denominator（30fps 近似）
	binary.LittleEndian.PutUint32(header[24:], 1)  // numerator
	binary.LittleEndian.PutUint32(header[28:], 0)  // frame count（结尾不回写也可播放）
	if _, err := f.Write(header); err != nil {
		return nil, err
	}
	return &ivfWriter{f: f}, nil
}

func (w *ivfWriter) writeFrame(data []byte, ts uint64) error {
	hdr := make([]byte, ivfFrameHeaderSize)
	binary.LittleEndian.PutUint32(hdr[0:], uint32(len(data)))
	binary.LittleEndian.PutUint64(hdr[4:], ts)
	if _, err := w.f.Write(hdr); err != nil {
		return err
	}
	_, err := w.f.Write(data)
	w.frameCount++
	return err
}

// parseVP8FrameInfo 从 VP8 关键帧的未压缩数据块解析宽高（0 表示解析失败）。
func parseVP8FrameInfo(frame []byte) (uint16, uint16) {
	// frame tag(3) + start code(3) + width(2) + height(2)
	if len(frame) < 10 {
		return 0, 0
	}
	if frame[3] != 0x9d || frame[4] != 0x01 || frame[5] != 0x23 {
		return 0, 0
	}
	w := uint16(frame[6]&0x3f)<<8 | uint16(frame[7])
	h := uint16(frame[8]&0x3f)<<8 | uint16(frame[9])
	return w, h
}

// vp8Depacketize 去掉 RTP 载荷头的 VP8 描述符，返回纯 VP8 数据。
func vp8Depacketize(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	b0 := payload[0]
	descLen := 1
	if b0&0x80 != 0 { // X bit
		if len(payload) < 2 {
			return nil
		}
		b1 := payload[1]
		if b1&0x80 != 0 { // I：PictureID（1 或 2 字节）
			if len(payload) < descLen+1 {
				return nil
			}
			descLen++
			if payload[descLen]&0x80 != 0 {
				descLen++
			}
		}
		if b1&0x40 != 0 { // L：TL0PICIDX
			descLen++
		}
		if b1&0x20 != 0 || b1&0x10 != 0 { // T/K
			descLen++
		}
	}
	if len(payload) <= descLen {
		return nil
	}
	return payload[descLen:]
}

// ==================== OGG（Opus） ====================

var oggCRCTable = func() [256]uint32 {
	var t [256]uint32
	for i := 0; i < 256; i++ {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ 0x04c11db7
			} else {
				r <<= 1
			}
			t[i] = r
		}
	}
	return t
}()

func oggCRC(p []byte) uint32 {
	crc := uint32(0)
	for _, b := range p {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

type oggWriter struct {
	f        *os.File
	serial   uint32
	pageNo   uint32
	granule  uint64
	samples  uint64 // 累计采样（Opus 每包 20ms → 960 采样 @48k）
}

func newOGGWriter(f *os.File, channels uint8, sampleRate uint32, serial uint32) (*oggWriter, error) {
	w := &oggWriter{f: f, serial: serial}
	// OpusHead
	head := make([]byte, 19)
	copy(head[0:], "OpusHead")
	head[8] = 1 // version
	head[9] = channels
	binary.LittleEndian.PutUint16(head[10:], 312) // pre-skip
	binary.LittleEndian.PutUint32(head[12:], sampleRate)
	binary.LittleEndian.PutUint16(head[16:], 0) // output gain
	head[18] = 0                                // mapping family
	if err := w.writePage(head, false); err != nil {
		return nil, err
	}
	// OpusTags（最简）
	tags := []byte("OpusTags")
	tags = append(tags, 0, 0, 0, 0) // vendor len = 0
	tags = append(tags, 0, 0, 0, 0) // comment count = 0
	if err := w.writePage(tags, false); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *oggWriter) writePage(data []byte, lastPacketInPage bool) error {
	page := make([]byte, 0, 27+1+len(data))
	page = append(page, 'O', 'g', 'g', 'S')
	page = append(page, 0) // version
	var hdrType byte
	if w.pageNo == 0 {
		hdrType |= 0x02 // BOS
	}
	if lastPacketInPage {
		hdrType |= 0x04
	}
	page = append(page, hdrType)
	granule := make([]byte, 8)
	binary.LittleEndian.PutUint64(granule, w.granule)
	page = append(page, granule...)
	serial := make([]byte, 4)
	binary.LittleEndian.PutUint32(serial, w.serial)
	page = append(page, serial...)
	pn := make([]byte, 4)
	binary.LittleEndian.PutUint32(pn, w.pageNo)
	page = append(page, pn...)
	page = append(page, 0, 0, 0, 0) // CRC 占位
	page = append(page, 1)           // segment count
	page = append(page, byte(len(data))) // lacing value（<=255）
	page = append(page, data...)

	binary.LittleEndian.PutUint32(page[22:], oggCRC(page))
	w.pageNo++
	_, err := w.f.Write(page)
	return err
}

func (w *oggWriter) writePacket(opus []byte) error {
	// Opus 默认 20ms 一包 @48kHz → 960 采样
	w.samples += 960
	w.granule = w.samples
	return w.writePage(opus, false)
}

// ==================== 原始 RTP dump（其他编码回退） ====================

type rawRTPWriter struct {
	f *os.File
}

func (w *rawRTPWriter) write(pkt *rtp.Packet) error {
	raw, err := pkt.Marshal()
	if err != nil {
		return err
	}
	_, err = w.f.Write(raw)
	return err
}

// ==================== 轨道录制器 ====================

type trackRecorder struct {
	mu       sync.Mutex
	kind     string // "ivf" | "ogg" | "rtp"
	filePath string

	ivf     *ivfWriter
	ogg     *oggWriter
	raw     *rawRTPWriter
	file    *os.File

	// VP8 帧重组
	frameBuf    []byte
	frameTS     uint64
	frameKey    bool
	gotWidth    uint16
	gotHeight   uint16
	ivfNotInit  bool

	bytesWritten uint64
}

// newTrackRecorder 打开录制文件并根据编码选择封装格式。
func newTrackRecorder(dir, baseName string, codec webrtc.RTPCodecCapability, kind string, streamID string) (*trackRecorder, error) {
	rec := &trackRecorder{}

	mime := strings.ToLower(codec.MimeType)
	switch {
	case mime == "audio/opus" && kind == "audio":
		path := filepath.Join(dir, baseName+".ogg")
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		ow, err := newOGGWriter(f, 2, 48000, time.Now().UnixNano()&0xffffffff)
		if err != nil {
			f.Close()
			return nil, err
		}
		rec.kind, rec.filePath, rec.file, rec.ogg = "ogg", path, f, ow
	case mime == "video/vp8":
		path := filepath.Join(dir, baseName+".ivf")
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		rec.kind, rec.filePath, rec.file = "ivf", path, f
		// IVF 头在拿到首个关键帧的宽高后写入
	default:
		path := filepath.Join(dir, baseName+".rtp")
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		rec.kind, rec.filePath, rec.file = "rtp", path, f
		rec.raw = &rawRTPWriter{f: f}
		// 生成配套 SDP，便于 ffmpeg 解封装
		pt := codec.PayloadType
		rtpmap := strings.TrimPrefix(codec.MimeType, "video/")
		rtpmap = strings.TrimPrefix(rtpmap, "audio/")
		sdp := fmt.Sprintf("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=conference-recording\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=%s 0 RTP/AVP %d\r\na=rtpmap:%d %s/%d\r\n",
			kind, pt, pt, rtpmap, codec.ClockRate)
		_ = os.WriteFile(path+".sdp", []byte(sdp), 0644)
	}
	return rec, nil
}

// writeRTP 写入一个 RTP 包。并发安全（每轨道一个转发 goroutine，录制也串行）。
func (r *trackRecorder) writeRTP(pkt *rtp.Packet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return
	}

	switch r.kind {
	case "ivf":
		payload := vp8Depacketize(pkt.Payload)
		if payload == nil {
			return
		}
		if pkt.Marker {
			// 帧最后一个分片
			r.frameBuf = append(r.frameBuf, payload...)
			if !r.ivfNotInit {
				// 首帧：从关键帧解析宽高并写 IVF 头（VP8 首帧必为关键帧）
				w, h := parseVP8FrameInfo(r.frameBuf)
				if w > 0 && h > 0 {
					r.gotWidth, r.gotHeight = w, h
					iw, err := newIVFWriter(r.file, w, h)
					if err != nil {
						return
					}
					r.ivf = iw
					r.ivfNotInit = true
				}
			}
			if r.ivf != nil {
				_ = r.ivf.writeFrame(r.frameBuf, uint64(pkt.Timestamp))
				r.bytesWritten += uint64(len(r.frameBuf))
			}
			r.frameBuf = r.frameBuf[:0]
		} else {
			if len(r.frameBuf) == 0 {
				r.frameTS = uint64(pkt.Timestamp)
			}
			r.frameBuf = append(r.frameBuf, payload...)
		}
	case "ogg":
		if r.ogg != nil {
			_ = r.ogg.writePacket(pkt.Payload)
			r.bytesWritten += uint64(len(pkt.Payload))
		}
	case "rtp":
		if r.raw != nil {
			_ = r.raw.write(pkt)
			r.bytesWritten += uint64(len(pkt.Raw))
		}
	}
}

func (r *trackRecorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

// ==================== 房间录制管理器 ====================

// roomRecorder 管理单个房间的录制目录和轨道文件。
type roomRecorder struct {
	mu      sync.Mutex
	dir     string
	started time.Time
	opened  map[string]*trackRecorder // key: trackKey
	closed  bool
}

// recorderManager 管理所有房间的录制器。
type recorderManager struct {
	mu       sync.Mutex
	baseDir  string
	rooms    map[uint]*roomRecorder
}

func newRecorderManager(baseDir string) *recorderManager {
	return &recorderManager{baseDir: baseDir, rooms: make(map[uint]*roomRecorder)}
}

func (m *recorderManager) enabled() bool {
	return m != nil && m.baseDir != ""
}

// trackRecorderFor 返回（必要时创建）某房间某轨道的录制器。
func (m *recorderManager) trackRecorderFor(roomID uint, trackKey string, codec webrtc.RTPCodecCapability, kind, streamID string) *trackRecorder {
	if !m.enabled() {
		return nil
	}
	m.mu.Lock()
	rr, ok := m.rooms[roomID]
	if !ok {
		dir := filepath.Join(m.baseDir, fmt.Sprintf("room_%d_%s", roomID, time.Now().Format("20060102_150405")))
		if err := os.MkdirAll(dir, 0755); err != nil {
			m.mu.Unlock()
			return nil
		}
		rr = &roomRecorder{dir: dir, started: time.Now(), opened: make(map[string]*trackRecorder)}
		m.rooms[roomID] = rr
	}
	m.mu.Unlock()

	rr.mu.Lock()
	defer rr.mu.Unlock()
	if rr.closed {
		return nil
	}
	if tr, ok := rr.opened[trackKey]; ok {
		return tr
	}
	baseName := sanitizeFileName(time.Now().Format("150405") + "_" + trackKey)
	tr, err := newTrackRecorder(rr.dir, baseName, codec, kind, streamID)
	if err != nil {
		return nil
	}
	rr.opened[trackKey] = tr
	return tr
}

// stopRoom 关闭并落盘某房间的全部录制文件（房间销毁/服务停机时调用）。
func (m *recorderManager) stopRoom(roomID uint) {
	if !m.enabled() {
		return
	}
	m.mu.Lock()
	rr, ok := m.rooms[roomID]
	if ok {
		delete(m.rooms, roomID)
	}
	m.mu.Unlock()
	if !ok {
		return
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.closed = true
	for _, tr := range rr.opened {
		tr.close()
	}
	// 写 manifest
	manifest := fmt.Sprintf("room_id=%d\nstarted=%s\nstopped=%s\n", roomID,
		rr.started.Format(time.RFC3339), time.Now().Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(rr.dir, "manifest.txt"), []byte(manifest), 0644)
}

// stopAll 停止全部录制（服务停机时调用）。
func (m *recorderManager) stopAll() {
	if !m.enabled() {
		return
	}
	m.mu.Lock()
	ids := make([]uint, 0, len(m.rooms))
	for id := range m.rooms {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.stopRoom(id)
	}
}

// listRecordings 返回录制目录清单（管理接口用）。
func (m *recorderManager) listRecordings() []map[string]interface{} {
	if !m.enabled() {
		return nil
	}
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return nil
	}
	var result []map[string]interface{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		roomDir := filepath.Join(m.baseDir, e.Name())
		files, err := os.ReadDir(roomDir)
		if err != nil {
			continue
		}
		fileList := make([]string, 0, len(files))
		for _, f := range files {
			fileList = append(fileList, f.Name())
		}
		result = append(result, map[string]interface{}{
			"dir":   e.Name(),
			"files": fileList,
		})
	}
	return result
}

func sanitizeFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
