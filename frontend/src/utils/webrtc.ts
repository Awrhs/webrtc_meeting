const SFU_HOST = import.meta.env.VITE_SFU_HOST || 'http://localhost:8081'
console.log('[WebRTC] SFU host:', SFU_HOST)

interface SFUMessage {
  type: 'join' | 'offer' | 'answer' | 'ice-candidate' | 'joined' | 'error'
  room_id?: number
  user_id?: number
  sdp?: string
  candidate?: string
  sdp_m_line_index?: number
  sdp_mid?: string
  error?: string
}

export class SFUConnection {
  private ws: WebSocket | null = null
  private pc: RTCPeerConnection | null = null
  private roomId: number
  private userId: number
  private localStream: MediaStream | null = null
  private onRemoteTrack: (track: MediaStreamTrack, stream: MediaStream) => void
  private onConnected: () => void
  private onError: (error: string) => void
  private iceCandidates: RTCIceCandidateInit[] = []

  constructor(
    roomId: number,
    userId: number,
    onRemoteTrack: (track: MediaStreamTrack, stream: MediaStream) => void,
    onConnected: () => void,
    onError: (error: string) => void
  ) {
    this.roomId = roomId
    this.userId = userId
    this.onRemoteTrack = onRemoteTrack
    this.onConnected = onConnected
    this.onError = onError
  }

  private getWsUrl(): string {
    const wsProtocol = SFU_HOST.startsWith('https') ? 'wss:' : 'ws:'
    const wsHost = SFU_HOST.replace(/^https?:\/\//, '')
    return `${wsProtocol}//${wsHost}/ws`
  }

  async connect(localStream: MediaStream): Promise<void> {
    this.localStream = localStream

    const configuration: RTCConfiguration = {
      iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
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
      event.streams.forEach((stream) => {
        stream.getTracks().forEach((track) => {
          this.onRemoteTrack(track, stream)
        })
      })
    }

    this.pc.onconnectionstatechange = () => {
      console.log('[WebRTC] Connection state:', this.pc?.connectionState)
      if (this.pc?.connectionState === 'connected') {
        this.onConnected()
      } else if (this.pc?.connectionState === 'failed') {
        this.onError('Connection failed')
      }
    }

    const wsUrl = this.getWsUrl()
    console.log('[WebRTC] Connecting to SFU WebSocket:', wsUrl)
    this.ws = new WebSocket(wsUrl)

    this.ws.onopen = () => {
      console.log('[WebRTC] Connected to SFU WebSocket, sending join...')
      this.send({
        type: 'join',
        room_id: this.roomId,
        user_id: this.userId,
      })
    }

    this.ws.onmessage = (event) => {
      this.handleMessage(event)
    }

    this.ws.onerror = (error) => {
      console.error('[WebRTC] WebSocket error:', error)
      this.onError('WebSocket connection error')
    }

    this.ws.onclose = (event) => {
      console.log('[WebRTC] WebSocket closed, code:', event.code)
    }
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

  private send(message: SFUMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    } else {
      console.warn('[WebRTC] WebSocket not ready, message not sent:', message.type)
    }
  }

  disconnect() {
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    if (this.pc) {
      this.pc.close()
      this.pc = null
    }
    if (this.localStream) {
      this.localStream.getTracks().forEach((track) => track.stop())
      this.localStream = null
    }
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
