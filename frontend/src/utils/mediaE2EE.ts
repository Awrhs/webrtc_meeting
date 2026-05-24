import { arrayBufferToBase64, groupE2EEManager } from './encryption'

export type MediaE2EEState = 'disabled' | 'unsupported' | 'negotiating' | 'enabled' | 'warning'

interface ReceiverBinding {
  trackId: string
  userId?: number
}

interface MediaKeyPackage {
  key_type: 'media'
  epoch: number
  media_key: string
  media_key_iv: string
}

interface MediaE2EEDiagnostics {
  encryptedFrames: number
  decryptedFrames: number
  decryptFailures: number
  missingReceiveKeys: number
  unknownEpochs: number
  sendKeySet: number
  receiveKeySet: number
}

export const isMediaE2EESupported = (): boolean => {
  return typeof RTCRtpScriptTransform !== 'undefined'
    && 'transform' in RTCRtpSender.prototype
    && 'transform' in RTCRtpReceiver.prototype
}

export class MediaE2EEManager {
  private worker: Worker | null = null
  private epoch = 0
  private mediaKeyBase64: string | null = null
  private enabled = false
  private receiverBindings = new Map<string, ReceiverBinding>()
  private receiverKeys = new Map<string, Map<number, string>>()
  private diagnostics: MediaE2EEDiagnostics | null = null
  private onNeedSendKey?: (targetUserId: number, encryptedMediaKey: string) => void
  private onStateChange?: (state: MediaE2EEState) => void

  constructor(enabled = true) {
    this.enabled = enabled && isMediaE2EESupported()
  }

  init(
    onNeedSendKey: (targetUserId: number, encryptedMediaKey: string) => void,
    onStateChange: (state: MediaE2EEState) => void
  ) {
    this.onNeedSendKey = onNeedSendKey
    this.onStateChange = onStateChange

    if (!this.enabled) {
      this.onStateChange?.(isMediaE2EESupported() ? 'disabled' : 'unsupported')
      return
    }

    this.ensureWorker()
    if (!this.mediaKeyBase64) {
      this.rotateMediaSenderKey()
    }
  }

  isEnabled(): boolean {
    return this.enabled
  }

  getState(): MediaE2EEState {
    if (!this.enabled) return isMediaE2EESupported() ? 'disabled' : 'unsupported'
    if (!this.mediaKeyBase64) return 'negotiating'
    if (this.diagnostics && (this.diagnostics.decryptFailures > 0 || this.diagnostics.missingReceiveKeys > 0)) return 'warning'
    return 'enabled'
  }

  getDiagnostics(): MediaE2EEDiagnostics | null {
    return this.diagnostics ? { ...this.diagnostics } : null
  }

  rotateMediaSenderKey(): number {
    if (!this.enabled) return this.epoch

    this.epoch++
    this.mediaKeyBase64 = arrayBufferToBase64(crypto.getRandomValues(new Uint8Array(32)).buffer as ArrayBuffer)
    this.ensureWorker()
    this.worker?.postMessage({
      type: 'set-send-key',
      epoch: this.epoch,
      keyBase64: this.mediaKeyBase64,
    })
    this.onStateChange?.('enabled')
    return this.epoch
  }

  async distributeMediaSenderKey(targetUserIds: number[]) {
    if (!this.enabled || !this.mediaKeyBase64) return

    await Promise.all(targetUserIds.map(async (userId) => {
      if (!groupE2EEManager.hasSharedKey(userId)) return
      const wrapped = await groupE2EEManager.encryptForMember(userId, this.mediaKeyBase64!)
      const payload: MediaKeyPackage = {
        key_type: 'media',
        epoch: this.epoch,
        media_key: wrapped.ciphertext,
        media_key_iv: wrapped.iv,
      }
      this.onNeedSendKey?.(userId, JSON.stringify(payload))
    }))
  }

  async receiveMediaSenderKey(senderId: number, encryptedPackage: string) {
    if (!this.enabled) return

    const payload = JSON.parse(encryptedPackage) as MediaKeyPackage
    if (payload.key_type !== 'media') return

    const mediaKeyBase64 = await groupE2EEManager.decryptFromMember(senderId, payload.media_key, payload.media_key_iv)
    let senderKeys = this.receiverKeys.get(senderId.toString())
    if (!senderKeys) {
      senderKeys = new Map()
      this.receiverKeys.set(senderId.toString(), senderKeys)
    }
    senderKeys.set(payload.epoch, mediaKeyBase64)

    for (const binding of this.receiverBindings.values()) {
      if (binding.userId === senderId) {
        this.setReceiveKey(binding.trackId, payload.epoch, mediaKeyBase64)
      }
    }

    this.onStateChange?.('enabled')
  }

  attachSender(sender: RTCRtpSender, track: MediaStreamTrack) {
    if (!this.enabled) return
    this.ensureWorker()
    sender.transform = new RTCRtpScriptTransform(this.worker!, {
      direction: 'send',
      trackId: track.id,
      kind: track.kind,
    })
  }

  attachReceiver(receiver: RTCRtpReceiver, track: MediaStreamTrack, userId?: number) {
    if (!this.enabled) return
    this.ensureWorker()
    receiver.transform = new RTCRtpScriptTransform(this.worker!, {
      direction: 'recv',
      trackId: track.id,
      kind: track.kind,
    })
    this.receiverBindings.set(track.id, { trackId: track.id, userId })
    if (userId !== undefined) {
      this.installReceiveKeysForTrack(track.id, userId)
    }
  }

  bindReceiverTrack(trackId: string, userId: number) {
    if (!this.enabled) return
    this.receiverBindings.set(trackId, { trackId, userId })
    this.installReceiveKeysForTrack(trackId, userId)
  }

  removeUser(userId: number) {
    this.receiverKeys.delete(userId.toString())
    for (const [trackId, binding] of this.receiverBindings.entries()) {
      if (binding.userId === userId) {
        this.receiverBindings.delete(trackId)
      }
    }
  }

  clearTransforms() {
    this.receiverBindings.clear()
  }

  clear() {
    this.worker?.postMessage({ type: 'clear' })
    this.worker?.terminate()
    this.worker = null
    this.mediaKeyBase64 = null
    this.epoch = 0
    this.receiverBindings.clear()
    this.receiverKeys.clear()
  }

  private ensureWorker() {
    if (this.worker) return
    this.worker = new Worker(new URL('./mediaE2EEWorker.ts', import.meta.url), { type: 'module' })
    this.worker.onmessage = (event: MessageEvent) => {
      this.handleWorkerMessage(event.data)
    }
  }

  private handleWorkerMessage(message: any) {
    if (message?.type !== 'diagnostics') return

    this.diagnostics = message.diagnostics ?? this.diagnostics
    if (message.event === 'send-key-failed' || message.event === 'receive-key-failed' || message.event === 'frame-decrypt-failed' || message.event === 'missing-receive-key') {
      this.onStateChange?.('warning')
      return
    }
    this.onStateChange?.(this.getState())
  }

  private installReceiveKeysForTrack(trackId: string, userId: number) {
    const senderKeys = this.receiverKeys.get(userId.toString())
    if (!senderKeys) return

    for (const [epoch, keyBase64] of senderKeys.entries()) {
      this.setReceiveKey(trackId, epoch, keyBase64)
    }
  }

  private setReceiveKey(trackId: string, epoch: number, keyBase64: string) {
    this.ensureWorker()
    this.worker?.postMessage({
      type: 'set-receive-key',
      trackId,
      epoch,
      keyBase64,
    })
  }
}
