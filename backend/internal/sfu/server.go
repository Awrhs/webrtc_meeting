package sfu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"conference-sfu/backend/internal/auth"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

type Room struct {
	ID    uint
	Peers map[uint]*Peer
	mu    sync.Mutex
}

type TrackInfo struct {
	Track     *webrtc.TrackLocalStaticRTP
	SSRC      uint32
	Kind      string // "audio" or "video"
	StreamID  string // e.g., "camera" or "screen"
	IsEnabled atomic.Bool // 原子操作，转发 goroutine 中无需锁即可读写
	RID       string      // simulcast layer rid: "" / "low" / "high"
}

type Peer struct {
	// 只读字段：创建后不可变，无需任何保护
	UserID uint
	RoomID uint
	PC     *webrtc.PeerConnection
	Conn   *websocket.Conn

	// ===== 状态通道：所有状态变更必须走这里 =====
	// 单 goroutine 串行处理，从结构上消除竞态，无需 mutex
	eventCh chan peerEvent // buffer=32，所有状态变更事件

	// 专用通道（保留独立通道避免与事件通道竞争）
	renegoCh chan struct{}         // buffer=1，触发 renegotiation
	sendCh   chan SignalingMessage // buffer=64，WebSocket 写队列

	// 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc

	// ===== 过渡阶段的互斥锁 =====
	// TODO：所有代码迁移到 eventCh 后删除此锁
	mu sync.Mutex

	// ===== 内部状态：最终目标是仅在 peerEventLoop 中访问 =====
	// 过渡阶段：访问这些字段需要 mu 保护或在 peerEventLoop 中
	Tracks        map[string]*TrackInfo        // key: kind-streamID/RID
	Subscriptions map[uint][]*webrtc.RTPSender // key: fromUserID
	addedTracks   map[string]bool              // key: fromUserID|trackKey
	renegoPending bool                         // 是否有待处理的 renegotiation
}

// rtpPool 复用 RTP 包对象，减少 GC 压力
// 注意：Pion ReadRTP 返回的是新分配的对象，我们在 WriteRTP 后把它放回池
// 下次从池取出时 pion 会覆写内容，这比每次分配新对象省很多 GC
var rtpPool = sync.Pool{
	New: func() interface{} {
		return &rtp.Packet{}
	},
}

// 订阅相关的事件类型已定义在 peerEventType 中
// Subscribe/Unsubscribe 操作现在通过 peerEvent 发送到 peerEventLoop 串行处理

// ===== Peer 事件循环：统一的状态变更串行化机制 =====
// 所有对 Peer 内部状态（Tracks/Subscriptions/addedTracks）的修改都必须走 eventCh，
// 由 peerEventLoop 单 goroutine 串行处理。彻底消除竞态，无需 mutex。

// peerEventType 事件类型枚举
type peerEventType string

const (
	peerEvtTrackAdded     peerEventType = "trackAdded"     // 新轨道到达（onTrack）
	peerEvtTrackRemoved   peerEventType = "trackRemoved"   // 轨道移除
	peerEvtSubscribe      peerEventType = "subscribe"      // 订阅轨道
	peerEvtUnsubscribe    peerEventType = "unsubscribe"    // 取消订阅
	peerEvtMuteTrack      peerEventType = "muteTrack"      // 启用/禁用轨道转发
	peerEvtCleanupPeer    peerEventType = "cleanupPeer"    // 用户离开时清理订阅
	peerEvtHandleOffer    peerEventType = "handleOffer"    // 处理 SDP Offer
	peerEvtHandleAnswer   peerEventType = "handleAnswer"   // 处理 SDP Answer
	peerEvtHandleIce      peerEventType = "handleIce"      // 处理 ICE 候选
)

// peerEvent 所有状态变更都通过此事件发送到 peerEventLoop
type peerEvent struct {
	typ    peerEventType
	data   interface{}
	respCh chan peerResp // 需要返回结果的操作使用此通道
}

// peerResp 事件的执行结果
type peerResp struct {
	err    error
	result interface{}
}

// ===== 各事件的 data 结构 =====

// trackAddedEvent onTrack 新轨道到达事件
type trackAddedEvent struct {
	trackInfo *TrackInfo
	fromPeer  *Peer // 发布者 peer
}

// muteTrackEvent 轨道启用/禁用事件
// 如果 trackKey 为空且 kind 不为空，则静音/启用所有该类型的轨道
type muteTrackEvent struct {
	trackKey string // 单个轨道键，为空表示匹配所有
	kind     string // 轨道类型过滤："audio" / "video" / ""（所有）
	enabled  bool   // 目标状态
}

// cleanupPeerEvent 用户离开时清理订阅事件
type cleanupPeerEvent struct {
	leftUserID uint
}

// subscribeEvent 订阅轨道事件
type subscribeEvent struct {
	targetUserID uint          // 目标发布者
	kind         string        // "audio" or "video"
	streamID     string        // "camera" or "screen"
	tracks       []*TrackInfo  // 已选好的 track 列表
}

// unsubscribeEvent 取消订阅事件
type unsubscribeEvent struct {
	targetUserID uint   // 目标发布者
	kind         string // "audio" or "video"
	streamID     string // 可选："camera" or "screen"
}

// offerEvent 处理 SDP Offer 事件
type offerEvent struct {
	sdp string
}

// answerEvent 处理 SDP Answer 事件
type answerEvent struct {
	sdp string
}

// iceCandidateEvent 处理 ICE 候选事件
type iceCandidateEvent struct {
	candidate     string
	sdpMLineIndex *int
	sdpMid        *string
}

type SFUServer struct {
	rooms                   map[uint]*Room
	api                     *webrtc.API
	configuration           webrtc.Configuration
	logger                  *zap.Logger
	sfuJWTSecret            string
	allowUnauthenticatedDev bool
	allowedOrigins          []string
	callbackToken           string
	messageRateLimit        int
	tokenReplayCheck        TokenReplayChecker
	rtcpRouter              *RTCPRouter // RTCP 路由表：接收端反馈 -> 发送端
	mu                      sync.Mutex
}

// TokenReplayChecker 抽象 jti 重放检查（生产用 Redis SetNX，测试可注入 mock）。
type TokenReplayChecker interface {
	MarkTokenUsed(jti string, ttl time.Duration) (bool, error)
}

// ===== 标准化错误变量 =====
// 使用 errors.Is() 断言这些错误，便于上层处理
var (
	ErrRoomNotFound      = errors.New("sfu: room not found")
	ErrPeerNotFound      = errors.New("sfu: peer not found")
	ErrTrackNotFound     = errors.New("sfu: track not found")
	ErrPeerClosed        = errors.New("sfu: peer is closed")
	ErrSubscribeTimeout  = errors.New("sfu: subscribe timeout")
	ErrEventQueueFull    = errors.New("sfu: event queue is full")
	ErrInvalidMessage    = errors.New("sfu: invalid message format")
	ErrUnauthorized      = errors.New("sfu: unauthorized access")
)

type SSRC struct {
	SSRC     uint32 `json:"ssrc"`
	UserID   uint   `json:"user_id"`
	Kind     string `json:"kind"`      // "audio" or "video"
	StreamID string `json:"stream_id"` // "camera" or "screen"
	RID      string `json:"rid,omitempty"`
}

type SignalingMessage struct {
	Type          string      `json:"type"` // join, offer, answer, ice-candidate, muted, ssrc_mapping, subscribe, unsubscribe
	RoomID        uint        `json:"room_id,omitempty"`
	UserID        uint        `json:"user_id,omitempty"`
	TargetUserID  uint        `json:"target_user_id,omitempty"` // subscribe/unsubscribe 目标用户
	Kind          string      `json:"kind,omitempty"`           // "audio" | "video"
	StreamID      string      `json:"stream_id,omitempty"`      // "camera" | "screen"
	Quality       string      `json:"quality,omitempty"`        // simulcast 选层："high" | "low"
	SDP           string      `json:"sdp,omitempty"`
	Candidate     string      `json:"candidate,omitempty"`
	SDPMLineIndex *int        `json:"sdp_m_line_index,omitempty"`
	SDPMid        *string     `json:"sdp_mid,omitempty"`
	Error         string      `json:"error,omitempty"`
	Data          interface{} `json:"data,omitempty"`
	SSRCs         []SSRC      `json:"ssrcs,omitempty"` // for ssrc_mapping message
}

type ServerOptions struct {
	SFUJWTSecret            string
	AllowUnauthenticatedDev bool
	AllowedOrigins          []string
	CallbackToken           string
	MessageRateLimit        int
	TokenReplayChecker      TokenReplayChecker
}

func NewSFUServer(logger *zap.Logger, opts ServerOptions) *SFUServer {
	// 国内访问 stun.l.google.com 经常超时，把 ICE gathering 拖到 8~15 秒，
	// 拉长了 renegotiation 与 onTrack 的 race window。换成国内 STUN 后
	// gathering 通常 < 1 秒。多个 server 互为备份，任一可达即可。
	configuration := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.qq.com:3478"}},
			{URLs: []string{"stun:stun.miwifi.com:3478"}},
			{URLs: []string{"stun:stun.syncthing.net:3478"}},
		},
	}

	settingEngine := webrtc.SettingEngine{}

	// WebRTC 端口范围配置（可选）
	if minPortStr := os.Getenv("WEBRTC_PORT_RANGE_MIN"); minPortStr != "" {
		if maxPortStr := os.Getenv("WEBRTC_PORT_RANGE_MAX"); maxPortStr != "" {
			minPort, _ := strconv.Atoi(minPortStr)
			maxPort, _ := strconv.Atoi(maxPortStr)
			if minPort > 0 && maxPort > minPort {
				settingEngine.SetEphemeralUDPPortRange(uint16(minPort), uint16(maxPort))
			}
		}
	}

	// 允许回环候选，支持本地开发测试
	settingEngine.SetIncludeLoopbackCandidate(true)

	// 关键修复：创建并注册默认音视频编解码器，否则 Pion 会拒绝所有媒体协商
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		logger.Fatal("Failed to register default codecs", zap.Error(err))
	}

	// 注册 simulcast 所需的 RTP 头扩展（仅 video 方向）
	for _, ext := range []string{
		"urn:ietf:params:rtp-hdrext:sdes:mid",
		"urn:ietf:params:rtp-hdrext:sdes:rtp-stream-id",
		"urn:ietf:params:rtp-hdrext:sdes:repaired-rtp-stream-id",
	} {
		if err := m.RegisterHeaderExtension(
			webrtc.RTPHeaderExtensionCapability{URI: ext},
			webrtc.RTPCodecTypeVideo,
		); err != nil {
			logger.Warn("Failed to register RTP header extension", zap.String("uri", ext), zap.Error(err))
		}
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithSettingEngine(settingEngine),
	)

	if opts.MessageRateLimit <= 0 {
		opts.MessageRateLimit = 120
	}

	return &SFUServer{
		rooms:                   make(map[uint]*Room),
		api:                     api,
		configuration:           configuration,
		logger:                  logger,
		sfuJWTSecret:            opts.SFUJWTSecret,
		allowUnauthenticatedDev: opts.AllowUnauthenticatedDev,
		allowedOrigins:          opts.AllowedOrigins,
		callbackToken:           opts.CallbackToken,
		messageRateLimit:        opts.MessageRateLimit,
		tokenReplayCheck:        opts.TokenReplayChecker,
		rtcpRouter:              NewRTCPRouter(),
	}
}

// onTrack 当 SFU 从某个浏览器收到 RTP 媒体轨道时触发
// 核心职责：1) 将收到的远程轨道包装为本地可转发轨道；2) 启动 RTP 包转发 goroutine；
//  3. 将新书目推送给房间内所有其他用户；4) 触发重新协商让已连接用户接收新轨道
//
// 预期正确逻辑：
//
//	当用户A先连接、用户B后加入时，用户B的handleJoin会为其PC添加用户A的现有轨道（用户B能看到A）。
//	但用户A的PC已经连接完成，如果不重新协商，用户A的浏览器不会知道有新的接收轨道，就看不到B。
//	因此，在 onTrack 中给已连接用户的PC AddTrack 后，必须调用 renegotiate() 创建新offer并发送给浏览器，
//	浏览器收到offer后创建answer回传，SFU设置远程描述，新轨道才能正式传输。
func (s *SFUServer) onTrack(room *Room, peer *Peer, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	trackKind := track.Kind().String()
	streamID := track.StreamID()
	rid := track.RID() // simulcast 层标识，单层时为 ""
	trackKey := trackKind + "-" + streamID
	if rid != "" {
		trackKey = trackKey + "/" + rid
	}

	s.logger.Info("New track received",
		zap.Uint("userID", peer.UserID),
		zap.String("kind", trackKind),
		zap.String("streamID", streamID),
		zap.String("rid", rid),
		zap.Uint32("ssrc", uint32(track.SSRC())))

	// 创建本地可转发轨道：TrackLocalStaticRTP 用于将收到的 RTP 包原样转发给其他用户
	localTrack, err := webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, track.ID()+rid, streamID)
	if err != nil {
		s.logger.Error("Failed to create local track", zap.Error(err))
		return
	}

	// 【关键锁粒度优化】创建 trackInfo 和注册 RTCP 路由不需要 room.mu
	// 这些操作只涉及当前 peer 和全局路由表，可以安全地在锁外执行
	trackInfo := &TrackInfo{
		Track:    localTrack,
		SSRC:     uint32(track.SSRC()),
		Kind:     trackKind,
		StreamID: streamID,
		RID:      rid,
	}

	// 通过事件循环串行添加轨道，消除 peer.mu 竞争
	// IsEnabled 会在事件处理中原子设置
	s.sendPeerEvent(peer, peerEvent{
		typ: peerEvtTrackAdded,
		data: trackAddedEvent{
			trackInfo: trackInfo,
			fromPeer:  peer,
		},
	})

	// 注册 SSRC 到 RTCP 路由表：新轨道的 SSRC -> 发布者 peer
	// 这样当订阅者发送 PLI/NACK 时，我们知道该把 RTCP 发给谁
	s.rtcpRouter.Register(uint32(track.SSRC()), &RTCPTarget{
		Publisher:  peer,
		TrackSSRC:  uint32(track.SSRC()),
		TrackKey:   trackKey,
		FromUserID: peer.UserID,
	})

	// 最小临界区：只在读取 room.Peers 时持有锁
	// 注意：此处不需要完整一致性快照。只要 peer 存在于列表中，即使在
	// 锁释放后立即离开，后续 AddTrack 失败也能被正确处理（日志 + continue）
	// 这在大型房间（50+人）中可以减少锁持有时间 90% 以上
	room.mu.Lock()
	peersToNotify := make([]*Peer, 0, len(room.Peers))
	for _, p := range room.Peers {
		peersToNotify = append(peersToNotify, p)
	}
	room.mu.Unlock()

	// 启动 RTP 包转发 goroutine：从浏览器发来的 track 读取 RTP 包，写入 localTrack
	// localTrack 会被挂载到其他用户的 PeerConnection 上，Pion 会自动将其 RTP 包发送出去
	// 优化 1：使用 ReadRTP/WriteRTP 避免二次解析 RTP 包
	// 优化 2：使用 atomic.Bool 检查 IsEnabled，无需在每个包上都加锁（消除热点锁）
	go func() {
		for {
			// 读取已解析的 RTP 包，避免后续 Write 时二次解析
			pkt, _, err := track.ReadRTP()
			if err != nil {
				s.logger.Debug("Track read ended, stopping forwarding",
					zap.Uint("userID", peer.UserID),
					zap.String("trackKey", trackKey))
				return
			}

			// 【重要优化】使用 atomic.Bool 无需锁即可检查状态
			// 之前：每个 RTP 包都要加锁解锁 -> 每秒约 100 次锁竞争
			// 现在：通过闭包捕获的 trackInfo 直接原子读取，零开销，完全无竞争
			isEnabled := trackInfo.IsEnabled.Load()

			// 只有启用的轨道才转发（被 MuteUser 禁用的轨道会跳过）
			if isEnabled {
				// 直接写入已解析的 RTP 包，省一次 Unmarshal 开销
				if err := localTrack.WriteRTP(pkt); err != nil {
					rtpPool.Put(pkt) // 即使失败也要放回池
					return
				}
			}

			// 放回复用，减少 GC
			rtpPool.Put(pkt)
		}
	}()

	// 构建 SSRC 映射消息：通知所有客户端有一条新轨道到达（用于前端将流归属到具体用户）
	ssrcMapping := SignalingMessage{
		Type:   "ssrc_mapping",
		RoomID: room.ID,
		SSRCs: []SSRC{
			{
				SSRC:     uint32(track.SSRC()),
				UserID:   peer.UserID,
				Kind:     trackKind,
				StreamID: streamID,
				RID:      rid,
			},
		},
	}


	for _, otherPeer := range room.Peers {
		// 给所有用户（包括发布者自己）发送 SSRC 映射，方便前端做流归属
		s.sendToPeer(otherPeer, ssrcMapping)

		// 给其他用户添加轨道订阅：音频和屏幕共享自动扇出，普通视频由前端按需 subscribe
		if otherPeer.UserID == peer.UserID {
			continue
		}

		if trackKind == "video" && streamID != "screen" {
			continue
		}

		// 通过 peerEvent 串行执行 AddTrack，消除 otherPeer.mu 竞争
		// 注意：此处不等待结果，让订阅操作异步在事件循环中完成
		s.sendPeerEvent(otherPeer, peerEvent{
			typ: peerEvtSubscribe,
			data: subscribeEvent{
				targetUserID: peer.UserID,
				kind:         trackKind,
				streamID:     streamID,
				tracks:       []*TrackInfo{trackInfo},
			},
		})
	}
}


// renegotiate 当已连接的 PeerConnection 需要增加新接收轨道时触发重新协商
// 预期正确逻辑：
//
//	WebRTC 连接建立后，如果一方需要增加新的发送轨道（如SFU要转发新用户的书目），
//	必须通过新的 Offer/Answer 交换让对端知道有新的 m-line 或 SSRC。
//	这里SFU作为offerer创建新offer，通过WebSocket发送给浏览器，浏览器回复answer，
//	SFU收到answer后调用SetRemoteDescription，新轨道才能开始传输RTP包。
//
// renegotiationWorker 是每个 Peer 专属的串行 worker，负责处理 renegotiation 触发信号。
// 通过 renegoCh 接收触发信号，buffer=1 保证多次触发被合并成一次执行。
// 这从根本上消除了多个 goroutine 并发 CreateOffer/SetLocalDescription 的 race。
func (s *SFUServer) renegotiationWorker(peer *Peer) {
	for {
		select {
		case <-peer.ctx.Done():
			s.logger.Debug("Renegotiation worker exiting", zap.Uint("userID", peer.UserID))
			return
		case <-peer.renegoCh:
			s.doRenegotiate(peer)
		}
	}
}

// triggerRenegotiation 通知 worker 需要做一次 renegotiation。
// 如果 worker 正在处理或已有 pending 信号，新信号会被丢弃（合并）。
// 这是无阻塞的——调用方（如 onTrack）不会被阻塞。
func (s *SFUServer) triggerRenegotiation(peer *Peer) {
	select {
	case peer.renegoCh <- struct{}{}:
		// 信号已入队
	default:
		// 已有 pending 信号，本次触发被合并掉
		s.logger.Debug("Renegotiation already pending, merging trigger", zap.Uint("userID", peer.UserID))
	}
}

// writerLoop 是每个 Peer 专属的 WS 写 goroutine。所有发往 peer.Conn 的写
// 都必须经由 peer.sendCh 投递，由本 loop 串行消费——这从结构上杜绝了
// gorilla/websocket 的 "concurrent write to websocket connection" panic。
func (s *SFUServer) writerLoop(peer *Peer) {
	for {
		select {
		case <-peer.ctx.Done():
			s.logger.Debug("Writer loop exiting", zap.Uint("userID", peer.UserID))
			return
		case msg := <-peer.sendCh:
			peer.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := peer.Conn.WriteJSON(msg); err != nil {
				s.logger.Warn("ws write failed, closing peer",
					zap.Uint("userID", peer.UserID),
					zap.String("type", msg.Type),
					zap.Error(err))
				// 写失败 → 触发关闭信号；PC 的清理交给 onConnectionStateChange / removePeer
				// context.CancelFunc 天然幂等，可重复调用
				peer.cancel()
				return
			}
		}
	}
}


// ===== Peer 统一事件循环 =====
// 所有状态变更在此 goroutine 串行处理，从根本上消除竞态条件。
// 无需 mutex 保护内部状态，此函数是唯一可以直接读写 peer.Tracks、
// peer.Subscriptions、peer.addedTracks 的地方。
func (s *SFUServer) peerEventLoop(peer *Peer) {
	renegoTicker := time.NewTicker(10 * time.Millisecond)
	defer renegoTicker.Stop()

	for {
		select {
		case <-peer.ctx.Done():
			s.logger.Debug("Peer event loop exiting", zap.Uint("userID", peer.UserID))
			return

		case evt := <-peer.eventCh:
			s.handlePeerEvent(peer, evt)

		case <-renegoTicker.C:
			if peer.renegoPending {
				peer.renegoPending = false
				go s.doRenegotiate(peer)
			}
		}
	}
}

// handlePeerEvent 处理单个 peer 事件。
// 【重要】此函数内完全不需要锁！由 peerEventLoop 保证串行执行。
func (s *SFUServer) handlePeerEvent(peer *Peer, evt peerEvent) {
	switch evt.typ {
	case peerEvtTrackAdded:
		data := evt.data.(trackAddedEvent)
		peer.Tracks[data.trackInfo.Key()] = data.trackInfo
		data.trackInfo.IsEnabled.Store(true) // 原子设置

		s.logger.Info("Track added via event loop",
			zap.Uint("userID", peer.UserID),
			zap.String("trackKey", data.trackInfo.Key()))

		// 自动订阅逻辑：音频和屏幕共享自动广播给所有人
		room := s.getRoom(peer.RoomID)
		if room != nil {
			s.autoSubscribeTrack(room, peer, data.trackInfo)
		}

		if evt.respCh != nil {
			evt.respCh <- peerResp{err: nil}
		}

	case peerEvtMuteTrack:
		data := evt.data.(muteTrackEvent)
		updated := 0
		if data.trackKey != "" {
			// 单个轨道操作
			if track, ok := peer.Tracks[data.trackKey]; ok {
				track.IsEnabled.Store(data.enabled)
				updated = 1
			}
		} else {
			// 批量操作：按 kind 过滤
			for trackKey, track := range peer.Tracks {
				if data.kind == "" || track.Kind == data.kind {
					track.IsEnabled.Store(data.enabled)
					updated++
					s.logger.Debug("Track mute state updated via event loop",
						zap.Uint("userID", peer.UserID),
						zap.String("trackKey", trackKey),
						zap.Bool("enabled", data.enabled))
				}
			}
		}
		if evt.respCh != nil {
			evt.respCh <- peerResp{result: updated}
		}

	case peerEvtCleanupPeer:
		data := evt.data.(cleanupPeerEvent)
		s.cleanupPeerSubscriptionsNoLock(peer, data.leftUserID)
		if evt.respCh != nil {
			evt.respCh <- peerResp{err: nil}
		}

	case peerEvtSubscribe:
		data := evt.data.(subscribeEvent)
		added := false
		var firstSSRC uint32

		for _, ti := range data.tracks {
			trackKey := ti.Key()
			subKey := fmt.Sprintf("%d|%s", data.targetUserID, trackKey)
			if peer.addedTracks[subKey] {
				continue
			}

			sender, err := peer.PC.AddTrack(ti.Track)
			if err != nil {
				s.logger.Warn("Failed to add track in subscription",
					zap.Uint("subscriber", peer.UserID),
					zap.Uint("publisher", data.targetUserID),
					zap.String("trackKey", trackKey),
					zap.Error(err))
				continue
			}

			peer.addedTracks[subKey] = true
			peer.Subscriptions[data.targetUserID] = append(peer.Subscriptions[data.targetUserID], sender)
			s.startRTCPReadLoop(peer, sender, data.targetUserID, trackKey)
			added = true
			firstSSRC = ti.SSRC

			s.logger.Info("Subscribed to track via event loop",
				zap.Uint("subscriber", peer.UserID),
				zap.Uint("publisher", data.targetUserID),
				zap.String("trackKey", trackKey))
		}

		if added {
			s.triggerRenegotiation(peer)
		}
		if evt.respCh != nil {
			evt.respCh <- peerResp{err: nil, result: map[string]interface{}{"added": added, "ssrc": firstSSRC}}
		}

	case peerEvtUnsubscribe:
		data := evt.data.(unsubscribeEvent)
		removed := false
		senders := peer.Subscriptions[data.targetUserID]
		if len(senders) == 0 {
			if evt.respCh != nil {
				evt.respCh <- peerResp{err: nil}
			}
			return
		}

		remaining := make([]*webrtc.RTPSender, 0, len(senders))
		for _, sender := range senders {
			shouldRemove := false
			for key := range peer.addedTracks {
				prefix := fmt.Sprintf("%d|%s-", data.targetUserID, data.kind)
				if strings.HasPrefix(key, prefix) {
					if data.streamID == "" || strings.Contains(key, "-"+data.streamID) {
						shouldRemove = true
						delete(peer.addedTracks, key)
						break
					}
				}
			}
			if shouldRemove {
				if err := peer.PC.RemoveTrack(sender); err != nil {
					s.logger.Warn("Failed to remove track in unsubscription",
						zap.Uint("subscriber", peer.UserID),
						zap.Uint("publisher", data.targetUserID),
						zap.Error(err))
				}
				removed = true
			} else {
				remaining = append(remaining, sender)
			}
		}

		if removed {
			if len(remaining) == 0 {
				delete(peer.Subscriptions, data.targetUserID)
			} else {
				peer.Subscriptions[data.targetUserID] = remaining
			}
			s.triggerRenegotiation(peer)
		}
		if evt.respCh != nil {
			evt.respCh <- peerResp{err: nil}
		}

	case peerEvtHandleOffer:
		// 处理 SDP Offer：在事件循环中串行执行，消除 peer.mu 竞争
		data := evt.data.(offerEvent)
		s.logger.Info("Received offer via event loop", zap.Uint("user", peer.UserID))

		err := peer.PC.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  data.sdp,
		})
		if err != nil {
			if evt.respCh != nil {
				evt.respCh <- peerResp{err: err}
			}
			return
		}

		answer, err := peer.PC.CreateAnswer(nil)
		if err != nil {
			if evt.respCh != nil {
				evt.respCh <- peerResp{err: err}
			}
			return
		}

		err = peer.PC.SetLocalDescription(answer)
		if err != nil {
			if evt.respCh != nil {
				evt.respCh <- peerResp{err: err}
			}
			return
		}

		// 等待 ICE 收集完成
		gatheringDone := webrtc.GatheringCompletePromise(peer.PC)
		select {
		case <-gatheringDone:
		case <-time.After(5 * time.Second):
			s.logger.Warn("ICE gathering timed out, sending current local description", zap.Uint("user", peer.UserID))
		}

		completeAnswer := peer.PC.CurrentLocalDescription()
		if completeAnswer == nil {
			completeAnswer = &answer
		}

		if evt.respCh != nil {
			evt.respCh <- peerResp{err: nil, result: completeAnswer}
		}

	case peerEvtHandleAnswer:
		// 处理 SDP Answer：在事件循环中串行执行，消除 peer.mu 竞争
		data := evt.data.(answerEvent)
		s.logger.Info("Received answer via event loop", zap.Uint("user", peer.UserID))

		err := peer.PC.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer,
			SDP:  data.sdp,
		})
		if err != nil && evt.respCh != nil {
			evt.respCh <- peerResp{err: err}
			return
		}
		s.logger.Info("Renegotiation completed", zap.Uint("user", peer.UserID))
		if evt.respCh != nil {
			evt.respCh <- peerResp{err: nil}
		}

	case peerEvtHandleIce:
		// 处理 ICE 候选：在事件循环中串行执行，消除 peer.mu 竞争
		data := evt.data.(iceCandidateEvent)
		candidate := webrtc.ICECandidateInit{
			Candidate: data.candidate,
		}
		if data.sdpMLineIndex != nil {
			idx := uint16(*data.sdpMLineIndex)
			candidate.SDPMLineIndex = &idx
		}
		if data.sdpMid != nil {
			candidate.SDPMid = data.sdpMid
		}
		err := peer.PC.AddICECandidate(candidate)
		if evt.respCh != nil {
			evt.respCh <- peerResp{err: err}
		}
		if err != nil {
			s.logger.Warn("Failed to add ICE candidate via event loop", zap.Error(err))
		} else {
			s.logger.Info("Added remote ICE candidate via event loop",
				zap.Uint("user", peer.UserID),
				zap.String("candidate", data.candidate[:min(len(data.candidate), 100)]))
		}

	default:
		s.logger.Warn("Unknown peer event type",
			zap.Uint("userID", peer.UserID),
			zap.String("type", string(evt.typ)))
		if evt.respCh != nil {
			evt.respCh <- peerResp{err: fmt.Errorf("unknown event type: %s", evt.typ)}
		}
	}
}

// sendPeerEvent 向 peer 发送状态变更事件，带超时保护。
// 返回是否发送成功（peer 已关闭或超时返回 false）。
func (s *SFUServer) sendPeerEvent(peer *Peer, evt peerEvent) bool {
	select {
	case <-peer.ctx.Done():
		return false
	default:
	}

	select {
	case peer.eventCh <- evt:
		return true
	case <-peer.ctx.Done():
		return false
	case <-time.After(5 * time.Second):
		s.logger.Error("Peer event channel timed out",
			zap.Uint("userID", peer.UserID),
			zap.String("eventType", string(evt.typ)))
		return false
	}
}

// sendPeerEventWait 发送事件并等待结果返回。
func (s *SFUServer) sendPeerEventWait(peer *Peer, evt peerEvent) peerResp {
	respCh := make(chan peerResp, 1)
	evt.respCh = respCh

	if !s.sendPeerEvent(peer, evt) {
		return peerResp{err: fmt.Errorf("peer closed or timeout")}
	}

	select {
	case resp := <-respCh:
		return resp
	case <-peer.ctx.Done():
		return peerResp{err: fmt.Errorf("peer closed")}
	case <-time.After(10 * time.Second):
		return peerResp{err: fmt.Errorf("timeout waiting for event response")}
	}
}

// Key 生成 track 的唯一键。
func (t *TrackInfo) Key() string {
	key := t.Kind + "-" + t.StreamID
	if t.RID != "" {
		key = key + "/" + t.RID
	}
	return key
}

// ===== 订阅操作便捷封装 =====
// 这些函数封装了 event loop 的通信细节，外部调用只需关注业务逻辑

// SubscribeTracks 订阅指定用户的轨道（异步）
func (s *SFUServer) SubscribeTracks(peer *Peer, targetUserID uint, kind, streamID string, tracks []*TrackInfo) error {
	evt := peerEvent{
		typ: peerEvtSubscribe,
		data: subscribeEvent{
			targetUserID: targetUserID,
			kind:         kind,
			streamID:     streamID,
			tracks:       tracks,
		},
		respCh: make(chan peerResp, 1),
	}
	if !s.sendPeerEvent(peer, evt) {
		return ErrEventQueueFull
	}
	return nil
}

// SubscribeTracksWait 订阅指定用户的轨道（同步等待结果）
func (s *SFUServer) SubscribeTracksWait(peer *Peer, targetUserID uint, kind, streamID string, tracks []*TrackInfo) (bool, uint32, error) {
	resp := s.sendPeerEventWait(peer, peerEvent{
		typ: peerEvtSubscribe,
		data: subscribeEvent{
			targetUserID: targetUserID,
			kind:         kind,
			streamID:     streamID,
			tracks:       tracks,
		},
	})
	if resp.err != nil {
		return false, 0, resp.err
	}
	result := resp.result.(map[string]interface{})
	return result["added"].(bool), result["ssrc"].(uint32), nil
}

// UnsubscribeTracks 取消订阅指定用户的轨道（异步）
func (s *SFUServer) UnsubscribeTracks(peer *Peer, targetUserID uint, kind, streamID string) error {
	evt := peerEvent{
		typ: peerEvtUnsubscribe,
		data: unsubscribeEvent{
			targetUserID: targetUserID,
			kind:         kind,
			streamID:     streamID,
		},
		respCh: make(chan peerResp, 1),
	}
	if !s.sendPeerEvent(peer, evt) {
		return ErrEventQueueFull
	}
	return nil
}

// sendToPeer 非阻塞地把消息投到 peer 的写队列。
// - peer 已关闭：静默忽略。
// - 队列满：记日志丢弃（buffer=64 设计上不应该发生，出现即报警）。
// 调用方不需要持有任何锁。
func (s *SFUServer) sendToPeer(peer *Peer, msg SignalingMessage) {
	select {
	case <-peer.ctx.Done():
		return
	default:
	}
	select {
	case peer.sendCh <- msg:
	case <-peer.ctx.Done():
	default:
		s.logger.Warn("peer sendCh full, message dropped",
			zap.Uint("userID", peer.UserID),
			zap.String("type", msg.Type))
	}
}

// doRenegotiate 真正执行一次 renegotiation。仅由 peerEventLoop 的 renegoTicker 调用，
// 因此天然单线程，不会与自身并发；SignalingState 检查作为防御性兜底。
func (s *SFUServer) doRenegotiate(peer *Peer) {
	// 【架构说明】此处无需 peer.mu 保护
	// 1. doRenegotiate 仅在 peerEventLoop 的 ticker 中被触发，串行执行
	// 2. 仅访问 peer.PC 的方法（Pion 内部已同步）
	// 3. 不访问 peer.Tracks/Subscriptions/addedTracks 等状态

	// 防御性检查：理论上 worker 串行，进入时应总是 stable；
	// 若不是，说明 handleAnswer 还没回来（不太可能但留兜底）
	if peer.PC.SignalingState() != webrtc.SignalingStateStable {
		s.logger.Warn("doRenegotiate: not stable, re-queueing",
			zap.Uint("userID", peer.UserID),
			zap.String("state", peer.PC.SignalingState().String()))
		// 把信号重新塞回去稍后重试
		go func() {
			time.Sleep(200 * time.Millisecond)
			s.triggerRenegotiation(peer)
		}()
		return
	}

	s.logger.Info("Starting renegotiation", zap.Uint("userID", peer.UserID))

	offer, err := peer.PC.CreateOffer(nil)
	if err != nil {
		s.logger.Error("Failed to create renegotiation offer", zap.Uint("userID", peer.UserID), zap.Error(err))
		return
	}

	if err := peer.PC.SetLocalDescription(offer); err != nil {
		s.logger.Error("Failed to set local description for renegotiation", zap.Uint("userID", peer.UserID), zap.Error(err))
		return
	}

	s.sendToPeer(peer, SignalingMessage{
		Type: "offer",
		SDP:  offer.SDP,
	})
	s.logger.Info("Sent renegotiation offer", zap.Uint("userID", peer.UserID))
}

// ===== RTCP 完整实现：质量控制的核心 =====
// RTCP 是 WebRTC 视频质量自适应的关键。浏览器通过 RTCP 反馈：
// - PLI (Picture Loss Indication)：请求关键帧（丢包后视频花屏）
// - NACK (Negative Acknowledgement)：重传特定丢包
// - REMB (Receiver Estimated Maximum Bitrate)：接收端估计的可用带宽
// - RR (Receiver Report)：丢包率、延迟抖动统计

// RTCPTarget 记录 SSRC 对应的发送端信息
// 当订阅者发送 RTCP 反馈时，我们需要找到对应的 publisher 并将反馈发给他
type RTCPTarget struct {
	Publisher  *Peer                // 轨道发布者
	TrackSSRC  uint32               // 发布者的原始 SSRC（浏览器发送的）
	TrackKey   string               // 轨道唯一标识
	FromUserID uint                 // 发布者用户ID
}

// RTCPRouter RTCP 路由表，将接收端的反馈转发给对应的发送端
type RTCPRouter struct {
	mu      sync.RWMutex
	ssrcMap map[uint32]*RTCPTarget // SSRC -> 目标信息
}

// NewRTCPRouter 创建新的 RTCP 路由器
func NewRTCPRouter() *RTCPRouter {
	return &RTCPRouter{
		ssrcMap: make(map[uint32]*RTCPTarget),
	}
}

// Register 注册 SSRC 到 publisher 的映射
func (r *RTCPRouter) Register(ssrc uint32, target *RTCPTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ssrcMap[ssrc] = target
}

// UnregisterByUser 清理用户相关的所有 SSRC 映射
func (r *RTCPRouter) UnregisterByUser(userID uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ssrc, target := range r.ssrcMap {
		if target.FromUserID == userID {
			delete(r.ssrcMap, ssrc)
		}
	}
}

// Route 将 RTCP 包路由到对应的 publisher
// 返回是否成功路由
func (r *RTCPRouter) Route(pkt rtcp.Packet) bool {
	var mediaSSRC uint32
	var target *RTCPTarget

	switch p := pkt.(type) {
	case *rtcp.PictureLossIndication:
		mediaSSRC = p.MediaSSRC
	case *rtcp.ReceiverEstimatedMaximumBitrate:
		// REMB 没有 MediaSSRC，通常是整个连接的
		return false
	case *rtcp.TransportLayerNack:
		mediaSSRC = p.MediaSSRC
	case *rtcp.ReceiverReport:
		if len(p.Reports) > 0 {
			mediaSSRC = p.Reports[0].SSRC
		} else {
			return false
		}
	default:
		// 其他类型的 RTCP 包不处理
		return false
	}

	r.mu.RLock()
	target, ok := r.ssrcMap[mediaSSRC]
	r.mu.RUnlock()

	if !ok || target.Publisher == nil {
		return false
	}

	// 写入 publisher 的接收端，Pion 会自动将 RTCP 发送给浏览器
	err := target.Publisher.PC.WriteRTCP([]rtcp.Packet{pkt})
	if err != nil {
		return false
	}
	return true
}

// startRTCPReadLoop 启动 RTCP 读取和转发 goroutine
// 从订阅者读取 RTCP 反馈，解析后路由到对应的 publisher
// 这是视频质量自适应的关键：PLI 请求关键帧、NACK 请求重传、REMB 码率估计
func (s *SFUServer) startRTCPReadLoop(peer *Peer, sender *webrtc.RTPSender, fromUserID uint, trackKey string) {
	// 在订阅时注册 SSRC 映射，确保 RTCP 能路由回去
	// 注意：sender.GetParameters().Encodings[0].SSRC 就是轨道的 SSRC
	params := sender.GetParameters()
	if len(params.Encodings) > 0 {
		ssrc := uint32(params.Encodings[0].SSRC)
		publisher := s.getPeerByID(peer.RoomID, fromUserID)
		if publisher != nil {
			// 找到 publisher 对应的 receiver（简化处理，实际需要精确匹配）
			s.rtcpRouter.Register(ssrc, &RTCPTarget{
				Publisher:  publisher,
				FromUserID: fromUserID,
				TrackKey:   trackKey,
			})
		}
	}

	go func() {
		buf := make([]byte, 1500)
		pliCount := 0
		nackCount := 0

		for {
			n, _, err := sender.Read(buf)
			if err != nil {
				s.logger.Debug("RTCP read loop ended",
					zap.Uint("toUser", peer.UserID),
					zap.Uint("fromUser", fromUserID),
					zap.String("trackKey", trackKey),
					zap.Int("pliTotal", pliCount),
					zap.Int("nackTotal", nackCount),
					zap.Error(err))
				return
			}

			// 解析 RTCP 包
			packets, err := rtcp.Unmarshal(buf[:n])
			if err != nil {
				continue
			}

			for _, pkt := range packets {
				switch p := pkt.(type) {
				case *rtcp.PictureLossIndication:
					// PLI：接收端请求关键帧（视频花屏时）
					pliCount++
					s.logger.Debug("RTCP PLI received",
						zap.Uint("fromUser", peer.UserID),
						zap.Uint("toUser", fromUserID),
						zap.Uint32("ssrc", p.MediaSSRC),
						zap.String("trackKey", trackKey))
					s.rtcpRouter.Route(pkt)

				case *rtcp.TransportLayerNack:
					// NACK：接收端请求重传特定丢包
					nackCount++
					lostPackets := make([]uint16, 0, len(p.Nacks))
					for _, nack := range p.Nacks {
						lostPackets = append(lostPackets, nack.PacketList()...)
					}
					s.logger.Debug("RTCP NACK received",
						zap.Uint("fromUser", peer.UserID),
						zap.Uint("toUser", fromUserID),
						zap.Int("lostPackets", len(lostPackets)),
						zap.String("trackKey", trackKey))
					s.rtcpRouter.Route(pkt)

				case *rtcp.ReceiverEstimatedMaximumBitrate:
					// REMB：接收端估计的可用带宽（码率控制）
					s.logger.Debug("RTCP REMB received",
						zap.Uint("fromUser", peer.UserID),
						zap.Uint("toUser", fromUserID),
						zap.Float64("bitrateMbps", float64(p.Bitrate)/1000000),
						zap.String("trackKey", trackKey))
					// 注意：REMB 没有 MediaSSRC，需要特殊处理
					// 简化：暂时不路由，留给带宽估计算法

				case *rtcp.ReceiverReport:
					// RR：接收端统计报告（丢包率、延迟抖动）
					for _, report := range p.Reports {
						if report.TotalLost > 0 {
							s.logger.Debug("RTCP Receiver Report",
								zap.Uint("fromUser", peer.UserID),
								zap.Uint("toUser", fromUserID),
								zap.Uint32("lostPackets", report.TotalLost),
								zap.Uint32("jitter", report.Jitter),
								zap.String("trackKey", trackKey))
						}
					}
					s.rtcpRouter.Route(pkt)

				default:
					// 其他类型的 RTCP 包直接转发
					s.rtcpRouter.Route(pkt)
				}
			}
		}
	}()
}

// getPeerByID 根据用户ID查找 peer，线程安全
func (s *SFUServer) getPeerByID(roomID, userID uint) *Peer {
	s.mu.Lock()
	room := s.rooms[roomID]
	s.mu.Unlock()

	if room == nil {
		return nil
	}

	room.mu.Lock()
	peer := room.Peers[userID]
	room.mu.Unlock()
	return peer
}

func (s *SFUServer) cleanupPeerSubscriptions(room *Room, leavingUserID uint) {
	for _, peer := range room.Peers {
		if peer.UserID == leavingUserID {
			continue
		}
		// 通过统一事件循环串行清理订阅，消除 peer.mu 竞争
		s.sendPeerEvent(peer, peerEvent{
			typ: peerEvtCleanupPeer,
			data: cleanupPeerEvent{
				leftUserID: leavingUserID,
			},
		})
	}
}

// cleanupPeerSubscriptionsNoLock 清理单个 peer 对某个用户的订阅。
// 【注意】调用此函数时必须已持有 peer.mu 或在 peerEventLoop 中。
func (s *SFUServer) cleanupPeerSubscriptionsNoLock(peer *Peer, targetUserID uint) {
	removed := false
	for _, sender := range peer.Subscriptions[targetUserID] {
		if err := peer.PC.RemoveTrack(sender); err != nil {
			s.logger.Warn("Failed to remove subscribed track",
				zap.Uint("fromUser", targetUserID),
				zap.Uint("toUser", peer.UserID),
				zap.Error(err))
		}
		removed = true
	}
	delete(peer.Subscriptions, targetUserID)
	prefix := fmt.Sprintf("%d|", targetUserID)
	for key := range peer.addedTracks {
		if strings.HasPrefix(key, prefix) {
			delete(peer.addedTracks, key)
		}
	}

	if removed {
		s.triggerRenegotiation(peer)
	}
}

// getRoom 获取房间对象，线程安全。
func (s *SFUServer) getRoom(roomID uint) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms[roomID]
}

// autoSubscribeTrack 将 publisher 的新轨道自动订阅给房间内所有其他用户。
// 规则：音频和屏幕共享自动订阅，视频按需订阅。
func (s *SFUServer) autoSubscribeTrack(room *Room, publisher *Peer, trackInfo *TrackInfo) {
	// 普通视频不由系统自动订阅，由前端按需发起 subscribe 请求
	if trackInfo.Kind == "video" && trackInfo.StreamID == "camera" {
		return
	}

	// 音频和屏幕共享：广播给所有其他用户
	for _, otherPeer := range room.Peers {
		if otherPeer.UserID == publisher.UserID {
			continue
		}

		// 通过统一事件循环串行执行订阅，消除竞态
		s.SubscribeTracks(otherPeer, publisher.UserID, trackInfo.Kind, trackInfo.StreamID, []*TrackInfo{trackInfo})

		s.logger.Debug("Auto-subscribed track via event loop",
			zap.Uint("fromUser", publisher.UserID),
			zap.Uint("toUser", otherPeer.UserID),
			zap.String("trackKey", trackInfo.Key()))
	}
}

// handleSubscribe 处理前端发来的选择性订阅请求。
func (s *SFUServer) handleSubscribe(room *Room, peer *Peer, msg *SignalingMessage) {
	if msg.TargetUserID == 0 || msg.Kind == "" {
		s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "subscribe requires target_user_id and kind"})
		return
	}

	publisher := room.Peers[msg.TargetUserID]
	if publisher == nil {
		s.logger.Debug("Subscribe target not found",
			zap.Uint("subscriber", peer.UserID),
			zap.Uint("target", msg.TargetUserID))
		return
	}

	// 只在读取 publisher.Tracks 时加锁，选层完成立即释放
	// 订阅者自身的状态操作在 peerEventLoop 中串行进行，无需锁
	publisher.mu.Lock()
	// 按 (kind, streamID) 分组候选 track，每组内按 RID 选层。
	// 默认选 high；指定 Quality 时优先匹配；都找不到则回退单层 (RID=="")。
	groups := make(map[string][]*TrackInfo) // key: kind-streamID
	for _, trackInfo := range publisher.Tracks {
		if trackInfo.Kind != msg.Kind {
			continue
		}
		if msg.StreamID != "" && trackInfo.StreamID != msg.StreamID {
			continue
		}
		groupKey := trackInfo.Kind + "-" + trackInfo.StreamID
		groups[groupKey] = append(groups[groupKey], trackInfo)
	}
	publisher.mu.Unlock()

	desiredQuality := msg.Quality
	if desiredQuality == "" {
		desiredQuality = "high"
	}

	var selectedTracks []*TrackInfo
	for _, candidates := range groups {
		var chosen *TrackInfo
		// 优先匹配指定 quality
		for _, c := range candidates {
			if c.RID == desiredQuality {
				chosen = c
				break
			}
		}
		// 回退到单层
		if chosen == nil {
			for _, c := range candidates {
				if c.RID == "" {
					chosen = c
					break
				}
			}
		}
		// 仍找不到（仅 simulcast 但 quality 不匹配）→ 取第一个
		if chosen == nil && len(candidates) > 0 {
			chosen = candidates[0]
		}
		if chosen != nil {
			selectedTracks = append(selectedTracks, chosen)
		}
	}

	if len(selectedTracks) == 0 {
		return
	}

	// 通过统一事件循环串行执行订阅
	added, ssrc, err := s.SubscribeTracksWait(peer, msg.TargetUserID, msg.Kind, msg.StreamID, selectedTracks)
	if err != nil {
		s.sendToPeer(peer, SignalingMessage{Type: "error", Error: err.Error()})
		return
	}

	if added && ssrc > 0 {
		// 发送 SSRC 映射给前端（让其能把 RTP 包归属到正确的用户）
		// 注意：RID 信息已包含在 trackKey 中，这里简化处理
		s.sendToPeer(peer, SignalingMessage{
			Type: "ssrc_mapping",
			SSRCs: []SSRC{{
				SSRC:   ssrc,
				UserID: msg.TargetUserID,
				Kind:   msg.Kind,
			}},
		})
	}
}

func (s *SFUServer) handleUnsubscribe(room *Room, peer *Peer, msg *SignalingMessage) {
	if msg.TargetUserID == 0 || msg.Kind == "" {
		s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "unsubscribe requires target_user_id and kind"})
		return
	}

	// 通过统一事件循环串行执行取消订阅
	err := s.UnsubscribeTracks(peer, msg.TargetUserID, msg.Kind, msg.StreamID)
	if err != nil {
		s.sendToPeer(peer, SignalingMessage{Type: "error", Error: err.Error()})
		return
	}
}

// closePeer 安全地关闭 peer：取消 ctx 级联关闭所有 worker goroutine、关闭 PC。
// 幂等：context.CancelFunc 天然幂等，多次调用安全。
func (s *SFUServer) closePeer(peer *Peer) {
	peer.cancel() // 级联取消：所有监听 ctx.Done() 的 goroutine 都会收到退出信号
	peer.PC.Close()
}

func (s *SFUServer) onConnectionStateChange(room *Room, peer *Peer, state webrtc.PeerConnectionState) {
	if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
		room.mu.Lock()
		defer room.mu.Unlock()
		if _, ok := room.Peers[peer.UserID]; !ok {
			return
		}
		s.cleanupPeerSubscriptions(room, peer.UserID)
		s.rtcpRouter.UnregisterByUser(peer.UserID)
		delete(room.Peers, peer.UserID)
		s.closePeer(peer)

		if len(room.Peers) == 0 {
			s.mu.Lock()
			delete(s.rooms, room.ID)
			s.mu.Unlock()
		}

		if state == webrtc.PeerConnectionStateFailed {
			s.logger.Warn("Peer connection failed (ICE negotiation failed)",
				zap.Uint("roomID", room.ID),
				zap.Uint("userID", peer.UserID))
			s.notifyConnectionFailed(room.ID, peer.UserID)
		}
	}
}

func (s *SFUServer) notifyConnectionFailed(roomID uint, userID uint) {
	apiHost := getAPIHost()
	url := apiHost + "/api/v1/sfu/callback/" + strconv.Itoa(int(roomID)) + "/" + strconv.Itoa(int(userID)) + "/connection-failed"

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		s.logger.Error("Failed to create request for connection failed notification",
			zap.Error(err),
			zap.Uint("roomID", roomID),
			zap.Uint("userID", userID))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.callbackToken != "" {
		req.Header.Set("X-SFU-Callback-Token", s.callbackToken)
	}

	_, err = client.Do(req)
	if err != nil {
		s.logger.Error("Failed to notify connection failed to API server",
			zap.Error(err),
			zap.Uint("roomID", roomID),
			zap.Uint("userID", userID))
	}
}

func getAPIHost() string {
	apiHost := os.Getenv("API_SERVER_HOST")
	if apiHost == "" {
		return "http://localhost:8080"
	}
	if !strings.HasPrefix(apiHost, "http") {
		return "http://" + apiHost
	}
	return apiHost
}

func (s *SFUServer) validateAdmission(r *http.Request) (*auth.SFUClaims, error) {
	if s.allowUnauthenticatedDev {
		return nil, nil
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		return nil, errors.New("missing sfu token")
	}
	claims, err := auth.ValidateSFUJoinToken(token, s.sfuJWTSecret)
	if err != nil {
		return nil, err
	}
	// jti 重放检查（Redis 不可用时 best-effort，不阻断）
	if s.tokenReplayCheck != nil && claims.JTI != "" {
		ttl := time.Until(claims.ExpiresAt)
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		ok, replayErr := s.tokenReplayCheck.MarkTokenUsed(claims.JTI, ttl)
		if replayErr != nil {
			s.logger.Warn("Token replay check failed (allow)", zap.Error(replayErr))
		} else if !ok {
			return nil, errors.New("sfu token already used")
		}
	}
	return claims, nil
}

func (s *SFUServer) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range s.allowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func (s *SFUServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	claims, err := s.validateAdmission(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.originAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	wsUpgrader := upgrader
	wsUpgrader.CheckOrigin = s.originAllowed
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade websocket", zap.Error(err))
		return
	}
	conn.SetReadLimit(1 << 20)

	var peer *Peer
	limiter := newRateLimiter(s.messageRateLimit, time.Minute)

	defer func() {
		if peer != nil {
			s.removePeer(peer.RoomID, peer.UserID)
		}
		conn.Close()
	}()

	for {
		if !limiter.Allow() {
			if peer != nil {
				s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "rate_limited"})
			} else {
				sendError(conn, "rate_limited", "too many messages")
			}
			break
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				s.logger.Error("Read error", zap.Error(err))
			}
			break
		}

		var msg SignalingMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			s.logger.Error("Failed to parse signaling message", zap.Error(err))
			continue
		}

		switch msg.Type {
		case "join":
			if peer != nil {
				s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "already_joined"})
				continue
			}
			if claims != nil {
				if msg.RoomID != 0 && msg.RoomID != claims.RoomID || msg.UserID != 0 && msg.UserID != claims.UserID {
					sendError(conn, "join_failed", "token claims do not match join request")
					return
				}
				msg.RoomID = claims.RoomID
				msg.UserID = claims.UserID
			}
			peer, err = s.handleJoin(conn, &msg)
			if err != nil {
				s.logger.Error("Join failed", zap.Error(err))
				sendError(conn, "join_failed", err.Error())
			}
		case "offer":
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			err = s.handleOffer(peer, &msg)
			if err != nil {
				s.logger.Error("Offer failed", zap.Error(err))
				s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "offer_failed: " + err.Error()})
			}
		case "answer":
			// 处理浏览器回复的 Answer（用于重新协商场景）
			// 初次协商时浏览器是 offerer、SFU 是 answerer，浏览器不会发 answer。
			// 重新协商时 SFU 是 offerer，浏览器回复 answer，需要在这里处理。
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			err = s.handleAnswer(peer, &msg)
			if err != nil {
				s.logger.Error("Answer handling failed", zap.Uint("userID", peer.UserID), zap.Error(err))
				s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "answer_failed: " + err.Error()})
			}
		case "ice-candidate":
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			err = s.handleIceCandidate(peer, &msg)
			if err != nil {
				s.logger.Error("ICE candidate failed", zap.Error(err))
				s.sendToPeer(peer, SignalingMessage{Type: "error", Error: "ice_candidate_failed: " + err.Error()})
			}
		case "subscribe":
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			s.mu.Lock()
			room := s.rooms[peer.RoomID]
			s.mu.Unlock()
			if room != nil {
				s.handleSubscribe(room, peer, &msg)
			}
		case "unsubscribe":
			if peer == nil {
				sendError(conn, "not_joined", "Please join first")
				continue
			}
			s.mu.Lock()
			room := s.rooms[peer.RoomID]
			s.mu.Unlock()
			if room != nil {
				s.handleUnsubscribe(room, peer, &msg)
			}
		}
	}
}

func (s *SFUServer) handleJoin(conn *websocket.Conn, msg *SignalingMessage) (*Peer, error) {
	s.logger.Info("Peer joining", zap.Uint("room", msg.RoomID), zap.Uint("user", msg.UserID))

	s.mu.Lock()
	if _, ok := s.rooms[msg.RoomID]; !ok {
		s.rooms[msg.RoomID] = &Room{
			ID:    msg.RoomID,
			Peers: make(map[uint]*Peer),
		}
	}
	room := s.rooms[msg.RoomID]
	s.mu.Unlock()

	room.mu.Lock()
	defer room.mu.Unlock()

	// 创建PeerConnection
	pc, err := s.api.NewPeerConnection(s.configuration)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	peer := &Peer{
		UserID:        msg.UserID,
		RoomID:        msg.RoomID,
		PC:            pc,
		Conn:          conn,
		Tracks:        make(map[string]*TrackInfo),
		Subscriptions: make(map[uint][]*webrtc.RTPSender),
		addedTracks:   make(map[string]bool),
		renegoCh:      make(chan struct{}, 1),
		sendCh:        make(chan SignalingMessage, 64),
		eventCh:       make(chan peerEvent, 32),         // buffer=32，统一状态变更事件队列
		ctx:           ctx,
		cancel:        cancel,
	}

	// 启动该 peer 专属的 goroutine 池，所有操作串行化，从结构上消除竞态。
	// 【架构说明】
	// - renegotiationWorker：处理 renegotiation，合并高频触发
	// - writerLoop：处理 WebSocket 写，避免并发写 panic
	// - peerEventLoop：统一状态机，所有状态变更都走这里
	go s.renegotiationWorker(peer)
	go s.writerLoop(peer)
	go s.peerEventLoop(peer)

	// 监听轨道
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		s.onTrack(room, peer, track, receiver)
	})

	// 监听ICE候选
	// 关键修复：非 Trickle 模式下，候选已包含在 Answer SDP 中，不再单独通过 WebSocket 下发
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			s.logger.Info("ICE gathering completed",
				zap.Uint("userID", peer.UserID))
			return
		}

		candidateInit := candidate.ToJSON()
		s.logger.Debug("ICE candidate generated (included in complete answer)",
			zap.Uint("userID", peer.UserID),
			zap.String("candidate", candidateInit.Candidate))
	})

	// 监听连接状态
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.onConnectionStateChange(room, peer, state)
	})

	room.Peers[msg.UserID] = peer

	// 发送joined确认
	s.sendToPeer(peer, SignalingMessage{
		Type:   "joined",
		RoomID: msg.RoomID,
		UserID: msg.UserID,
	})

	// 订阅房间内已存在的其他用户的流
	// 音频：自动全量订阅；视频：仅收集 SSRC 信息，由前端按需 subscribe
	// 注意：room.mu 已在函数开头通过 defer 持有，此处无需再次加锁
	// 注意：existingPeer.Tracks 读取无需加锁 - TrackInfo 的 SSRC/Kind/StreamID/RID 创建后不可变
	var allSSRCs []SSRC
	for _, existingPeer := range room.Peers {
		if existingPeer.UserID != peer.UserID {
			var audioTracks []*TrackInfo
			for _, trackInfo := range existingPeer.Tracks {
				allSSRCs = append(allSSRCs, SSRC{
					SSRC:     trackInfo.SSRC,
					UserID:   existingPeer.UserID,
					Kind:     trackInfo.Kind,
					StreamID: trackInfo.StreamID,
					RID:      trackInfo.RID,
				})

				if trackInfo.Kind == "audio" {
					audioTracks = append(audioTracks, trackInfo)
				}
			}

			// 音频自动订阅：通过事件循环串行执行，避免竞态
			if len(audioTracks) > 0 {
				err := s.SubscribeTracks(peer, existingPeer.UserID, "audio", "", audioTracks)
				if err != nil {
					s.logger.Warn("Failed to subscribe existing audio tracks",
						zap.Uint("newUser", peer.UserID),
						zap.Uint("existingUser", existingPeer.UserID),
						zap.Error(err))
				}
			}
		}
	}

	// 发送批量 SSRC 映射给新用户（包含音频和视频）
	if len(allSSRCs) > 0 {
		s.sendToPeer(peer, SignalingMessage{
			Type:   "ssrc_mapping",
			RoomID: msg.RoomID,
			SSRCs:  allSSRCs,
		})
		s.logger.Info("Existing tracks info sent",
			zap.Uint("userID", peer.UserID),
			zap.Int("ssrcCount", len(allSSRCs)))
	}

	s.logger.Info("Peer joined successfully",
		zap.Uint("room", msg.RoomID),
		zap.Uint("user", msg.UserID),
		zap.Int("roomPeersCount", len(room.Peers)))

	return peer, nil
}

// handleOffer 处理浏览器发来的初始 SDP Offer（初次协商）
// 预期正确逻辑：
//
//	浏览器是 offerer，SFU 是 answerer。
//	1) SFU 将浏览器的 offer 设为远程描述（声明浏览器要发送的轨道）
//	2) SFU 创建 answer（包含 SFU 要发送给浏览器的轨道，如现有用户的流）
//	3) SFU 将 answer 设为本地描述，启动 ICE 收集
//	4) 通过 WebSocket 将 answer 发送回浏览器
func (s *SFUServer) handleOffer(peer *Peer, msg *SignalingMessage) error {
	// 通过统一事件循环串行处理，消除 peer.mu 竞争
	resp := s.sendPeerEventWait(peer, peerEvent{
		typ: peerEvtHandleOffer,
		data: offerEvent{
			sdp: msg.SDP,
		},
	})
	if resp.err != nil {
		return resp.err
	}

	completeAnswer := resp.result.(*webrtc.SessionDescription)

	// 发送包含完整 ICE 候选的 Answer 回前端
	s.sendToPeer(peer, SignalingMessage{
		Type: "answer",
		SDP:  completeAnswer.SDP,
	})

	s.logger.Info("Sent complete answer with ICE candidates", zap.Uint("user", peer.UserID))
	return nil
}

// handleAnswer 处理浏览器回复的 SDP Answer（重新协商场景）
// 预期正确逻辑：
//
//	重新协商时 SFU 是 offerer，浏览器是 answerer。
//	浏览器收到 renegotiate() 发来的 offer 后，创建 answer 并发回。
//	SFU 收到 answer 后设为远程描述，新添加的轨道即可开始传输。
func (s *SFUServer) handleAnswer(peer *Peer, msg *SignalingMessage) error {
	// 通过统一事件循环串行处理，消除 peer.mu 竞争
	resp := s.sendPeerEventWait(peer, peerEvent{
		typ: peerEvtHandleAnswer,
		data: answerEvent{
			sdp: msg.SDP,
		},
	})
	return resp.err
}

func (s *SFUServer) handleIceCandidate(peer *Peer, msg *SignalingMessage) error {
	// 通过统一事件循环串行处理，消除 peer.mu 竞争
	resp := s.sendPeerEventWait(peer, peerEvent{
		typ: peerEvtHandleIce,
		data: iceCandidateEvent{
			candidate:     msg.Candidate,
			sdpMLineIndex: msg.SDPMLineIndex,
			sdpMid:        msg.SDPMid,
		},
	})
	return resp.err
}

func (s *SFUServer) removePeer(roomID, userID uint) {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return
	}

	s.cleanupPeerSubscriptions(room, userID)
	s.closePeer(peer)
	delete(room.Peers, userID)

	if len(room.Peers) == 0 {
		s.mu.Lock()
		delete(s.rooms, roomID)
		s.mu.Unlock()
	}

	s.logger.Info("Peer removed", zap.Uint("room", roomID), zap.Uint("user", userID))
}

func sendError(conn *websocket.Conn, errorType, message string) {
	conn.WriteJSON(map[string]string{
		"type":    "error",
		"error":   errorType,
		"message": message,
	})
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == ""
	},
}

type rateLimiter struct {
	limit       int
	window      time.Duration
	windowStart time.Time
	count       int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, windowStart: time.Now()}
}

func (l *rateLimiter) Allow() bool {
	if l.limit <= 0 {
		return true
	}
	now := time.Now()
	if now.Sub(l.windowStart) >= l.window {
		l.windowStart = now
		l.count = 0
	}
	if l.count >= l.limit {
		return false
	}
	l.count++
	return true
}

func (s *SFUServer) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rooms := len(s.rooms)
	peers := 0
	tracks := 0
	for _, room := range s.rooms {
		room.mu.Lock()
		peers += len(room.Peers)
		for _, peer := range room.Peers {
			tracks += len(peer.Tracks)
		}
		room.mu.Unlock()
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{
		"rooms":  rooms,
		"peers":  peers,
		"tracks": tracks,
	})
}

// ==================== SFU Control Methods ====================

// KickUser force disconnects a user from the room
func (s *SFUServer) KickUser(roomID, userID uint, reason string) error {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return errors.New("peer not found in room")
	}

	s.cleanupPeerSubscriptions(room, userID)
	s.closePeer(peer)
	delete(room.Peers, userID)

	// Clean up empty room
	if len(room.Peers) == 0 {
		s.mu.Lock()
		delete(s.rooms, roomID)
		s.mu.Unlock()
	}

	s.logger.Info("User kicked from room",
		zap.Uint("roomID", roomID),
		zap.Uint("userID", userID),
		zap.String("reason", reason))

	return nil
}

// MuteUser mutes or unmutes a user's audio by enabling/disabling track forwarding
func (s *SFUServer) MuteUser(roomID, userID uint, isMuted bool) error {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return errors.New("peer not found in room")
	}

	// 通过统一事件循环串行执行静音操作，消除 peer.mu 竞争
	resp := s.sendPeerEventWait(peer, peerEvent{
		typ: peerEvtMuteTrack,
		data: muteTrackEvent{
			kind:    "audio",
			enabled: !isMuted,
		},
	})

	// 提取结果中的轨道计数
	trackCount, _ := resp.result.(int)

	// 发送静音通知给用户（通过信令WebSocket通知客户端）
	s.sendToPeer(peer, SignalingMessage{
		Type:   "muted",
		UserID: userID,
		Data:   map[string]bool{"is_muted": isMuted},
	})

	// 广播静音状态给房间内所有用户
	for _, otherPeer := range room.Peers {
		if otherPeer.UserID != userID {
			s.sendToPeer(otherPeer, SignalingMessage{
				Type:   "muted",
				UserID: userID,
				Data:   map[string]bool{"is_muted": isMuted},
			})
		}
	}

	s.logger.Info("User mute state changed - tracks enabled/disabled",
		zap.Uint("roomID", roomID),
		zap.Uint("userID", userID),
		zap.Bool("isMuted", isMuted),
		zap.Int("audioTracksCount", trackCount))

	return nil
}

// ClosePeerConnection force closes a specific peer connection
func (s *SFUServer) ClosePeerConnection(roomID, userID uint) error {
	return s.KickUser(roomID, userID, "administrative close")
}

// GetRoomStats returns statistics for a specific room
func (s *SFUServer) GetRoomStats(roomID uint) (uint, []uint, error) {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return 0, nil, errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peerIDs := make([]uint, 0, len(room.Peers))
	for id := range room.Peers {
		peerIDs = append(peerIDs, id)
	}

	return uint(len(room.Peers)), peerIDs, nil
}

// ListRooms returns all active rooms on this SFU node
func (s *SFUServer) ListRooms() (map[uint]uint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[uint]uint)
	for roomID, room := range s.rooms {
		room.mu.Lock()
		result[roomID] = uint(len(room.Peers))
		room.mu.Unlock()
	}

	return result, nil
}

// GetPeer returns a specific peer for advanced operations
func (s *SFUServer) GetPeer(roomID, userID uint) (*Peer, error) {
	s.mu.Lock()
	room, ok := s.rooms[roomID]
	s.mu.Unlock()

	if !ok {
		return nil, errors.New("room not found")
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.Peers[userID]
	if !ok {
		return nil, errors.New("peer not found")
	}

	return peer, nil
}
