const MAGIC = new Uint8Array([0x4D, 0x45, 0x32, 0x45]) // "ME2E"
const VERSION = 1
const HEADER_SIZE = 4 + 1 + 4 + 12 // magic + version + epoch + iv = 21 bytes

interface SendKeyState {
  epoch: number
  key: CryptoKey
}

interface ReceiveKeyState {
  epoch: number
  key: CryptoKey
}

interface Diagnostics {
  encryptedFrames: number
  decryptedFrames: number
  decryptFailures: number
  missingReceiveKeys: number
  unknownEpochs: number
  sendKeySet: number
  receiveKeySet: number
}

const receiveKeys = new Map<string, ReceiveKeyState>()
const diagnostics: Diagnostics = {
  encryptedFrames: 0,
  decryptedFrames: 0,
  decryptFailures: 0,
  missingReceiveKeys: 0,
  unknownEpochs: 0,
  sendKeySet: 0,
  receiveKeySet: 0,
}

let currentSendKey: SendKeyState | null = null
let diagnosticsSeq = 0

function report(type: string, details: Record<string, unknown> = {}) {
  diagnosticsSeq++
  postMessage({
    type: 'diagnostics',
    event: type,
    seq: diagnosticsSeq,
    diagnostics: { ...diagnostics },
    ...details,
  })
}

function encodeFrameHeader(epoch: number, iv: Uint8Array): Uint8Array {
  const header = new Uint8Array(HEADER_SIZE)
  header.set(MAGIC, 0)
  header[4] = VERSION
  const view = new DataView(header.buffer)
  view.setUint32(5, epoch, false)
  header.set(iv, 9)
  return header
}

function parseFrameHeader(data: ArrayBuffer): { epoch: number; iv: Uint8Array; payloadOffset: number } | null {
  if (data.byteLength < HEADER_SIZE) return null
  const bytes = new Uint8Array(data)
  if (bytes[0] !== 0x4D || bytes[1] !== 0x45 || bytes[2] !== 0x32 || bytes[3] !== 0x45) return null
  if (bytes[4] !== VERSION) return null
  const view = new DataView(data)
  const epoch = view.getUint32(5, false)
  const iv = bytes.slice(9, 21)
  return { epoch, iv, payloadOffset: HEADER_SIZE }
}

async function importAesKey(keyBase64: string): Promise<CryptoKey> {
  const raw = Uint8Array.from(atob(keyBase64), c => c.charCodeAt(0))
  return crypto.subtle.importKey('raw', raw.buffer as ArrayBuffer, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt'])
}

async function encryptFrame(frameData: ArrayBuffer, key: CryptoKey, epoch: number): Promise<ArrayBuffer> {
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: iv.buffer as ArrayBuffer },
    key,
    frameData
  )
  const header = encodeFrameHeader(epoch, iv)
  const result = new Uint8Array(header.length + ciphertext.byteLength)
  result.set(header, 0)
  result.set(new Uint8Array(ciphertext), header.length)
  return result.buffer as ArrayBuffer
}

async function decryptFrame(frameData: ArrayBuffer, key: CryptoKey): Promise<ArrayBuffer | null> {
  const parsed = parseFrameHeader(frameData)
  if (!parsed) return null

  const ciphertext = new Uint8Array(frameData, parsed.payloadOffset)
  try {
    return await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: parsed.iv.buffer as ArrayBuffer },
      key,
      ciphertext
    )
  } catch {
    return null
  }
}

onrtctransform = (event: RTCRtpTransformEvent) => {
  const { readable, writable, options } = event.transformer
  const direction = options?.direction

  report(direction === 'send' ? 'sender-transform-attached' : 'receiver-transform-attached', {
    trackId: options.trackId,
    kind: options.kind,
  })

  const reader = readable.getReader()
  const writer = writable.getWriter()

  const processFrame = async () => {
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        if (direction === 'send' && currentSendKey) {
          try {
            const encrypted = await encryptFrame(value.data, currentSendKey.key, currentSendKey.epoch)
            value.data = encrypted
            diagnostics.encryptedFrames++
            if (diagnostics.encryptedFrames === 1 || diagnostics.encryptedFrames % 300 === 0) {
              report('frame-encrypted')
            }
          } catch {
            diagnostics.decryptFailures++
            report('frame-encrypt-failed')
          }
        } else if (direction === 'recv') {
          const parsed = parseFrameHeader(value.data)
          if (parsed) {
            const keyEntry = receiveKeys.get(`${options.trackId}:${parsed.epoch}`)
            if (keyEntry) {
              const decrypted = await decryptFrame(value.data, keyEntry.key)
              if (decrypted) {
                value.data = decrypted
                diagnostics.decryptedFrames++
                if (diagnostics.decryptedFrames === 1 || diagnostics.decryptedFrames % 300 === 0) {
                  report('frame-decrypted')
                }
              } else {
                diagnostics.decryptFailures++
                report('frame-decrypt-failed', { trackId: options.trackId, epoch: parsed.epoch })
              }
            } else {
              diagnostics.missingReceiveKeys++
              diagnostics.unknownEpochs++
              if (diagnostics.missingReceiveKeys === 1 || diagnostics.missingReceiveKeys % 300 === 0) {
                report('missing-receive-key', { trackId: options.trackId, epoch: parsed.epoch })
              }
            }
          }
        }

        await writer.write(value)
      }
    } catch {
      report('transform-stream-closed', { direction, trackId: options.trackId })
    } finally {
      writer.releaseLock()
    }
  }

  processFrame()
}

onmessage = (event: MessageEvent) => {
  const { type, keyBase64, epoch, trackId } = event.data

  switch (type) {
    case 'set-send-key':
      importAesKey(keyBase64).then(key => {
        currentSendKey = { epoch, key }
        diagnostics.sendKeySet++
        report('send-key-set', { epoch })
      }).catch(() => {
        report('send-key-failed', { epoch })
      })
      break

    case 'set-receive-key':
      importAesKey(keyBase64).then(key => {
        receiveKeys.set(`${trackId}:${epoch}`, { epoch, key })
        diagnostics.receiveKeySet++
        report('receive-key-set', { trackId, epoch })
      }).catch(() => {
        report('receive-key-failed', { trackId, epoch })
      })
      break

    case 'remove-receive-keys':
      for (const k of receiveKeys.keys()) {
        if (k.startsWith(`${trackId}:`)) receiveKeys.delete(k)
      }
      report('receive-keys-removed', { trackId })
      break

    case 'clear':
      currentSendKey = null
      receiveKeys.clear()
      report('cleared')
      break
  }
}

export {}
