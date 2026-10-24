const MAGIC = new Uint8Array([0x4D, 0x45, 0x32, 0x45]) // "ME2E"
const VERSION = 2
// 头部：magic(4) + version(1) + epoch(4) + counter(4) + iv(12) = 25 字节
// counter 为发送方单调递增序号，接收方据此做重放防护（滑动窗口）
const HEADER_SIZE = 4 + 1 + 4 + 4 + 12
const REPLAY_WINDOW_SIZE = 128

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
let sendCounter = 0
let diagnosticsSeq = 0

// 重放滑动窗口：trackId:epoch -> { highest, bitmap }（最低位代表 highest）
const replayWindows = new Map<string, { highest: number; bitmap: bigint }>()

// 检查并更新重放窗口。返回 true 表示首次出现（接受），false 表示重放/过旧（丢弃）。
function checkAndUpdateReplay(windowKey: string, counter: number): boolean {
  let win = replayWindows.get(windowKey)
  if (!win) {
    win = { highest: counter, bitmap: 1n }
    replayWindows.set(windowKey, win)
    return true
  }
  if (counter > win.highest) {
    const shift = counter - win.highest
    if (shift >= REPLAY_WINDOW_SIZE) {
      win.bitmap = 1n
    } else {
      win.bitmap = (win.bitmap << BigInt(shift)) | 1n
      const mask = (1n << BigInt(REPLAY_WINDOW_SIZE)) - 1n
      win.bitmap &= mask
    }
    win.highest = counter
    return true
  }
  const offset = win.highest - counter
  if (offset >= REPLAY_WINDOW_SIZE) {
    return false
  }
  const bit = 1n << BigInt(offset)
  if ((win.bitmap & bit) !== 0n) {
    return false
  }
  win.bitmap |= bit
  return true
}

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

function encodeFrameHeader(epoch: number, counter: number, iv: Uint8Array): Uint8Array {
  const header = new Uint8Array(HEADER_SIZE)
  header.set(MAGIC, 0)
  header[4] = VERSION
  const view = new DataView(header.buffer)
  view.setUint32(5, epoch, false)
  view.setUint32(9, counter, false)
  header.set(iv, 13)
  return header
}

function parseFrameHeader(data: ArrayBuffer): { epoch: number; counter: number; iv: Uint8Array; payloadOffset: number } | null {
  if (data.byteLength < HEADER_SIZE) return null
  const bytes = new Uint8Array(data)
  if (bytes[0] !== 0x4D || bytes[1] !== 0x45 || bytes[2] !== 0x32 || bytes[3] !== 0x45) return null
  if (bytes[4] !== VERSION) return null
  const view = new DataView(data)
  const epoch = view.getUint32(5, false)
  const counter = view.getUint32(9, false)
  const iv = bytes.slice(13, 25)
  return { epoch, counter, iv, payloadOffset: HEADER_SIZE }
}

async function importAesKey(keyBase64: string): Promise<CryptoKey> {
  const raw = Uint8Array.from(atob(keyBase64), c => c.charCodeAt(0))
  return crypto.subtle.importKey('raw', raw.buffer as ArrayBuffer, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt'])
}

async function encryptFrame(frameData: ArrayBuffer, key: CryptoKey, epoch: number): Promise<ArrayBuffer> {
  const iv = crypto.getRandomValues(new Uint8Array(12))
  sendCounter = (sendCounter + 1) >>> 0
  const header = encodeFrameHeader(epoch, sendCounter, iv)
  // 帧头前 13 字节（magic+version+epoch+counter，不含 IV）作为 AAD 参与认证
  const aad = header.slice(0, 13)
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv, additionalData: aad, tagLength: 128 },
    key,
    frameData
  )
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
    // AAD 必须与加密侧一致：帧头前 13 字节（magic+version+epoch+counter，不含 IV）
    const ivBuf = new Uint8Array(parsed.iv)
    const aad = new Uint8Array(frameData.slice(0, 13))
    return await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: ivBuf, additionalData: aad, tagLength: 128 },
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
              // 先解密认证（AAD 覆盖帧头），认证通过后再做重放窗口检查：
              // 解密失败的帧不污染重放窗口。
              const decrypted = await decryptFrame(value.data, keyEntry.key)
              if (decrypted !== null) {
                if (!checkAndUpdateReplay(`${options.trackId}:${parsed.epoch}`, parsed.counter)) {
                  diagnostics.unknownEpochs++
                  report('replay-detected', { trackId: options.trackId, epoch: parsed.epoch })
                  continue // 丢弃重放帧，不写入解码器
                }
                value.data = decrypted
                diagnostics.decryptedFrames++
                if (diagnostics.decryptedFrames === 1 || diagnostics.decryptedFrames % 300 === 0) {
                  report('frame-decrypted')
                }
              } else {
                diagnostics.decryptFailures++
                report('frame-decrypt-failed', { trackId: options.trackId, epoch: parsed.epoch })
                continue // 认证失败：丢弃，不将密文下发给解码器
              }
            } else {
              // fail-closed：E2EE 会话中缺密钥的帧直接丢弃，
              // 绝不把未加密/未认证内容下发给解码器（防降级攻击）；
              // 密钥分发到达（set-receive-key）后媒体自动恢复。
              diagnostics.missingReceiveKeys++
              diagnostics.unknownEpochs++
              if (diagnostics.missingReceiveKeys === 1 || diagnostics.missingReceiveKeys % 300 === 0) {
                report('missing-receive-key', { trackId: options.trackId, epoch: parsed.epoch })
              }
              continue
            }
          } else {
            // 无有效帧头：E2EE 会话中不允许明文帧，丢弃
            diagnostics.missingReceiveKeys++
            report('missing-receive-key', { trackId: options.trackId })
            continue
          }
        } else {
          // 发送方向但未设置发送密钥：fail-closed，丢弃帧不发明文
          if (diagnostics.missingReceiveKeys % 300 === 0) {
            report('send-key-missing', { trackId: options.trackId })
          }
          diagnostics.missingReceiveKeys++
          continue
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
      sendCounter = 0
      receiveKeys.clear()
      replayWindows.clear()
      report('cleared')
      break
  }
}

export {}
