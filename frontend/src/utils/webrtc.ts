import { api } from './api'
import { MediaE2EEManager } from './mediaE2EE'

const SFU_HOST = import.meta.env.VITE_SFU_HOST || 'http://localhost:8081'
console.log('[WebRTC] SFU host:', SFU_HOST)

interface SSRCInfo {
  ssrc: number
  user_id: number
  kind: string
  stream_id: string
  rid?: string
}
interface SFUMessage {
  type: 'join' | 'offer' | 'answer' | 'ice-candidate' | 'joined' | 'error'
    | 'ssrc_mapping' | 'muted' | 'video_state' | 'subscribe' | 'unsubscribe'
  room_id?: number
  user_id?: number
  target_user_id?: number
  kind?: string
  stream_id?: string
  quality?: string
  sdp?: string
  candidate?: string
  sdp_m_line_index?: number
  sdp_mid?: string
  error?: string
  ssrcs?: SSRCInfo[]
  data?: any
}

export class SFUConnection {
  private ws: WebSocket | null = null
  private pc: RTCPeerConnection | null = null
  private roomId: number
  private userId: number
  private sfuToken: string
  // 集群模式：房间所在 SFU 实例的 WebSocket 地址（join 响应下发；为空时回退 VITE_SFU_HOST）
  private sfuUrl: string
  // 远端流元信息：streamId -> { userId, kind, rid }（用于区分摄像头/屏幕共享流）
  private streamMeta: Map<string, { userId: number; kind: string; rid: string }> = new Map()
  private localStream: MediaStream | null = null
  private screenStream: MediaStream | null = null
  // 摄像头轨道（可能晚于 connect 发布：默认以音频入会，并机时才 replaceVideoTrack）
  private cameraTrack: MediaStreamTrack | null = null
  private cameraStream: MediaStream | null = null
  private cameraSender: RTCRtpSender | null = null
  private remoteStreams: Map<number, MediaStream> = new Map()
  // SSRC 到 UserID 的映射表（由 SFU 信令填充）
  private ssrcToUserId: Map<number, number> = new Map()
  // 媒体流 ID（SDP msid）到 UserID 的映射表。
  // 【重要】Pion 转发时会给每个订阅者重写 SSRC（随机新值），SFU 下发的发布者原始
  // SSRC 与本地接收到的 SSRC 永远对不上；而 msid（StreamID）被 SFU 原样保留，
  // 浏览器 event.streams[0].id 就是它，因此流归属以 msid 为准，SSRC 仅作兆底。
  private streamIdToUserId: Map<string, number> = new Map()
  // UserID 到 用户名的映射（用于显示）
  private userIdToName: Map<number, string> = new Map()
  private onRemoteTrack: (userId: number, track: MediaStreamTrack, stream: MediaStream) => void
  private onConnected: () => void
  private onDisconnected: () => void
  private onError: (error: string) => void
  private iceCandidates: RTCIceCandidateInit[] = []
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private reconnectDelay = 3000
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  // ICE 进入 disconnected 后的宽限定时器：多数 disconnected 是瞬时抖动，会自行恢复
  private iceDisconnectTimer: ReturnType<typeof setTimeout> | null = null
  private isManualDisconnect = false
  private negotiationPromise: Promise<void> = Promise.resolve()
  private pendingRemoteTracks: { ssrc: number; streamId: string; track: MediaStreamTrack; stream: MediaStream }[] = []
  private mediaE2EEManager: MediaE2EEManager | null = null
  // 已同步给 SFU 的视频订阅集合（每次 WS 连接重置）
  private subscribedVideos: Set<number> = new Set()
  // 期望的视频订阅集合（跨重连保留，joined 后重新同步）
  private desiredVideoSubscriptions: Set<number> = new Set()
  // WebSocket 未 open 时的消息缓冲：避免 join 前的 subscribe 等消息被静默丢弃
  private pendingMessages: SFUMessage[] = []

  constructor(
    roomId: number,
    userId: number,
    sfuToken: string,
    onRemoteTrack: (userId: number, track: MediaStreamTrack, stream: MediaStream) => void,
    onConnected: () => void,
    onDisconnected: () => void,
    onError: (error: string) => void,
    mediaE2EEManager?: MediaE2EEManager | null,
    sfuUrl?: string
  ) {
    this.roomId = roomId
    this.userId = userId
    this.sfuToken = sfuToken
    this.onRemoteTrack = onRemoteTrack
    this.onConnected = onConnected
    this.onDisconnected = onDisconnected
    this.onError = onError
    this.mediaE2EEManager = mediaE2EEManager ?? null
    this.sfuUrl = sfuUrl || ''
  }

  private getWsUrl(): string {
    // 集群模式下优先使用房间所在实例的地址，否则回退到全局配置
    const host = this.sfuUrl || SFU_HOST
    const wsProtocol = host.startsWith('https') || host.startsWith('wss') ? 'wss:' : 'ws:'
    const wsHost = host.replace(/^wss?:\/\//, '').replace(/^https?:\/\//, '')
    return `${wsProtocol}//${wsHost}/ws?token=${encodeURIComponent(this.sfuToken)}`
  }

  private canUseSimulcast(): boolean {
    try {
      const caps = (RTCRtpSender as any).getCapabilities?.('video')
      return typeof this.pc?.addTransceiver === 'function' && !!caps
    } catch {
      return false
    }
  }

  async connect(localStream: MediaStream): Promise<void> {
    this.isManualDisconnect = false
    this.localStream = localStream

    const stunServer = import.meta.env.VITE_STUN_SERVER || 'stun:stun.l.google.com:19302'
    const turnServer = import.meta.env.VITE_TURN_SERVER
    const turnUsername = import.meta.env.VITE_TURN_USERNAME
    const turnCredential = import.meta.env.VITE_TURN_CREDENTIAL

    const iceServers: RTCIceServer[] = [{ urls: stunServer }]

    // 如果配置了 TURN 服务器，则添加
    if (turnServer && turnUsername && turnCredential) {
      iceServers.push({
        urls: turnServer,
        username: turnUsername,
        credential: turnCredential,
      })
      console.log('[WebRTC] TURN server configured:', turnServer)
    }

    const configuration: RTCConfiguration = {
      iceServers,
      encodedInsertableStreams: this.mediaE2EEManager?.isEnabled() || undefined,
    }

    this.pc = new RTCPeerConnection(configuration)

    localStream.getTracks().forEach((track) => {
      let sender: RTCRtpSender
      if (track.kind === 'video' && this.canUseSimulcast()) {
        try {
          const transceiver = this.pc!.addTransceiver(track, {
            direction: 'sendrecv',
            streams: [localStream],
            sendEncodings: [
              { rid: 'high', maxBitrate: 1_200_000 },
              { rid: 'low', maxBitrate: 250_000, scaleResolutionDownBy: 4 },
            ],
          })
          sender = transceiver.sender
          console.log('[WebRTC] Camera track added with simulcast (high/low)')
        } catch (err) {
          console.warn('[WebRTC] addTransceiver simulcast failed, fallback to addTrack:', err)
          sender = this.pc!.addTrack(track, localStream)
        }
      } else {
        sender = this.pc!.addTrack(track, localStream)
      }
      this.mediaE2EEManager?.attachSender(sender, track)
      // localStream 里的视频轨道就是摄像头（屏幕共享从不放进 localStream）：
      // 记录 sender，供 muteVideo/unpublishCamera 精确操作，而不是对所有视频 sender 一刀切
      if (track.kind === 'video') {
        this.cameraSender = sender
        this.cameraTrack = track
        this.cameraStream = localStream
      }
    })

    // 重连场景：摄像头轨道可能晚于 connect() 发布（默认音频入会），
    // 重建 PC 后需要重新发布，否则重连后远端看不到摄像头画面。
    // 注意这里不能立即 createAndSendOffer——此时 WS 还未建立，
    // joined 之后统一发 offer（会包含新建的 transceiver）。
    if (this.cameraTrack && !localStream.getVideoTracks().some(t => t === this.cameraTrack)) {
      try {
        this.publishCameraTrack(this.cameraTrack, this.cameraStream ?? undefined)
      } catch (err) {
        console.warn('[WebRTC] Failed to re-publish camera track after reconnect:', err)
        this.cameraTrack = null
        this.cameraStream = null
      }
    }

    this.pc.onicecandidate = (event) => {
      if (event.candidate) {
        this.sendIceCandidate(event.candidate)
      }
    }

    this.pc.ontrack = (event) => {
      const track = event.track
      const stream = event.streams[0]
      if (!stream) return

      // 从RTCRtpReceiver获取SSRC（使用类型转换，因为浏览器API支持但TS类型不完整）
      const receiver = event.receiver
      const syncSource = receiver?.getSynchronizationSources?.()[0]
      const ssrc = syncSource ? (syncSource as any).ssrc ?? syncSource.source ?? 0 : 0

      console.log('[WebRTC] Received remote track:', track.kind, 'SSRC:', ssrc, 'StreamID:', stream.id)

      // 流归属优先级：msid（SFU 原样保留，可靠）> SSRC（被 Pion 重写，基本对不上）
      const mappedUserId = this.ssrcToUserId.get(ssrc) ?? this.streamIdToUserId.get(stream.id)
      this.mediaE2EEManager?.attachReceiver(receiver, track, mappedUserId)
      if (mappedUserId !== undefined) {
        this.onRemoteTrack(mappedUserId, track, stream)
        return
      }

      this.pendingRemoteTracks.push({ ssrc, streamId: stream.id, track, stream })
      this.onRemoteTrack(0, track, stream)
    }

    this.pc.oniceconnectionstatechange = () => {
      const state = this.pc?.iceConnectionState
      console.log('[WebRTC] ICE connection state:', state)
      if (state === 'failed') {
        this.clearIceDisconnectTimer()
        this.scheduleReconnect()
        return
      }
      if (state === 'disconnected') {
        // disconnected 常常是瞬时的（网络抖动、切换 Wi-Fi），ICE 会自行恢复；
        // 给 5 秒宽限，仍未恢复才重建连接，避免一次抖动就拆掉整条媒体链路
        if (!this.iceDisconnectTimer) {
          this.iceDisconnectTimer = setTimeout(() => {
            this.iceDisconnectTimer = null
            const current = this.pc?.iceConnectionState
            if (current === 'disconnected' || current === 'failed') {
              console.warn('[WebRTC] ICE still disconnected after grace period, reconnecting')
              this.scheduleReconnect()
            }
          }, 5000)
        }
        return
      }
      if (state === 'connected' || state === 'completed') {
        this.clearIceDisconnectTimer()
      }
    }

    this.pc.onconnectionstatechange = () => {
      console.log('[WebRTC] Connection state:', this.pc?.connectionState)
      if (this.pc?.connectionState === 'connected') {
        this.reconnectAttempts = 0
        this.onConnected()
      } else if (this.pc?.connectionState === 'failed') {
        this.onError('Connection failed')
        this.scheduleReconnect()
      }
    }

    const wsUrl = this.getWsUrl()
    console.log('[WebRTC] Connecting to SFU WebSocket:', wsUrl.replace(/token=[^&]+/, 'token=***'))
    this.ws = new WebSocket(wsUrl)

    this.ws.onopen = () => {
      console.log('[WebRTC] Connected to SFU WebSocket, sending join...')
      // 注意：重连计数只在 PeerConnection 真正 connected 时归零。
      // 若在这里归零，"WS 能连上但媒体一直失败"的情况会无限重连。
      this.send({
        type: 'join',
        room_id: this.roomId,
        user_id: this.userId,
      })
      // 刷新 join 前缓存的消息（如提前下发的 subscribe），join 优先已先入队
      this.flushPendingMessages()
      // 发送缓存的 ICE 候选
      while (this.iceCandidates.length > 0) {
        const candidate = this.iceCandidates.shift()!
        this.send({
          type: 'ice-candidate',
          candidate: candidate.candidate,
          sdp_m_line_index: candidate.sdpMLineIndex ?? undefined,
          sdp_mid: candidate.sdpMid ?? undefined,
        })
      }
    }

    this.ws.onmessage = (event) => {
      this.handleMessage(event)
    }

    this.ws.onerror = (error) => {
      console.error('[WebRTC] WebSocket error:', error)
      this.onError('WebSocket connection error')
    }

      this.ws.onclose = (event) => {
      console.log('[WebRTC] WebSocket closed, code:', event.code, 'reason:', event.reason)
      // 4001：同账号在其他设备/标签页入会，本连接被服务端顶替。
      // 此时不能自动重连（会反过来顶替新连接），直接停止。
      if (event.code === 4001) {
        this.isManualDisconnect = true
        this.onError('同一账号在其他设备加入了会议，本连接已被替换')
        this.onDisconnected()
        return
      }
      if (!this.isManualDisconnect && event.code !== 1000) {
        this.scheduleReconnect()
      }
      this.onDisconnected()
    }
  }

  private clearIceDisconnectTimer() {
    if (this.iceDisconnectTimer) {
      clearTimeout(this.iceDisconnectTimer)
      this.iceDisconnectTimer = null
    }
  }

  private scheduleReconnect() {
    if (this.isManualDisconnect) return
    // 已有重连计划：不重复计数。ICE failed 与 connectionState failed、WS onclose
    // 往往接连触发，旧实现会把一次故障记成两三次尝试。
    if (this.reconnectTimer) return
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error('[WebRTC] Max reconnect attempts reached')
      this.onError('连接失败次数过多，请刷新页面重试')
      return
    }

    this.reconnectAttempts++
    console.log(`[WebRTC] Scheduling reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})...`)

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (this.isManualDisconnect) return
      console.log('[WebRTC] Attempting reconnect...')
      const stream = this.localStream
      if (!stream) return
      this.refreshSFUToken()
        .then(() => {
          // 刷新 token 期间用户可能已主动离会（disconnect）：不能再把连接拉起来
          if (this.isManualDisconnect) return
          // 重连时不能销毁本地媒体（摄像头/麦克风），只重建 WS + PC；
          // closeTransport() 会把 localStream 置空，之前直接传 this.localStream!
          // 会导致 connect(null) 崩溃，重连必然失败。
          this.closeTransport(false)
          return this.connect(stream)
        })
        .catch((err) => {
          if (this.isManualDisconnect) return
          console.error('[WebRTC] Reconnect failed:', err)
          this.onError('媒体重连失败，请刷新页面重试')
        })
    }, this.reconnectDelay)
  }

  private async handleMessage(event: MessageEvent) {
    let message: SFUMessage
    try {
      message = JSON.parse(event.data)
    } catch {
      console.warn('[WebRTC] Ignoring malformed message from SFU')
      return
    }
    console.log('[WebRTC] Received message from SFU:', message.type)

    switch (message.type) {
      case 'joined':
        console.log('[WebRTC] Joined SFU room, creating offer...')
        // 新连接：SFU 侧订阅状态为空，重置已同步集合并重发期望的视频订阅
        this.subscribedVideos = new Set()
        this.syncVideoSubscriptions()
        await this.createAndSendOffer()
        break
      case 'answer':
        console.log('[WebRTC] Received answer from SFU')
        if (message.sdp) {
          // 与 offer/重协商共用同一条串行队列：setRemoteDescription 不能与进行中的协商交错
          const sdp = message.sdp
          await this.queueNegotiation(async () => {
            if (!this.pc) return
            await this.pc.setRemoteDescription({ type: 'answer', sdp })
            // 远端描述已设置：刷新此前缓存的 ICE 候选
            await this.flushPendingIceCandidates()
            console.log('[WebRTC] Remote description set')
          })
        }
        break
      case 'offer':
        console.log('[WebRTC] Received renegotiation offer from SFU')
        if (message.sdp) {
          await this.handleRemoteOffer(message.sdp)
        }
        break
      case 'ice-candidate':
        console.log('[WebRTC] Received ICE candidate from SFU')
        if (message.candidate && this.pc) {
          const cand = {
            candidate: message.candidate,
            sdpMLineIndex: message.sdp_m_line_index,
            sdpMid: message.sdp_mid,
          }
          // 远端描述未设置前候选无法应用：缓存，待 answer 设置后刷新
          if (!this.pc.remoteDescription) {
            this.pendingIceCandidates.push(cand)
          } else {
            try {
              await this.pc.addIceCandidate(cand)
            } catch (error) {
              console.warn('[WebRTC] Failed to add ICE candidate:', error)
            }
          }
        }
        break
      case 'error':
        console.error('[WebRTC] SFU error:', message.error)
        this.onError(message.error || 'SFU error')
        break
      case 'ssrc_mapping':
        // 处理流/SSRC到UserID的映射
        if (message.ssrcs && message.ssrcs.length > 0) {
          console.log('[WebRTC] Received SSRC mapping:', message.ssrcs)
          this.setSsrcMappings(message.ssrcs.map(s => ({
            ssrc: s.ssrc,
            userId: s.user_id,
            streamId: s.stream_id,
            rid: s.rid,
            kind: s.kind,
          })))
        }
        break
      case 'muted':
        // 处理静音通知（由管理员强制静音）
        console.log('[WebRTC] User mute state changed:', message.user_id, message.data)
        break
    }
  }

  private async createAndSendOffer() {
    return this.queueNegotiation(async () => {
      if (!this.pc) return

      const offer = await this.pc.createOffer({
        offerToReceiveAudio: true,
        offerToReceiveVideo: true,
      })
      await this.pc.setLocalDescription(offer)

      console.log('[WebRTC] Sending offer to SFU')
      this.send({
        type: 'offer',
        room_id: this.roomId,
        user_id: this.userId,
        sdp: offer.sdp,
      })
    })
  }

  // 远端描述设置前到达的 ICE 候选缓存（浏览器要求先有 remote description 才能应用候选）
  private pendingIceCandidates: RTCIceCandidateInit[] = []

  private async flushPendingIceCandidates() {
    const pending = this.pendingIceCandidates
    this.pendingIceCandidates = []
    for (const cand of pending) {
      try {
        await this.pc?.addIceCandidate(cand)
      } catch (error) {
        console.warn('[WebRTC] Failed to add buffered ICE candidate:', error)
      }
    }
  }

  private async handleRemoteOffer(sdp: string) {
    return this.queueNegotiation(async () => {
      if (!this.pc) return
      // glare 保护：若我方刚发出 offer（have-local-offer），先回滚再应用远端 offer。
      // 正常流程 SFU 不会与我方同时 offer，此分支仅在重连竞态下兜底。
      if (this.pc.signalingState === 'have-local-offer') {
        console.warn('[WebRTC] Glare detected, rolling back local offer')
        await this.pc.setRemoteDescription({ type: 'rollback' })
      }
      await this.pc.setRemoteDescription({ type: 'offer', sdp })
      const answer = await this.pc.createAnswer()
      await this.pc.setLocalDescription(answer)
      // answer 之前缓存的候选现在可以应用了
      await this.flushPendingIceCandidates()
      this.send({
        type: 'answer',
        room_id: this.roomId,
        user_id: this.userId,
        sdp: answer.sdp,
      })
    })
  }

  private queueNegotiation(operation: () => Promise<void>) {
    this.negotiationPromise = this.negotiationPromise.then(operation).catch((error) => {
      console.error('[WebRTC] Negotiation failed:', error)
      this.onError('WebRTC negotiation failed')
    })
    return this.negotiationPromise
  }

  private sendIceCandidate(candidate: RTCIceCandidate) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this.iceCandidates.push(candidate.toJSON())
      return
    }

    this.send({
      type: 'ice-candidate',
      candidate: candidate.candidate,
      sdp_m_line_index: candidate.sdpMLineIndex ?? undefined,
      sdp_mid: candidate.sdpMid ?? undefined,
    })
  }

  private async refreshSFUToken() {
    const response = await api.post(`/api/v1/rooms/${this.roomId}/sfu-token`)
    this.sfuToken = response.data.sfu_token
    // 集群模式：实例可能变化，同步更新
    if (response.data.sfu_url) {
      this.sfuUrl = response.data.sfu_url
    }
  }

  private send(message: SFUMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    } else if (this.ws && this.ws.readyState === WebSocket.CONNECTING) {
      // 连接建立中：缓存消息，onopen 后统一刷新（防止订阅消息在 join 前丢失）
      this.pendingMessages.push(message)
    } else {
      console.warn('[WebRTC] WebSocket not ready, message not sent:', message.type)
    }
  }

  private flushPendingMessages() {
    while (this.pendingMessages.length > 0) {
      const msg = this.pendingMessages.shift()!
      // join 已由 onopen 自行发送，这里只刷新其余消息（如提前下发的 subscribe）
      if (msg.type !== 'join' && this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify(msg))
      }
    }
  }

  subscribeToUser(targetUserId: number, kind: string, streamId?: string, quality?: string) {
    this.send({
      type: 'subscribe',
      target_user_id: targetUserId,
      kind,
      stream_id: streamId,
      quality,
    })
    console.log('[WebRTC] Subscribed to', targetUserId, kind, streamId || '', quality || '')
  }

  unsubscribeFromUser(targetUserId: number, kind: string, streamId?: string) {
    this.send({
      type: 'unsubscribe',
      target_user_id: targetUserId,
      kind,
      stream_id: streamId,
    })
    console.log('[WebRTC] Unsubscribed from', targetUserId, kind, streamId || '')
  }

  // 手动选择某成员的视频质量层：'auto'（自适应）| 'high' | 'low'
  setVideoQuality(targetUserId: number, quality: 'auto' | 'high' | 'low') {
    this.subscribeToUser(targetUserId, 'video', undefined, quality)
    console.log('[WebRTC] Quality for', targetUserId, 'set to', quality)
  }

  setVideoSubscriptions(userIds: number[]) {
    const newSet = new Set(userIds)
    // 期望集合跨重连保留；WS 未连接时发送会被丢弃，joined 后会统一重发
    this.desiredVideoSubscriptions = newSet
    this.syncVideoSubscriptions()
  }

  // 将期望订阅集与已同步集做差量同步到 SFU。
  // 退订时带上摄像头流 ID：只退订摄像头，保留自动订阅的屏幕共享流。
  private syncVideoSubscriptions() {
    // WS 未就绪时不发送：joined 之后会统一重新同步。
    // 否则 open 前被缓存的一份与 joined 后重发的一份会让每个订阅发两次。
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    const newSet = this.desiredVideoSubscriptions
    for (const id of newSet) {
      if (!this.subscribedVideos.has(id)) {
        this.subscribeToUser(id, 'video')
      }
    }
    for (const id of this.subscribedVideos) {
      if (!newSet.has(id)) {
        const cameraStreamId = this.cameraStreamIdOf(id)
        this.unsubscribeFromUser(id, 'video', cameraStreamId || undefined)
      }
    }
    this.subscribedVideos = new Set(newSet)
  }

  // 根据流元信息找到某用户的摄像头流 ID（带 rid 的视频流）；找不到返回空串。
  private cameraStreamIdOf(userId: number): string {
    for (const [streamId, meta] of this.streamMeta.entries()) {
      if (meta.userId === userId && meta.kind === 'video' && meta.rid) {
        return streamId
      }
    }
    return ''
  }

  disconnect() {
    this.isManualDisconnect = true

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    this.closeTransport()
  }

  // closeTransport 重建传输层。stopLocalMedia=false 用于重连场景：
  // 保留摄像头/麦克风轨道（不调用 track.stop()），重连后重新发布。
  private closeTransport(stopLocalMedia = true) {
    this.clearIceDisconnectTimer()

    if (this.ws) {
      const ws = this.ws
      this.ws = null
      // 先摘掉回调再关闭：旧连接的 onclose/onerror 不能再触发 onDisconnected 或重连，
      // 否则重连成功后旧连接的关闭事件会把新连接的状态搅乱
      ws.onopen = null
      ws.onmessage = null
      ws.onerror = null
      ws.onclose = null
      ws.close(1000, 'Connection reset')
    }

    if (this.pc) {
      const pc = this.pc
      this.pc = null
      pc.onicecandidate = null
      pc.ontrack = null
      pc.oniceconnectionstatechange = null
      pc.onconnectionstatechange = null
      pc.close()
    }

    if (stopLocalMedia) {
      // 停止所有本地媒体轨道，释放摄像头/麦克风
      if (this.localStream) {
        this.localStream.getTracks().forEach(track => track.stop())
        this.localStream = null
      }
      if (this.screenStream) {
        this.screenStream.getTracks().forEach(track => track.stop())
        this.screenStream = null
      }
      if (this.cameraTrack) {
        this.cameraTrack.stop()
        this.cameraTrack = null
      }
      if (this.cameraStream) {
        this.cameraStream.getTracks().forEach(track => track.stop())
        this.cameraStream = null
      }
      this.cameraSender = null
    }

    this.iceCandidates = []
    this.remoteStreams.clear()
    this.ssrcToUserId.clear()
    this.streamIdToUserId.clear()
    this.streamMeta.clear()
    // PC 已销毁：sender 引用随之失效（重连后由 publishCameraTrack 重建）
    this.cameraSender = null
    this.pendingRemoteTracks = []
    this.pendingIceCandidates = []
    this.subscribedVideos.clear()
    this.negotiationPromise = Promise.resolve()
    if (stopLocalMedia) {
      this.mediaE2EEManager?.clearTransforms()
    }
  }

  // 静音/取消静音基于 PC 中实际发送的轨道操作，而不是记录在案的 stream 对象：
  // 摄像头轨道可能晚于 connect() 发布（replaceVideoTrack），
  // 旧实现读取 this.localStream 会导致摄像头轨道永远不会被正确启停。
  muteAudio(muted: boolean) {
    this.pc?.getSenders()
      .filter(s => s.track?.kind === 'audio')
      .forEach(s => {
        if (s.track) s.track.enabled = !muted
      })
    if (this.localStream) {
      this.localStream.getAudioTracks().forEach((track) => {
        track.enabled = !muted
      })
    }
  }

  // 只作用于摄像头轨道：屏幕共享是独立的视频 sender，不能被"关摄像头"一并关掉
  muteVideo(muted: boolean) {
    if (this.cameraTrack) {
      this.cameraTrack.enabled = !muted
    }
    const senderTrack = this.cameraSender?.track
    if (senderTrack && senderTrack !== this.cameraTrack) {
      senderTrack.enabled = !muted
    }
  }

  // 关闭摄像头时真正停止发布（replaceTrack(null)），不再发送黑帧浪费上行带宽；
  // 重新开启时用 replaceTrack 恢复同一 sender/SSRC，SFU 无需重新协商。
  async unpublishCamera(): Promise<void> {
    if (!this.pc || !this.cameraSender || !this.cameraTrack) return
    try {
      await this.cameraSender.replaceTrack(null)
    } catch (err) {
      console.warn('[WebRTC] unpublishCamera failed:', err)
    }
  }

  // 动态添加/替换摄像头视频轨道（默认入会时不携带视频，开摄像头时才发布）
  async replaceVideoTrack(newTrack: MediaStreamTrack, stream?: MediaStream) {
    if (!this.pc) return

    // 已存在摄像头 sender：直接替换轨道，无需重新协商。
    // 注：unpublishCamera 后 sender 保留（track 为 null），republish 同样走这里。
    const existingSender = this.cameraSender
      ?? this.pc.getSenders().find((s) => s.track?.kind === 'video')
    if (existingSender && (existingSender.track === null || existingSender.track?.kind === 'video')) {
      await existingSender.replaceTrack(newTrack)
      this.cameraSender = existingSender
      this.cameraTrack = newTrack
      if (stream) this.cameraStream = stream
      this.mediaE2EEManager?.attachSender(existingSender, newTrack)
      return
    }

    // 不存在视频 sender（默认音频入会）：必须新建 transceiver 发布视频轨道，
    // 否则视频根本不会被发送给 SFU（旧实现在这里直接 no-op，导致开摄像头无效）。
    // 摄像头默认两层 simulcast，与 connect() 内首次携带视频的行为保持一致。
    this.publishCameraTrack(newTrack, stream)

    // 新增 m-line 需要重新协商
    await this.createAndSendOffer()
  }

  // 在当前 PC 上发布摄像头轨道（新建 simulcast transceiver 或普通 addTrack）。
  // 不触发协商，由调用方决定何时 createAndSendOffer。
  private publishCameraTrack(newTrack: MediaStreamTrack, stream?: MediaStream) {
    if (!this.pc) return

    let sender: RTCRtpSender | null = null
    if (this.canUseSimulcast()) {
      try {
        const transceiver = this.pc.addTransceiver(newTrack, {
          direction: 'sendrecv',
          streams: stream ? [stream] : [],
          sendEncodings: [
            { rid: 'high', maxBitrate: 1_200_000 },
            { rid: 'low', maxBitrate: 250_000, scaleResolutionDownBy: 4 },
          ],
        })
        sender = transceiver.sender
        this.mediaE2EEManager?.attachSender(transceiver.sender, newTrack)
        console.log('[WebRTC] Camera track published with simulcast (high/low)')
      } catch (err) {
        console.warn('[WebRTC] addTransceiver simulcast failed, fallback to addTrack:', err)
        sender = this.pc.addTrack(newTrack, stream ?? new MediaStream([newTrack]))
        this.mediaE2EEManager?.attachSender(sender, newTrack)
      }
    } else {
      sender = this.pc.addTrack(newTrack, stream ?? new MediaStream([newTrack]))
      this.mediaE2EEManager?.attachSender(sender, newTrack)
    }

    this.cameraSender = sender
    this.cameraTrack = newTrack
    if (stream) this.cameraStream = stream
  }

  // 添加新的视频轨道（用于屏幕共享，不替换摄像头）
  async addVideoTrack(newTrack: MediaStreamTrack, stream: MediaStream) {
    if (!this.pc) return
    const sender = this.pc.addTrack(newTrack, stream)
    this.mediaE2EEManager?.attachSender(sender, newTrack)
    this.screenStream = stream
    await this.createAndSendOffer()
  }

  // 移除屏幕共享轨道
  async removeScreenTrack() {
    if (!this.pc || !this.screenStream) return
    const senders = this.pc.getSenders()
    for (const sender of senders) {
      if (sender.track && this.screenStream.getTracks().includes(sender.track)) {
        this.pc.removeTrack(sender)
      }
    }
    this.screenStream = null
    await this.createAndSendOffer()
  }

  // 设置 SSRC 到 UserID 的映射（由 SFU 信令调用）
  setSsrcMapping(ssrc: number, userId: number) {
    this.ssrcToUserId.set(ssrc, userId)
    console.log(`[WebRTC] SSRC mapping set: ${ssrc} -> user ${userId}`)
  }

  // 批量设置 SSRC / 媒体流ID 映射，并尝试归属此前未识别的远端轨道
  setSsrcMappings(mappings: { ssrc: number; userId: number; streamId?: string; rid?: string; kind?: string }[]) {
    mappings.forEach(({ ssrc, userId, streamId, rid, kind }) => {
      this.ssrcToUserId.set(ssrc, userId)
      if (streamId) {
        this.streamIdToUserId.set(streamId, userId)
        this.streamMeta.set(streamId, { userId, kind: kind || 'video', rid: rid || '' })
      }
    })
    this.pendingRemoteTracks = this.pendingRemoteTracks.filter(({ ssrc, streamId, track, stream }) => {
      const userId = this.ssrcToUserId.get(ssrc) ?? this.streamIdToUserId.get(streamId)
      if (userId === undefined) return true
      this.mediaE2EEManager?.bindReceiverTrack(track.id, userId)
      this.onRemoteTrack(userId, track, stream)
      return false
    })
    console.log(`[WebRTC] Bulk SSRC mappings set: ${mappings.length} entries`)
  }

  // 查询某个远端流的元信息（kind: audio/video，rid: simulcast 层标识，空串表示屏幕共享/单层流）
  getStreamMeta(streamId: string): { userId: number; kind: string; rid: string } | undefined {
    return this.streamMeta.get(streamId)
  }

  // 设置用户名映射
  setUserName(userId: number, name: string) {
    this.userIdToName.set(userId, name)
  }

  getConnectionState(): string {
    return this.pc?.connectionState || 'disconnected'
  }
}

export const getMediaStream = async (video: boolean, audio: boolean): Promise<MediaStream> => {
  return navigator.mediaDevices.getUserMedia({
    video: video
      ? {
          width: { ideal: 1920 },
          height: { ideal: 1080 },
        }
      : false,
    audio: audio
      ? {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        }
      : false,
  })
}

export const enumerateDevices = async (): Promise<{
  videoinputs: MediaDeviceInfo[]
  audioinputs: MediaDeviceInfo[]
  audiooutputs: MediaDeviceInfo[]
}> => {
  const devices = await navigator.mediaDevices.enumerateDevices()
  return {
    videoinputs: devices.filter((d) => d.kind === 'videoinput'),
    audioinputs: devices.filter((d) => d.kind === 'audioinput'),
    audiooutputs: devices.filter((d) => d.kind === 'audiooutput'),
  }
}
