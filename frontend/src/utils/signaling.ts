import { RoomMember, SignalingMessage } from '@/types'
import { groupE2EEManager } from './encryption'
import { api } from './api'
import { MediaE2EEManager } from './mediaE2EE'

export class SignalingClient {
  private ws: WebSocket | null = null
  private handlers: Map<string, ((data: any) => void)[]> = new Map()
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private reconnectDelay = 3000
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private isManualDisconnect = false
  private roomMembers: Set<number> = new Set()
  private pendingSenderKeyTargets: Set<number> = new Set()
  private currentUserId: number = 0
  private currentRoomId: number = 0
  private mediaE2EEManager: MediaE2EEManager | null = null

  setMediaE2EEManager(manager: MediaE2EEManager | null) {
    this.mediaE2EEManager = manager
  }

  private getWsUrl(): string {
    const token = localStorage.getItem('token')
    const apiHost = import.meta.env.VITE_API_HOST || 'http://localhost:8080'
    const wsProtocol = apiHost.startsWith('https') ? 'wss:' : 'ws:'
    const wsHost = apiHost.replace(/^https?:\/\//, '')
    return `${wsProtocol}//${wsHost}/ws?token=${token}`
  }

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const token = localStorage.getItem('token')
      if (!token || token === 'null') {
        reject(new Error('No authentication token found'))
        return
      }

      this.isManualDisconnect = false

      if (this.reconnectAttempts >= this.maxReconnectAttempts) {
        reject(new Error('Max reconnect attempts reached'))
        return
      }

      const wsUrl = this.getWsUrl()
      console.log('[Signaling] Connecting to:', wsUrl.replace(/token=([^&]+)/, 'token=***'))
      this.ws = new WebSocket(wsUrl)

      this.ws.onopen = () => {
        console.log('[Signaling] WebSocket connected')
        this.reconnectAttempts = 0
        resolve()
      }

      this.ws.onerror = (error) => {
        console.error('[Signaling] WebSocket error:', error)
        reject(error)
      }

      this.ws.onmessage = (event) => {
        void this.handleMessage(event)
      }

      this.ws.onclose = (event) => {
        console.log('[Signaling] WebSocket closed, code:', event.code)
        if (!this.isManualDisconnect && event.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
          this.reconnectAttempts++
          console.log(`[Signaling] Reconnecting... (${this.reconnectAttempts}/${this.maxReconnectAttempts})`)
          if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer)
          }
          this.reconnectTimer = setTimeout(() => {
            this.connect().catch(() => {})
          }, this.reconnectDelay)
        }
      }
    })
  }

  disconnect() {
    this.isManualDisconnect = true

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    this.handlers.clear()

    if (this.ws) {
      this.ws.close(1000, 'User disconnected')
      this.ws = null
    }

    this.roomMembers.clear()
    this.pendingSenderKeyTargets.clear()
    this.reconnectAttempts = 0
    void this.deletePublicKey()
  }

  send(message: SignalingMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    } else {
      console.warn('[Signaling] WebSocket not connected, message not sent')
    }
  }

  async joinRoom(roomId: number, userId: number, members: RoomMember[] = []) {
    this.currentRoomId = roomId
    this.currentUserId = userId
    this.roomMembers = new Set(members.map(member => member.user_id).filter(memberId => memberId !== userId))

    await groupE2EEManager.init(userId)
    await this.uploadCurrentPublicKey()

    this.send({
      type: 'join_room',
      room_id: roomId,
      user_id: userId,
      data: null,
    })

    this.broadcastPublicKey()
    await this.fetchPublicKeysForMembers(Array.from(this.roomMembers))
    this.initMediaE2EE()
    await this.distributeMediaKeys(Array.from(this.roomMembers))
  }

  broadcastPublicKey() {
    const publicKey = groupE2EEManager.getIdentityPublicKey()
    this.send({
      type: 'public_key',
      room_id: this.currentRoomId,
      user_id: this.currentUserId,
      data: { public_key: publicKey },
    })
    console.log('[Signaling] Broadcasted public key for E2EE')
  }

  async addRoomMember(userId: number, publicKey?: string) {
    if (userId === this.currentUserId) return

    this.roomMembers.add(userId)

    if (publicKey) {
      await groupE2EEManager.addMember(userId, publicKey)
      await this.sendPendingSenderKey(userId)
      await this.distributeMediaKeys([userId])
      console.log(`[Signaling] E2EE shared key established with user ${userId}`)
    }
  }

  removeRoomMember(userId: number) {
    this.roomMembers.delete(userId)
    this.pendingSenderKeyTargets.delete(userId)
    groupE2EEManager.removeMember(userId)
    this.mediaE2EEManager?.removeUser(userId)
  }

  hasE2EEKey(userId: number): boolean {
    return this.roomMembers.has(userId) && groupE2EEManager.isReady()
  }

  async rotateForMembershipChange(reason: 'join' | 'leave' | 'kick', changedUserId: number, activeMemberIds: number[]) {
    const recipients = activeMemberIds.filter(userId => {
      if (userId === this.currentUserId) return false
      return reason === 'join' || userId !== changedUserId
    })
    this.roomMembers = new Set(recipients)

    if (reason !== 'join') {
      groupE2EEManager.removeMember(changedUserId)
    }

    const epoch = await groupE2EEManager.rotateSenderChain()
    this.broadcastPublicKey()
    this.send({
      type: 'key_rotation',
      room_id: this.currentRoomId,
      user_id: this.currentUserId,
      data: {
        epoch,
        reason,
        changed_user_id: changedUserId,
      },
    })

    this.mediaE2EEManager?.rotateMediaSenderKey()
    recipients.forEach(userId => this.pendingSenderKeyTargets.add(userId))
    await this.fetchPublicKeysForMembers(recipients)
    await this.sendPendingSenderKeys()
    await this.distributeMediaKeys(recipients)
  }

  async sendCurrentSenderKeyTo(userId: number) {
    const packages = await groupE2EEManager.distributeCurrentSenderKey([userId])
    const encryptedChainKey = packages.get(userId)
    if (!encryptedChainKey) {
      this.pendingSenderKeyTargets.add(userId)
      return
    }

    this.send({
      type: 'sender_key',
      room_id: this.currentRoomId,
      user_id: this.currentUserId,
      target_id: userId,
      data: {
        from_user: this.currentUserId,
        encrypted_chain_key: encryptedChainKey,
      },
    })
    this.pendingSenderKeyTargets.delete(userId)
  }

  async sendMediaSenderKeyTo(userId: number, encryptedMediaKey: string) {
    this.send({
      type: 'sender_key',
      room_id: this.currentRoomId,
      user_id: this.currentUserId,
      target_id: userId,
      data: {
        from_user: this.currentUserId,
        key_type: 'media',
        encrypted_chain_key: encryptedMediaKey,
      },
    })
  }

  on(type: string, handler: (data: any) => void) {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, [])
    }
    this.handlers.get(type)!.push(handler)
  }

  off(type: string, handler: (data: any) => void) {
    if (this.handlers.has(type)) {
      const handlers = this.handlers.get(type)!
      const index = handlers.indexOf(handler)
      if (index !== -1) {
        handlers.splice(index, 1)
      }
    }
  }

  private async handleMessage(event: MessageEvent) {
    try {
      const message = JSON.parse(event.data) as SignalingMessage

      if (message.type === 'public_key' && message.user_id !== this.currentUserId) {
        const publicKey = message.data?.public_key
        if (publicKey) {
          await this.addRoomMember(message.user_id, publicKey)
        }
      }

      if (message.type === 'sender_key' && message.data?.key_type === 'media') {
        if (!message.target_id || message.target_id === this.currentUserId) {
          await this.mediaE2EEManager?.receiveMediaSenderKey(
            message.data.from_user,
            message.data.encrypted_chain_key
          )
        }
        return
      }

      const handlers = this.handlers.get(message.type)
      if (handlers) {
        handlers.forEach(handler => handler(message))
      }
    } catch (error) {
      console.error('[Signaling] Failed to parse message:', error)
    }
  }

  private initMediaE2EE() {
    this.mediaE2EEManager?.init(
      (targetUserId, encryptedMediaKey) => {
        void this.sendMediaSenderKeyTo(targetUserId, encryptedMediaKey)
      },
      (state) => {
        console.log('[Signaling] Media E2EE state:', state)
      }
    )
  }

  private async distributeMediaKeys(userIds: number[]) {
    await this.mediaE2EEManager?.distributeMediaSenderKey(userIds)
  }

  private async sendPendingSenderKeys() {
    await Promise.all(Array.from(this.pendingSenderKeyTargets).map(userId => this.sendPendingSenderKey(userId)))
  }

  private async sendPendingSenderKey(userId: number) {
    if (!this.pendingSenderKeyTargets.has(userId)) return
    await this.sendCurrentSenderKeyTo(userId)
  }

  private async fetchPublicKeysForMembers(userIds: number[]) {
    await Promise.all(userIds.map(async (userId) => {
      try {
        const response = await api.get(`/api/v1/e2ee/public-key/${userId}`)
        if (response.data?.exists && response.data.public_key) {
          await this.addRoomMember(userId, response.data.public_key)
        }
      } catch (error) {
        console.warn(`[Signaling] Failed to fetch public key for user ${userId}`, error)
      }
    }))
  }

  private async uploadCurrentPublicKey() {
    try {
      await api.post('/api/v1/e2ee/public-key', {
        public_key: groupE2EEManager.getIdentityPublicKey(),
        key_type: 'ecdh_p256',
      })
    } catch (error) {
      console.warn('[Signaling] Failed to upload public key', error)
    }
  }

  private async deletePublicKey() {
    try {
      await api.delete('/api/v1/e2ee/public-key')
    } catch {
    }
  }
}

export default SignalingClient
