import { api } from './api'

const SFU_HOST = import.meta.env.VITE_SFU_HOST || 'http://localhost:8081'
console.log('[WebRTC] SFU host:', SFU_HOST)

interface SSRCInfo {
  ssrc: number
  user_id: number
  kind: string
  stream_id: string
}

interface SFUMessage {
  type: 'join' | 'offer' | 'answer' | 'ice-candidate' | 'joined' | 'error' | 'ssrc_mapping' | 'muted'
  room_id?: number
  user_id?: number
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
  private localStream: MediaStream | null = null
  private screenStream: MediaStream | null = null
  private remoteStreams: Map<number, MediaStream> = new Map()
  // SSRC 到 UserID 的映射表（由 SFU 信令填充）
  private ssrcToUserId: Map<number, number> = new Map()
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
  private isManualDisconnect = false

  constructor(
    roomId: number,
    userId: number,
    sfuToken: string,
    onRemoteTrack: (userId: number, track: MediaStreamTrack, stream: MediaStream) => void,
    onConnected: () => void,
    onDisconnected: () => void,
    onError: (error: string) => void
  ) {
    this.roomId = roomId
    this.userId = userId
    this.sfuToken = sfuToken
    this.onRemoteTrack = onRemoteTrack
    this.onConnected = onConnected
    this.onDisconnected = onDisconnected
    this.onError = onError
  }

  private getWsUrl(): string {
    const wsProtocol = SFU_HOST.startsWith('https') ? 'wss:' : 'ws:'
    const wsHost = SFU_HOST.replace(/^https?:\/\//, '')
    return `${wsProtocol}//${wsHost}/ws?token=${encodeURIComponent(this.sfuToken)}`
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
    }

    this.pc = new RTCPeerConnection(configuration)

    localStream.getTracks().forEach((track) => {
      this.pc!.addTrack(track, localStream)
    })

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

      // 尝试从 SSRC 映射获取 UserID
      const mappedUserId = this.ssrcToUserId.get(ssrc)

      if (mappedUserId !== undefined) {
        // 有 SSRC 映射，正确关联到用户
        this.onRemoteTrack(mappedUserId, track, stream)
      } else {
        // 暂未收到 SSRC 映射，暂存到 userId=0
        // VideoGrid 会按成员顺序分配显示
        this.onRemoteTrack(0, track, stream)
      }
    }

    this.pc.oniceconnectionstatechange = () => {
      console.log('[WebRTC] ICE connection state:', this.pc?.iceConnectionState)
      // ICE 连接失败自动重连
      if (this.pc?.iceConnectionState === 'failed' || this.pc?.iceConnectionState === 'disconnected') {
        this.scheduleReconnect()
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
    console.log('[WebRTC] Connecting to SFU WebSocket:', wsUrl)
    this.ws = new WebSocket(wsUrl)

    this.ws.onopen = () => {
      console.log('[WebRTC] Connected to SFU WebSocket, sending join...')
      this.reconnectAttempts = 0
      this.send({
        type: 'join',
        room_id: this.roomId,
        user_id: this.userId,
      })
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
      if (!this.isManualDisconnect && event.code !== 1000) {
        this.scheduleReconnect()
      }
      this.onDisconnected()
    }
  }

  private scheduleReconnect() {
    if (this.isManualDisconnect) return
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error('[WebRTC] Max reconnect attempts reached')
      this.onError('连接失败次数过多，请刷新页面重试')
      return
    }

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }

    this.reconnectAttempts++
    console.log(`[WebRTC] Scheduling reconnect (${this.reconnectAttempts}/${this.maxReconnectAttempts})...`)

    this.reconnectTimer = setTimeout(() => {
      console.log('[WebRTC] Attempting reconnect...')
      if (this.localStream) {
        this.refreshSFUToken()
          .then(() => this.connect(this.localStream!))
          .catch((err) => {
            console.error('[WebRTC] Reconnect failed:', err)
          })
      }
    }, this.reconnectDelay)
  }

  private async handleMessage(event: MessageEvent) {
    const message: SFUMessage = JSON.parse(event.data)
    console.log('[WebRTC] Received message from SFU:', message.type)

    switch (message.type) {
      case 'joined':
        console.log('[WebRTC] Joined SFU room, creating offer...')
        await this.createAndSendOffer()
        break
      case 'answer':
        console.log('[WebRTC] Received answer from SFU')
        if (message.sdp && this.pc) {
          await this.pc.setRemoteDescription({
            type: 'answer',
            sdp: message.sdp,
          })
          console.log('[WebRTC] Remote description set')
        }
        break
      case 'ice-candidate':
        console.log('[WebRTC] Received ICE candidate from SFU')
        if (message.candidate && this.pc) {
          try {
            await this.pc.addIceCandidate({
              candidate: message.candidate,
              sdpMLineIndex: message.sdp_m_line_index,
              sdpMid: message.sdp_mid,
            })
          } catch (error) {
            console.warn('[WebRTC] Failed to add ICE candidate:', error)
          }
        }
        break
      case 'error':
        console.error('[WebRTC] SFU error:', message.error)
        this.onError(message.error || 'SFU error')
        break
      case 'ssrc_mapping':
        // 处理SSRC到UserID的映射
        if (message.ssrcs && message.ssrcs.length > 0) {
          console.log('[WebRTC] Received SSRC mapping:', message.ssrcs)
          this.setSsrcMappings(message.ssrcs.map(s => ({
            ssrc: s.ssrc,
            userId: s.user_id,
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
  }

  private send(message: SFUMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    } else {
      console.warn('[WebRTC] WebSocket not ready, message not sent:', message.type)
    }
  }

  disconnect() {
    this.isManualDisconnect = true

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    if (this.ws) {
      this.ws.close(1000, 'User disconnected')
      this.ws = null
    }

    if (this.pc) {
      this.pc.close()
      this.pc = null
    }

    this.remoteStreams.clear()
  }

  muteAudio(muted: boolean) {
    if (this.localStream) {
      this.localStream.getAudioTracks().forEach((track) => {
        track.enabled = !muted
      })
    }
  }

  muteVideo(muted: boolean) {
    if (this.localStream) {
      this.localStream.getVideoTracks().forEach((track) => {
        track.enabled = !muted
      })
    }
  }

  // 动态添加轨道（用于切换摄像头/屏幕共享）
  async replaceVideoTrack(newTrack: MediaStreamTrack) {
    if (!this.pc) return
    const sender = this.pc.getSenders().find((s) => s.track?.kind === 'video')
    if (sender) {
      await sender.replaceTrack(newTrack)
    }
  }

  // 添加新的视频轨道（用于屏幕共享，不替换摄像头）
  async addVideoTrack(newTrack: MediaStreamTrack, stream: MediaStream) {
    if (!this.pc) return
    this.pc.addTrack(newTrack, stream)
    this.screenStream = stream
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
  }

  // 设置 SSRC 到 UserID 的映射（由 SFU 信令调用）
  setSsrcMapping(ssrc: number, userId: number) {
    this.ssrcToUserId.set(ssrc, userId)
    console.log(`[WebRTC] SSRC mapping set: ${ssrc} -> user ${userId}`)
  }

  // 批量设置 SSRC 映射
  setSsrcMappings(mappings: { ssrc: number; userId: number }[]) {
    mappings.forEach(({ ssrc, userId }) => {
      this.ssrcToUserId.set(ssrc, userId)
    })
    console.log(`[WebRTC] Bulk SSRC mappings set: ${mappings.length} entries`)
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
