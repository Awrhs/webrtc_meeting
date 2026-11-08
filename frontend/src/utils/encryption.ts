/**
 * 端到端加密工具 - 基于 Sender Keys 的群聊 E2EE
 *
 * 核心原理 (来自 Signal Protocol / MLS RFC 9420):
 * 1. ECDH P-256 两两密钥协商：成员间的共享密钥，用于加密分发 Sender Key 与私聊消息
 * 2. 每个用户维护独立的 Sender Key Chain，HKDF 棘轮派生消息密钥，每条消息独立密钥
 * 3. ECDSA P-256 签名身份密钥：每条群聊消息带签名，接收方先验签再解密（发送者认证，
 *    防止持有同一条 Sender Key 的其他成员或服务器伪造消息）
 * 4. 乱序/丢包：缓存被跳过的消息密钥（有上限），每把消息密钥只能使用一次（重放防护）；
 *    单次允许跳过的序号数有上限（防止伪造的巨大序号拖垮 CPU）
 * 5. 解密失败绝不提交棘轮状态：伪造/损坏的消息不会让真正的消息变得无法解密
 */

// ==================== 类型定义 ====================

export interface KeyPair {
  publicKey: string
  privateKey: CryptoKey
}

export interface SigningKeyPair {
  publicKey: string
  privateKey: CryptoKey
}

export interface SenderChain {
  chainKey: string
  messageNumber: number
  publicKey: string
  epoch: number
}

export interface EncryptedMessage {
  /** 1：旧版无签名；2：带 ECDSA 签名（signature 必填） */
  version: number
  senderId: number
  epoch?: number
  messageNumber: number
  ciphertext: string
  iv: string
  /** v2：ECDSA-P256/SHA-256 对 messageSigningPayload() 的签名（base64） */
  signature?: string
  senderKeyDistribution?: {
    encryptedChainKey: string
    signature: string
  }
}

/** 成员公钥包：ECDH 协商公钥 + ECDSA 签名公钥（旧客户端只有前者） */
export interface PublicKeyBundle {
  ecdh: string
  ecdsa?: string
}

export type KeyChangedHandler = (userId: number, oldFingerprint: string, newFingerprint: string) => void

// ==================== 基础工具 ====================

export const arrayBufferToBase64 = (buffer: ArrayBuffer): string => {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary)
}

export const base64ToArrayBuffer = (base64: string): ArrayBuffer => {
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
}

const toArrayBuffer = (bytes: Uint8Array): ArrayBuffer => {
  const buffer = new ArrayBuffer(bytes.byteLength)
  new Uint8Array(buffer).set(bytes)
  return buffer
}

const textEncode = (text: string): Uint8Array => new TextEncoder().encode(text)
const textDecode = (buffer: ArrayBuffer): string => new TextDecoder().decode(buffer)

// ==================== PBKDF2 密钥派生（用于加密存储） ====================

export const pbkdf2Derive = async (
  password: string,
  salt: ArrayBuffer,
  iterations: number = 100000
): Promise<CryptoKey> => {
  const passwordKey = await crypto.subtle.importKey(
    'raw',
    toArrayBuffer(textEncode(password)),
    { name: 'PBKDF2' },
    false,
    ['deriveKey']
  )

  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations, hash: 'SHA-256' },
    passwordKey,
    { name: 'AES-GCM', length: 256 },
    true,
    ['encrypt', 'decrypt']
  )
}

// ==================== ECDH 密钥协商 ====================

export const generateKeyPair = async (): Promise<KeyPair> => {
  const keyPair = await crypto.subtle.generateKey(
    { name: 'ECDH', namedCurve: 'P-256' },
    true,
    ['deriveKey']
  )

  const publicKeyRaw = await crypto.subtle.exportKey('raw', keyPair.publicKey)
  const publicKey = arrayBufferToBase64(publicKeyRaw)

  return { publicKey, privateKey: keyPair.privateKey }
}

export const deriveSharedKey = async (
  privateKey: CryptoKey,
  peerPublicKeyBase64: string
): Promise<CryptoKey> => {
  const peerPublicKeyRaw = base64ToArrayBuffer(peerPublicKeyBase64)
  const peerPublicKey = await crypto.subtle.importKey(
    'raw',
    peerPublicKeyRaw,
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    []
  )

  return crypto.subtle.deriveKey(
    { name: 'ECDH', public: peerPublicKey },
    privateKey,
    { name: 'AES-GCM', length: 256 },
    true,
    ['encrypt', 'decrypt']
  )
}

// ==================== ECDSA 签名（发送者认证） ====================

export const generateSigningKeyPair = async (): Promise<SigningKeyPair> => {
  const keyPair = await crypto.subtle.generateKey(
    { name: 'ECDSA', namedCurve: 'P-256' },
    true,
    ['sign', 'verify']
  )
  const publicKeyRaw = await crypto.subtle.exportKey('raw', keyPair.publicKey)
  return { publicKey: arrayBufferToBase64(publicKeyRaw), privateKey: keyPair.privateKey }
}

export const signPayload = async (privateKey: CryptoKey, payload: string): Promise<string> => {
  const signature = await crypto.subtle.sign(
    { name: 'ECDSA', hash: 'SHA-256' },
    privateKey,
    toArrayBuffer(textEncode(payload))
  )
  return arrayBufferToBase64(signature)
}

export const verifyPayload = async (
  publicKeyBase64: string,
  payload: string,
  signatureBase64: string
): Promise<boolean> => {
  try {
    const publicKey = await crypto.subtle.importKey(
      'raw',
      base64ToArrayBuffer(publicKeyBase64),
      { name: 'ECDSA', namedCurve: 'P-256' },
      false,
      ['verify']
    )
    return await crypto.subtle.verify(
      { name: 'ECDSA', hash: 'SHA-256' },
      publicKey,
      base64ToArrayBuffer(signatureBase64),
      toArrayBuffer(textEncode(payload))
    )
  } catch {
    return false
  }
}

/** 消息签名覆盖的字段：发送者、epoch、序号、IV、密文——任何一项被篡改签名即失效 */
export const messageSigningPayload = (
  message: Pick<EncryptedMessage, 'senderId' | 'epoch' | 'messageNumber' | 'iv' | 'ciphertext'>
): string =>
  `e2ee-msg-v2|${message.senderId}|${message.epoch ?? 0}|${message.messageNumber}|${message.iv}|${message.ciphertext}`

// ==================== 公钥包（ECDH + ECDSA） ====================

export const encodePublicKeyBundle = (ecdh: string, ecdsa: string): string =>
  JSON.stringify({ v: 2, ecdh, ecdsa })

/** 兼容旧格式：旧客户端上传的是裸的 ECDH 公钥 base64，没有签名公钥 */
export const decodePublicKeyBundle = (value: string): PublicKeyBundle => {
  const trimmed = value.trim()
  if (trimmed.startsWith('{')) {
    try {
      const parsed = JSON.parse(trimmed)
      if (parsed && typeof parsed.ecdh === 'string') {
        return { ecdh: parsed.ecdh, ecdsa: typeof parsed.ecdsa === 'string' ? parsed.ecdsa : undefined }
      }
    } catch {
      // 不是合法 JSON，按裸公钥处理
    }
  }
  return { ecdh: value }
}

// ==================== 公钥指纹验证 ====================

/**
 * 生成公钥指纹（用于线下验证，防止中间人攻击）。
 * 对 ECDH 公钥与 ECDSA 签名公钥的拼接做 SHA-256，取前 16 字节的 32 位 hex
 * （128 bit 安全强度，分 4 组展示）。两把钥匙任何一把被替换，指纹都会变化。
 */
export const getPublicKeyFingerprint = async (publicKey: string, signingKey?: string): Promise<string> => {
  const ecdh = new Uint8Array(base64ToArrayBuffer(publicKey))
  const ecdsa = signingKey ? new Uint8Array(base64ToArrayBuffer(signingKey)) : new Uint8Array(0)
  const material = new Uint8Array(ecdh.length + ecdsa.length)
  material.set(ecdh)
  material.set(ecdsa, ecdh.length)
  const hash = await crypto.subtle.digest('SHA-256', toArrayBuffer(material))
  const bytes = new Uint8Array(hash).slice(0, 16)
  const hex = Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('').toUpperCase()
  return `${hex.slice(0, 8)}-${hex.slice(8, 16)}-${hex.slice(16, 24)}-${hex.slice(24, 32)}`
}

// ==================== HKDF 棘轮算法 ====================

export const hkdfDerive = async (
  salt: ArrayBuffer,
  inputKeyMaterial: ArrayBuffer,
  info: string,
  outputLength: number = 32
): Promise<ArrayBuffer> => {
  const hmacKey = await crypto.subtle.importKey(
    'raw',
    salt,
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign']
  )
  const prk = await crypto.subtle.sign('HMAC', hmacKey, inputKeyMaterial)

  const expandKey = await crypto.subtle.importKey(
    'raw',
    prk,
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign']
  )

  const infoBuffer = textEncode(info)
  const t = new Uint8Array(infoBuffer.length + 1)
  t.set(infoBuffer)
  t[infoBuffer.length] = 0x01

  const okm = await crypto.subtle.sign('HMAC', expandKey, t)
  return okm.slice(0, outputLength)
}

const ratchetStep = async (chainKey: string): Promise<{ nextChainKey: string; messageKey: string }> => {
  const chainKeyBuffer = base64ToArrayBuffer(chainKey)
  const zeroSalt = new Uint8Array(32)

  const messageKeyBuffer = await hkdfDerive(toArrayBuffer(zeroSalt), chainKeyBuffer, 'message', 32)
  const nextChainKeyBuffer = await hkdfDerive(toArrayBuffer(zeroSalt), chainKeyBuffer, 'chain', 32)

  return {
    nextChainKey: arrayBufferToBase64(nextChainKeyBuffer),
    messageKey: arrayBufferToBase64(messageKeyBuffer),
  }
}

// ==================== AES-GCM 加密/解密 ====================

const aesEncrypt = async (plaintext: string, keyBase64: string): Promise<{ ciphertext: string; iv: string }> => {
  const keyBuffer = base64ToArrayBuffer(keyBase64)
  const key = await crypto.subtle.importKey('raw', keyBuffer, { name: 'AES-GCM' }, false, ['encrypt'])
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const encrypted = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: toArrayBuffer(iv) }, key, toArrayBuffer(textEncode(plaintext)))
  return { ciphertext: arrayBufferToBase64(encrypted), iv: arrayBufferToBase64(toArrayBuffer(iv)) }
}

const aesDecrypt = async (ciphertext: string, iv: string, keyBase64: string): Promise<string> => {
  const keyBuffer = base64ToArrayBuffer(keyBase64)
  const key = await crypto.subtle.importKey('raw', keyBuffer, { name: 'AES-GCM' }, false, ['decrypt'])
  const encryptedBuffer = base64ToArrayBuffer(ciphertext)
  const ivBuffer = base64ToArrayBuffer(iv)
  const decrypted = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: ivBuffer }, key, encryptedBuffer)
  return textDecode(decrypted)
}

// ==================== 密钥持久化存储 ====================

const STORAGE_KEY = 'e2ee_key_store'
const STORAGE_SALT = 'e2ee_salt'

/**
 * 生成随机盐值（只在首次使用时生成一次）
 */
const getOrCreateSalt = (): ArrayBuffer => {
  let saltBase64 = localStorage.getItem(STORAGE_SALT)
  if (!saltBase64) {
    const salt = crypto.getRandomValues(new Uint8Array(16))
    saltBase64 = arrayBufferToBase64(toArrayBuffer(salt))
    localStorage.setItem(STORAGE_SALT, saltBase64)
  }
  return base64ToArrayBuffer(saltBase64)
}

// ==================== 群聊 E2EE 管理器 (Sender Keys) ====================

interface PendingDecrypt {
  msg: EncryptedMessage
  resolve: (s: string) => void
  reject: (e: Error) => void
}

export class GroupE2EEManager {
  private identityKeyPair: KeyPair | null = null
  private signingKeyPair: SigningKeyPair | null = null
  private pairwiseSharedKeys: Map<number, CryptoKey> = new Map()
  // 成员原始公钥（base64）：用于计算指纹线下比对
  private memberPublicKeys: Map<number, string> = new Map()
  // 成员 ECDSA 签名公钥（base64）：校验其消息签名；旧客户端可能没有
  private memberSigningKeys: Map<number, string> = new Map()
  private mySenderChain: SenderChain | null = null
  private peerSenderChains: Map<string, SenderChain> = new Map()
  // 被跳过的消息密钥：senderId:epoch -> (messageNumber -> messageKey)。
  // 乱序到达的旧消息用它解密，用后即删——每把密钥只能用一次，这就是重放防护。
  private skippedMessageKeys: Map<string, Map<number, string>> = new Map()
  private currentRoomMembers: Set<number> = new Set()
  private currentEpoch = 0

  // 消息缓存：Sender Key 或签名公钥尚未到达时先缓存，到达后再解密
  private pendingMessages: Map<string, PendingDecrypt[]> = new Map()

  // 解密串行化：链状态是可变的，并发解密会互相破坏棘轮位置
  private decryptQueue: Promise<unknown> = Promise.resolve()

  // 单次允许向前跳过的最大序号数：超过视为伪造/损坏（防止巨大序号拖垮 CPU）
  protected maxSkip = 500
  // 每条链最多缓存多少把跳过的消息密钥
  protected maxSkippedKeys = 256

  private onNeedSendKey?: (targetUserId: number, encryptedChainKey: string) => void
  private onStateChange?: (ready: boolean) => void
  private onFingerprintReady?: (fingerprint: string) => void
  private onKeyChanged?: KeyChangedHandler

  private senderChainKey(senderId: number, epoch: number): string {
    return `${senderId}:${epoch}`
  }

  // ============= 初始化 =============

  async init(userId?: number): Promise<void> {
    // 1) 尝试从本设备加密存储恢复（刷新页面后继续解密历史消息、保持身份公钥稳定）
    const restored = await this.tryRestoreFromDevice(userId)
    if (restored) {
      console.log('[E2EE] Keys restored from device-encrypted store')
      this.onStateChange?.(this.isReady())
      return
    }

    // 2) 生成新密钥对（首次在本设备使用）
    this.identityKeyPair = await generateKeyPair()
    this.signingKeyPair = await generateSigningKeyPair()
    console.log('[E2EE] New identity + signing key pairs generated')

    const initialChainKey = arrayBufferToBase64(toArrayBuffer(crypto.getRandomValues(new Uint8Array(32))))
    this.mySenderChain = {
      chainKey: initialChainKey,
      messageNumber: 0,
      publicKey: this.identityKeyPair.publicKey,
      epoch: this.currentEpoch,
    }

    this.onStateChange?.(this.isReady())
    this.scheduleSave()

    // 通知指纹已就绪
    this.onFingerprintReady?.(await this.getMyFingerprint())
  }

  // 设备密钥：用于加密本地的 E2EE 密钥存储（随机生成，保存在 localStorage）。
  // 注意：这只能防止直接拷贝密文，无法防御能读取 localStorage 的 XSS（文档已声明）。
  private async getOrCreateDeviceSecret(): Promise<string> {
    let secret = localStorage.getItem('e2ee_device_secret')
    if (!secret) {
      secret = arrayBufferToBase64(toArrayBuffer(crypto.getRandomValues(new Uint8Array(32))))
      localStorage.setItem('e2ee_device_secret', secret)
    }
    return secret
  }

  private async tryRestoreFromDevice(_userId?: number): Promise<boolean> {
    try {
      const stored = localStorage.getItem(STORAGE_KEY)
      if (!stored) return false
      const { ciphertext, iv } = JSON.parse(stored)
      const secret = await this.getOrCreateDeviceSecret()
      const salt = getOrCreateSalt()
      const encryptionKey = await pbkdf2Derive(secret, salt)
      const encryptionKeyRaw = await crypto.subtle.exportKey('raw', encryptionKey)
      const decrypted = await aesDecrypt(ciphertext, iv, arrayBufferToBase64(encryptionKeyRaw))
      const data = JSON.parse(decrypted)

      const privateKey = await crypto.subtle.importKey(
        'jwk',
        JSON.parse(data.identityPrivateKey),
        { name: 'ECDH', namedCurve: 'P-256' },
        true,
        ['deriveKey']
      )
      this.identityKeyPair = { publicKey: data.identityPublicKey, privateKey }
      this.currentEpoch = data.epoch ?? 0

      // 签名密钥：旧版本存储里没有，补生成一对（指纹会随之变化一次）
      if (data.signingPrivateKey && data.signingPublicKey) {
        const signingPrivateKey = await crypto.subtle.importKey(
          'jwk',
          JSON.parse(data.signingPrivateKey),
          { name: 'ECDSA', namedCurve: 'P-256' },
          true,
          ['sign']
        )
        this.signingKeyPair = { publicKey: data.signingPublicKey, privateKey: signingPrivateKey }
      } else {
        this.signingKeyPair = await generateSigningKeyPair()
      }

      // 恢复成员公钥并重新派生两两共享密钥（ECDH 结果与刷新前一致）
      for (const [userIdStr, publicKey] of data.memberPublicKeys ?? []) {
        const userId = Number(userIdStr)
        try {
          const sharedKey = await deriveSharedKey(privateKey, publicKey)
          this.pairwiseSharedKeys.set(userId, sharedKey)
          this.memberPublicKeys.set(userId, publicKey)
          this.currentRoomMembers.add(userId)
        } catch {
          // 公钥损坏时跳过，等待对方重新广播
        }
      }
      for (const [userIdStr, signingKey] of data.memberSigningKeys ?? []) {
        this.memberSigningKeys.set(Number(userIdStr), signingKey)
      }

      // 恢复对方的 Sender Chain（用于解密历史消息）
      for (const entry of data.peerChains ?? []) {
        this.peerSenderChains.set(`${entry.userId}:${entry.chain.epoch}`, entry.chain)
      }

      // 恢复自己的链后立即轮换到新 epoch：对端可能已丢失旧链，
      // 新 epoch 的首条消息（messageNumber=0）会触发 Sender Key 重新分发
      await this.rotateSenderChain()

      this.onFingerprintReady?.(await this.getMyFingerprint())
      void this.refreshFingerprintCache()
      return true
    } catch {
      return false
    }
  }

  /**
   * 保存当前密钥状态到本设备加密存储（PBKDF2 + 设备随机密钥）。
   * 刷新页面后可恢复身份密钥、签名密钥、Sender Chain 与成员公钥，继续解密历史消息。
   * 设计权衡：能读取 localStorage 的攻击者（XSS）本身就能窃取解密后的明文，
   * 此处加密主要防止密文被直接拷贝复用。
   */
  async saveToStorage(_userId: number): Promise<void> {
    if (!this.identityKeyPair) return
    try {
      const privJwk = await crypto.subtle.exportKey('jwk', this.identityKeyPair.privateKey)
      const signingPrivJwk = this.signingKeyPair
        ? await crypto.subtle.exportKey('jwk', this.signingKeyPair.privateKey)
        : null
      const data = {
        identityPublicKey: this.identityKeyPair.publicKey,
        identityPrivateKey: JSON.stringify(privJwk),
        signingPublicKey: this.signingKeyPair?.publicKey ?? null,
        signingPrivateKey: signingPrivJwk ? JSON.stringify(signingPrivJwk) : null,
        senderChain: this.mySenderChain,
        epoch: this.currentEpoch,
        peerChains: Array.from(this.peerSenderChains.entries()).map(([key, chain]) => ({
          userId: Number(key.split(':')[0]),
          chain,
        })),
        memberPublicKeys: Array.from(this.memberPublicKeys.entries()),
        memberSigningKeys: Array.from(this.memberSigningKeys.entries()),
      }
      const secret = await this.getOrCreateDeviceSecret()
      const salt = getOrCreateSalt()
      const encryptionKey = await pbkdf2Derive(secret, salt)
      const encryptionKeyRaw = await crypto.subtle.exportKey('raw', encryptionKey)
      const { ciphertext, iv } = await aesEncrypt(JSON.stringify(data), arrayBufferToBase64(encryptionKeyRaw))
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ ciphertext, iv, version: 3 }))
    } catch (e) {
      console.warn('[E2EE] saveToStorage failed', e)
    }
  }

  // 防抖保存：密钥高频变更（棘轮/成员变更）时合并写入
  private saveTimer: ReturnType<typeof setTimeout> | null = null

  private scheduleSave(): void {
    if (this.saveTimer) clearTimeout(this.saveTimer)
    this.saveTimer = setTimeout(() => {
      this.saveTimer = null
      void this.saveToStorage(0)
    }, 500)
  }

  getIdentityPublicKey(): string {
    if (!this.identityKeyPair) throw new Error('E2EE not initialized')
    return this.identityKeyPair.publicKey
  }

  getSigningPublicKey(): string {
    if (!this.signingKeyPair) throw new Error('E2EE not initialized')
    return this.signingKeyPair.publicKey
  }

  /** 上传/广播用的公钥包（ECDH + ECDSA） */
  getPublicKeyBundle(): string {
    return encodePublicKeyBundle(this.getIdentityPublicKey(), this.getSigningPublicKey())
  }

  async getMyFingerprint(): Promise<string> {
    if (!this.identityKeyPair) throw new Error('E2EE not initialized')
    return getPublicKeyFingerprint(this.identityKeyPair.publicKey, this.signingKeyPair?.publicKey)
  }

  setCallbacks(
    onNeedSendKey: (targetUserId: number, encryptedChainKey: string) => void,
    onStateChange: (ready: boolean) => void,
    onFingerprintReady?: (fingerprint: string) => void,
    onKeyChanged?: KeyChangedHandler
  ): void {
    this.onNeedSendKey = onNeedSendKey
    this.onStateChange = onStateChange
    this.onFingerprintReady = onFingerprintReady
    this.onKeyChanged = onKeyChanged
  }

  // ============= 成员管理 =============

  /**
   * 登记成员公钥并派生两两共享密钥。
   * 已知成员的公钥发生变化时（可能是对方重装/清除了密钥，也可能是中间人），
   * 通过 onKeyChanged 告警，提示用户重新核对指纹；不静默接受。
   */
  async addMember(userId: number, peerPublicKey: string, peerSigningKey?: string): Promise<void> {
    if (!this.identityKeyPair) throw new Error('E2EE not initialized')

    const prevPublicKey = this.memberPublicKeys.get(userId)
    const prevSigningKey = this.memberSigningKeys.get(userId)
    const ecdhChanged = prevPublicKey !== undefined && prevPublicKey !== peerPublicKey
    const signingChanged = prevSigningKey !== undefined && peerSigningKey !== undefined && prevSigningKey !== peerSigningKey
    if (ecdhChanged || signingChanged) {
      const oldFingerprint = await getPublicKeyFingerprint(prevPublicKey!, prevSigningKey)
      const newFingerprint = await getPublicKeyFingerprint(peerPublicKey, peerSigningKey ?? prevSigningKey)
      console.warn(`[E2EE] Identity key of user ${userId} changed: ${oldFingerprint} -> ${newFingerprint}`)
      this.onKeyChanged?.(userId, oldFingerprint, newFingerprint)
    }

    const sharedKey = await deriveSharedKey(this.identityKeyPair.privateKey, peerPublicKey)
    this.pairwiseSharedKeys.set(userId, sharedKey)
    this.memberPublicKeys.set(userId, peerPublicKey)
    if (peerSigningKey) {
      this.memberSigningKeys.set(userId, peerSigningKey)
    }
    this.currentRoomMembers.add(userId)

    console.log(`[E2EE] Established shared key with user ${userId}`)
    void this.refreshFingerprintCache()
    this.scheduleSave()
    this.checkReadyState()

    // 签名公钥到达后，之前因缺少它而缓存的消息可以处理了
    if (peerSigningKey) {
      this.tryProcessPendingMessagesForSender(userId)
    }
  }

  removeMember(userId: number): void {
    this.pairwiseSharedKeys.delete(userId)
    this.memberPublicKeys.delete(userId)
    this.memberSigningKeys.delete(userId)
    for (const key of this.peerSenderChains.keys()) {
      if (key.startsWith(`${userId}:`)) {
        this.peerSenderChains.delete(key)
        this.skippedMessageKeys.delete(key)
      }
    }
    this.currentRoomMembers.delete(userId)
    this.scheduleSave()
    this.checkReadyState()
  }

  private checkReadyState(): void {
    this.onStateChange?.(this.isReady())
  }

  isReady(): boolean {
    return this.pairwiseSharedKeys.size > 0
  }

  // 计算成员公钥指纹。未与该成员建立共享密钥时返回 null（UI 据此提示等待协商）。
  async getMemberFingerprintAsync(userId: number): Promise<string | null> {
    const publicKey = this.memberPublicKeys.get(userId)
    if (!publicKey) return null
    return getPublicKeyFingerprint(publicKey, this.memberSigningKeys.get(userId))
  }

  getMemberFingerprint(userId: number): string | null {
    if (!this.memberPublicKeys.has(userId)) return null
    // 同步版本：指纹计算是异步的，这里返回缓存的指纹
    return this.fingerprintCache.get(userId) ?? null
  }

  private fingerprintCache: Map<number, string> = new Map()

  private async refreshFingerprintCache(): Promise<void> {
    for (const [userId, publicKey] of this.memberPublicKeys.entries()) {
      const fingerprint = await getPublicKeyFingerprint(publicKey, this.memberSigningKeys.get(userId))
      this.fingerprintCache.set(userId, fingerprint)
    }
  }

  hasSharedKey(userId: number): boolean {
    return this.pairwiseSharedKeys.has(userId)
  }

  hasSigningKey(userId: number): boolean {
    return this.memberSigningKeys.has(userId)
  }

  async encryptForMember(userId: number, plaintext: string): Promise<{ ciphertext: string; iv: string }> {
    const sharedKey = this.pairwiseSharedKeys.get(userId)
    if (!sharedKey) throw new Error(`No shared key with user ${userId}`)

    const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
    return aesEncrypt(plaintext, arrayBufferToBase64(sharedKeyRaw))
  }

  async decryptFromMember(userId: number, ciphertext: string, iv: string): Promise<string> {
    const sharedKey = this.pairwiseSharedKeys.get(userId)
    if (!sharedKey) throw new Error(`No shared key with user ${userId}`)

    const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
    return aesDecrypt(ciphertext, iv, arrayBufferToBase64(sharedKeyRaw))
  }

  // ============= Sender Key 分发 =============

  async rotateSenderChain(): Promise<number> {
    if (!this.identityKeyPair) throw new Error('E2EE not initialized')

    this.currentEpoch++
    this.mySenderChain = {
      chainKey: arrayBufferToBase64(toArrayBuffer(crypto.getRandomValues(new Uint8Array(32)))),
      messageNumber: 0,
      publicKey: this.identityKeyPair.publicKey,
      epoch: this.currentEpoch,
    }
    this.onStateChange?.(this.isReady())
    this.scheduleSave()
    return this.currentEpoch
  }

  /**
   * 把当前 Sender Key 用两两共享密钥加密后打包。
   * 包本身由 AES-GCM（ECDH 共享密钥）认证：只有双方能生成/解开，无需额外签名。
   */
  async distributeCurrentSenderKey(targetUserIds?: number[]): Promise<Map<number, string>> {
    if (!this.mySenderChain) throw new Error('Sender chain not initialized')

    const encryptedKeys = new Map<number, string>()
    const targets = targetUserIds ? new Set(targetUserIds) : null

    for (const [userId, sharedKey] of this.pairwiseSharedKeys.entries()) {
      if (targets && !targets.has(userId)) continue

      const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
      const { ciphertext, iv } = await aesEncrypt(
        this.mySenderChain.chainKey,
        arrayBufferToBase64(sharedKeyRaw)
      )

      // messageNumber 是这把链密钥对应的位置：接收方从这里开始棘轮。
      // 没有它，接收方只能假设位置 0；若分发发生在发过消息之后，解密全部错位。
      const distributionPackage = JSON.stringify({
        chainKey: ciphertext,
        chainKeyIv: iv,
        publicKey: this.identityKeyPair!.publicKey,
        epoch: this.mySenderChain.epoch,
        messageNumber: this.mySenderChain.messageNumber,
      })

      encryptedKeys.set(userId, distributionPackage)
    }

    return encryptedKeys
  }

  private distributeSenderKey(): Promise<Map<number, string>> {
    return this.distributeCurrentSenderKey()
  }

  async receiveSenderKey(senderId: number, encryptedPackage: string): Promise<void> {
    const sharedKey = this.pairwiseSharedKeys.get(senderId)
    if (!sharedKey) throw new Error(`No shared key with user ${senderId}`)

    const { chainKey: encryptedChainKey, chainKeyIv, publicKey, epoch = 0, messageNumber = 0 } = JSON.parse(encryptedPackage)
    const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
    const chainKey = await aesDecrypt(
      encryptedChainKey,
      chainKeyIv,
      arrayBufferToBase64(sharedKeyRaw)
    )

    const key = this.senderChainKey(senderId, epoch)
    const existing = this.peerSenderChains.get(key)
    if (existing && existing.messageNumber >= messageNumber) {
      // 已持有同一 epoch 中位置相同或更靠后的链：不能用旧位置覆盖，
      // 否则已经推进过的棘轮被拉回，后续消息全部解不开
      console.log(`[E2EE] Ignoring sender key from user ${senderId} (epoch ${epoch}, position ${messageNumber}, have ${existing.messageNumber})`)
      this.tryProcessPendingMessages(senderId, epoch)
      return
    }

    this.peerSenderChains.set(key, {
      chainKey,
      messageNumber,
      publicKey,
      epoch,
    })
    // 链被替换，旧链缓存的跳过密钥不再适用
    this.skippedMessageKeys.delete(key)

    // 尝试解密之前缓存的消息
    this.tryProcessPendingMessages(senderId, epoch)

    console.log(`[E2EE] Installed sender key from user ${senderId}`)
    this.scheduleSave()
  }

  // ============= 待处理消息缓存 =============

  private addPendingMessage(
    senderId: number,
    msg: EncryptedMessage,
    resolve: (s: string) => void,
    reject: (e: Error) => void
  ): void {
    const key = this.senderChainKey(senderId, msg.epoch ?? 0)
    if (!this.pendingMessages.has(key)) {
      this.pendingMessages.set(key, [])
    }
    this.pendingMessages.get(key)!.push({ msg, resolve, reject })

    // 缓存最多保留 50 条，超出丢弃最早的
    if (this.pendingMessages.get(key)!.length > 50) {
      const oldest = this.pendingMessages.get(key)!.shift()
      oldest?.reject(new Error('Message expired in cache'))
    }
  }

  /**
   * 在解密队列上处理某条链缓存的消息：能解的解掉并移除；仍缺密钥的继续留着；
   * 明确失败的（签名无效、重放）直接拒绝并移除，不再无限期占用缓存。
   */
  private tryProcessPendingMessages(senderId: number, epoch: number): void {
    const key = this.senderChainKey(senderId, epoch)
    this.decryptQueue = this.decryptQueue.then(async () => {
      const pending = this.pendingMessages.get(key)
      if (!pending || pending.length === 0) return

      // 按序号排序
      pending.sort((a, b) => a.msg.messageNumber - b.msg.messageNumber)

      const remaining: PendingDecrypt[] = []
      for (const entry of pending) {
        try {
          const outcome = await this.decryptOrDefer(entry.msg)
          if (outcome.deferred) {
            remaining.push(entry)
          } else {
            entry.resolve(outcome.plaintext)
          }
        } catch (error) {
          entry.reject(error as Error)
        }
      }
      if (remaining.length > 0) {
        this.pendingMessages.set(key, remaining)
      } else {
        this.pendingMessages.delete(key)
      }
    }).catch(() => undefined) // 队列绝不能停在 rejected 状态，否则后续解密全部挂起
  }

  private tryProcessPendingMessagesForSender(senderId: number): void {
    for (const key of Array.from(this.pendingMessages.keys())) {
      if (key.startsWith(`${senderId}:`)) {
        this.tryProcessPendingMessages(senderId, Number(key.split(':')[1]))
      }
    }
  }

  // ============= 加密/解密 =============

  async encryptMessage(plaintext: string, myUserId: number): Promise<EncryptedMessage> {
    if (!this.mySenderChain) throw new Error('Sender chain not initialized')
    if (!this.signingKeyPair) throw new Error('Signing key not initialized')

    const currentMessageNumber = this.mySenderChain.messageNumber

    // 本 epoch 的首条消息：先把当前链（位置 0）分发出去，再推进棘轮。
    // 【顺序很重要】旧实现先推进再分发，对方拿到的是位置 1 的链密钥，
    // 却用它去解位置 0 的消息，必然失败。
    if (currentMessageNumber === 0) {
      const encryptedKeys = await this.distributeSenderKey()
      for (const [userId, encryptedChainKey] of encryptedKeys.entries()) {
        this.onNeedSendKey?.(userId, encryptedChainKey)
      }
    }

    const { nextChainKey, messageKey } = await ratchetStep(this.mySenderChain.chainKey)
    this.mySenderChain.chainKey = nextChainKey
    this.mySenderChain.messageNumber++

    const { ciphertext, iv } = await aesEncrypt(plaintext, messageKey)

    const unsigned = {
      version: 2,
      senderId: myUserId,
      epoch: this.mySenderChain.epoch,
      messageNumber: currentMessageNumber,
      ciphertext,
      iv,
    }
    const signature = await signPayload(this.signingKeyPair.privateKey, messageSigningPayload(unsigned))
    const message: EncryptedMessage = { ...unsigned, signature }

    this.scheduleSave()
    return message
  }

  /**
   * 解密一条群聊消息。所有解密在同一条队列上串行执行；
   * 缺少 Sender Key / 签名公钥时返回一个待定的 Promise，密钥到达后自动完成。
   */
  decryptMessage(message: EncryptedMessage): Promise<string> {
    return new Promise<string>((resolve, reject) => {
      this.decryptQueue = this.decryptQueue.then(async () => {
        try {
          const outcome = await this.decryptOrDefer(message)
          if (outcome.deferred) {
            this.addPendingMessage(message.senderId, message, resolve, reject)
            console.log(`[E2EE] Message #${message.messageNumber} from user ${message.senderId} cached, waiting for keys`)
          } else {
            resolve(outcome.plaintext)
          }
        } catch (error) {
          reject(error as Error)
        }
      }).catch(() => undefined) // 队列绝不能停在 rejected 状态，否则后续解密全部挂起
    })
  }

  /**
   * 尝试解密；缺少解密所需的密钥时返回 deferred，而不是失败。
   * 【仅在解密队列内调用】
   */
  private async decryptOrDefer(
    message: EncryptedMessage
  ): Promise<{ deferred: true } | { deferred: false; plaintext: string }> {
    const epoch = message.epoch ?? 0
    const chain = this.peerSenderChains.get(this.senderChainKey(message.senderId, epoch))
    const requiresSignature = (message.version ?? 1) >= 2
    const signingKey = this.memberSigningKeys.get(message.senderId)

    if (!chain || (requiresSignature && !signingKey)) {
      return { deferred: true }
    }
    const plaintext = await this.decryptWithChain(message, chain, requiresSignature ? signingKey : undefined)
    return { deferred: false, plaintext }
  }

  /**
   * 真正的解密：先验签，再派生消息密钥，解密成功后才提交棘轮状态。
   * 【仅在解密队列内调用】
   */
  private async decryptWithChain(message: EncryptedMessage, chain: SenderChain, signingKey?: string): Promise<string> {
    const epoch = message.epoch ?? 0
    const chainId = this.senderChainKey(message.senderId, epoch)
    const n = message.messageNumber

    // 1) 发送者认证：v2 消息必须带有效签名
    if (signingKey) {
      if (!message.signature) throw new Error('消息缺少签名')
      const valid = await verifyPayload(signingKey, messageSigningPayload(message), message.signature)
      if (!valid) throw new Error('签名无效')
    }

    // 2) 旧序号：只能用缓存的跳过密钥，用后即删（一把密钥只能用一次 → 重放被拒）
    if (n < chain.messageNumber) {
      const skipped = this.skippedMessageKeys.get(chainId)
      const messageKey = skipped?.get(n)
      if (!messageKey) throw new Error('重复或过期的消息')
      const plaintext = await aesDecrypt(message.ciphertext, message.iv, messageKey)
      skipped!.delete(n)
      return plaintext
    }

    // 3) 新序号：向前棘轮到目标序号。先在局部变量上推演，解密成功后再提交，
    //    这样伪造/损坏的消息不会让后续真正的消息变得无法解密。
    if (n - chain.messageNumber > this.maxSkip) {
      throw new Error('消息序号跳跃过大')
    }
    let chainKey = chain.chainKey
    const newlySkipped: Array<[number, string]> = []
    for (let i = chain.messageNumber; i < n; i++) {
      const step = await ratchetStep(chainKey)
      newlySkipped.push([i, step.messageKey])
      chainKey = step.nextChainKey
    }
    const { nextChainKey, messageKey } = await ratchetStep(chainKey)
    const plaintext = await aesDecrypt(message.ciphertext, message.iv, messageKey)

    // 4) 提交链状态
    this.storeSkippedKeys(chainId, newlySkipped)
    chain.chainKey = nextChainKey
    chain.messageNumber = n + 1
    this.scheduleSave()
    return plaintext
  }

  private storeSkippedKeys(chainId: string, entries: Array<[number, string]>): void {
    if (entries.length === 0) return
    let skipped = this.skippedMessageKeys.get(chainId)
    if (!skipped) {
      skipped = new Map()
      this.skippedMessageKeys.set(chainId, skipped)
    }
    for (const [n, key] of entries) {
      skipped.set(n, key)
    }
    // 超出上限时丢弃最早的（Map 保持插入顺序）
    while (skipped.size > this.maxSkippedKeys) {
      const oldest = skipped.keys().next().value as number
      skipped.delete(oldest)
    }
  }

  // ============= 清理 =============

  clear(): void {
    this.identityKeyPair = null
    this.signingKeyPair = null
    this.pairwiseSharedKeys.clear()
    this.memberPublicKeys.clear()
    this.memberSigningKeys.clear()
    this.mySenderChain = null
    this.peerSenderChains.clear()
    this.skippedMessageKeys.clear()
    this.currentRoomMembers.clear()
    this.currentEpoch = 0
    this.pendingMessages.clear()
    this.fingerprintCache.clear()
  }
}

/**
 * 安全清除 localStorage 中所有 E2EE 密钥数据（含设备密钥）。
 * 用于显式登出或用户主动清除：调用后历史密钥全部销毁，需重新进行密钥交换。
 */
export const clearLegacyKeyStore = (): void => {
  try {
    localStorage.removeItem(STORAGE_KEY)
    localStorage.removeItem(STORAGE_SALT)
    localStorage.removeItem('e2ee_device_secret')
    // 清除可能存在的旧版本 key（带 user id后缀）
    const keysToRemove: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key && (key.startsWith('e2ee_') || key.includes('_key'))) {
        keysToRemove.push(key)
      }
    }
    keysToRemove.forEach(k => localStorage.removeItem(k))
    console.log('[E2EE] Key store cleared from localStorage')
  } catch (e) {
    // 静默失败
  }
}

// 全局单例
export const groupE2EEManager = new GroupE2EEManager()
// 注意：不再在模块加载时清除持久化密钥（saveToStorage 会写入本设备加密存储）。
// 显式清除入口：authSlice.logout 与 Profile 页的“清除本设备 E2EE 密钥”按钮。
