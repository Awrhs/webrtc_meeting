/**
 * 端到端加密工具 - 基于 MLS (Messaging Layer Security) 的 Sender Keys 方案
 *
 * 核心原理 (来自 Signal Protocol / MLS RFC 9420):
 * 1. ECDH P-256 两两密钥协商
 * 2. 每个用户维护独立的 Sender Key Chain
 * 3. HKDF 棘轮派生消息密钥，每条消息独立密钥
 * 4. 首次发送时分发 Sender Key，后续自动棘轮
 */

// ==================== 类型定义 ====================

export interface KeyPair {
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
  version: number
  senderId: number
  epoch?: number
  messageNumber: number
  ciphertext: string
  iv: string
  senderKeyDistribution?: {
    encryptedChainKey: string
    signature: string
  }
}

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

// ==================== 公钥指纹验证 ====================

/**
 * 生成公钥指纹（用于线下验证，防止中间人攻击）
 * 格式：XXXX-XXXX  8位可读字符串
 */
export const getPublicKeyFingerprint = async (publicKey: string): Promise<string> => {
  const publicKeyRaw = base64ToArrayBuffer(publicKey)
  const hash = await crypto.subtle.digest('SHA-256', publicKeyRaw)
  const hashBase64 = arrayBufferToBase64(hash)
  const fingerprint = hashBase64.slice(0, 8).toUpperCase()
  return `${fingerprint.slice(0, 4)}-${fingerprint.slice(4, 8)}`
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

/**
 * 加密保存所有密钥状态到 localStorage
 * 使用用户密码作为加密密钥
 */
export const saveKeyStore = async (
  password: string,
  data: {
    myIdentityPublicKey: string
    myIdentityPrivateKey: string
    mySenderChain: SenderChain | null
    peerSenderChains: Array<{ userId: number; chain: SenderChain }>
  }
): Promise<void> => {
  const salt = getOrCreateSalt()
  const encryptionKey = await pbkdf2Derive(password, salt)
  const encryptionKeyRaw = await crypto.subtle.exportKey('raw', encryptionKey)
  const { ciphertext, iv } = await aesEncrypt(JSON.stringify(data), arrayBufferToBase64(encryptionKeyRaw))

  localStorage.setItem(STORAGE_KEY, JSON.stringify({ ciphertext, iv, version: 1 }))
}

/**
 * 从 localStorage 解密恢复密钥状态
 */
export const loadKeyStore = async (
  password: string
): Promise<{
  myIdentityPublicKey?: string
  myIdentityPrivateKey: string
  mySenderChain: SenderChain | null
  peerSenderChains: Array<{ userId: number; chain: SenderChain }>
} | null> => {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (!stored) return null

  try {
    const { ciphertext, iv } = JSON.parse(stored)
    const salt = getOrCreateSalt()
    const encryptionKey = await pbkdf2Derive(password, salt)
    const encryptionKeyRaw = await crypto.subtle.exportKey('raw', encryptionKey)
    const decrypted = await aesDecrypt(ciphertext, iv, arrayBufferToBase64(encryptionKeyRaw))
    return JSON.parse(decrypted)
  } catch {
    // 密码错误或数据损坏
    return null
  }
}

/**
 * 检查是否有已保存的密钥状态
 */
export const hasSavedKeyStore = (): boolean => {
  return localStorage.getItem(STORAGE_KEY) !== null
}

// ==================== 群聊 E2EE 管理器 (Sender Keys) ====================

export class GroupE2EEManager {
  private identityKeyPair: KeyPair | null = null
  private pairwiseSharedKeys: Map<number, CryptoKey> = new Map()
  // 成员原始公钥（base64）：用于计算指纹线下比对
  private memberPublicKeys: Map<number, string> = new Map()
  private mySenderChain: SenderChain | null = null
  private peerSenderChains: Map<string, SenderChain> = new Map()
  private currentRoomMembers: Set<number> = new Set()
  private currentEpoch = 0

  // 消息缓存：用于处理序号跳跃/丢包
  private pendingMessages: Map<string, Array<{ msg: EncryptedMessage; resolve: (s: string) => void; reject: (e: Error) => void }>> = new Map()

  // 重放滑动窗口：senderId|epoch -> { highest, bitmap }
  // bitmap 的最低位代表 highest，向上代表 highest-1, highest-2, ...
  private replayWindows: Map<string, { highest: number; bitmap: bigint }> = new Map()
  private readonly REPLAY_WINDOW_SIZE = 128

  private onNeedSendKey?: (targetUserId: number, encryptedChainKey: string) => void
  private onStateChange?: (ready: boolean) => void
  private onFingerprintReady?: (fingerprint: string) => void

  private senderChainKey(senderId: number, epoch: number): string {
    return `${senderId}:${epoch}`
  }

  /**
   * 检查并更新重放窗口。返回 true 表示首次出现（接受），false 表示重放（拒绝）。
   */
  private checkAndUpdateReplay(senderId: number, epoch: number, messageNumber: number): boolean {
    const key = `${senderId}|${epoch}`
    const win = this.replayWindows.get(key)
    if (!win) {
      this.replayWindows.set(key, { highest: messageNumber, bitmap: 1n })
      return true
    }
    if (messageNumber > win.highest) {
      const shift = messageNumber - win.highest
      if (shift >= this.REPLAY_WINDOW_SIZE) {
        // 跳跃过大，重置窗口
        win.bitmap = 1n
      } else {
        win.bitmap = (win.bitmap << BigInt(shift)) | 1n
        // 截断到窗口大小
        const mask = (1n << BigInt(this.REPLAY_WINDOW_SIZE)) - 1n
        win.bitmap = win.bitmap & mask
      }
      win.highest = messageNumber
      return true
    }
    const offset = win.highest - messageNumber
    if (offset >= this.REPLAY_WINDOW_SIZE) {
      // 太旧，超出窗口
      return false
    }
    const bit = 1n << BigInt(offset)
    if ((win.bitmap & bit) !== 0n) {
      // 已置位 → 重放
      return false
    }
    win.bitmap = win.bitmap | bit
    return true
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
    console.log('[E2EE] New identity key pair generated')

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
    const fingerprint = await getPublicKeyFingerprint(this.identityKeyPair.publicKey)
    this.onFingerprintReady?.(fingerprint)
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

      // 恢复对方的 Sender Chain（用于解密历史消息）
      for (const entry of data.peerChains ?? []) {
        this.peerSenderChains.set(`${entry.userId}:${entry.chain.epoch}`, entry.chain)
      }

      // 恢复自己的链后立即轮换到新 epoch：对端可能已丢失旧链，
      // 新 epoch 的首条消息（messageNumber=0）会触发 Sender Key 重新分发
      await this.rotateSenderChain()

      const fingerprint = await getPublicKeyFingerprint(this.identityKeyPair.publicKey)
      this.onFingerprintReady?.(fingerprint)
      void this.refreshFingerprintCache()
      return true
    } catch {
      return false
    }
  }

  /**
   * 保存当前密钥状态到本设备加密存储（PBKDF2 + 设备随机密钥）。
   * 刷新页面后可恢复身份密钥、Sender Chain 与成员公钥，继续解密历史消息。
   * 设计权衡：能读取 localStorage 的攻击者（XSS）本身就能窃取解密后的明文，
   * 此处加密主要防止密文被直接拷贝复用。
   */
  async saveToStorage(_userId: number): Promise<void> {
    if (!this.identityKeyPair) return
    try {
      const privJwk = await crypto.subtle.exportKey('jwk', this.identityKeyPair.privateKey)
      const data = {
        identityPublicKey: this.identityKeyPair.publicKey,
        identityPrivateKey: JSON.stringify(privJwk),
        senderChain: this.mySenderChain,
        epoch: this.currentEpoch,
        peerChains: Array.from(this.peerSenderChains.entries()).map(([key, chain]) => ({
          userId: Number(key.split(':')[0]),
          chain,
        })),
        memberPublicKeys: Array.from(this.memberPublicKeys.entries()),
      }
      const secret = await this.getOrCreateDeviceSecret()
      const salt = getOrCreateSalt()
      const encryptionKey = await pbkdf2Derive(secret, salt)
      const encryptionKeyRaw = await crypto.subtle.exportKey('raw', encryptionKey)
      const { ciphertext, iv } = await aesEncrypt(JSON.stringify(data), arrayBufferToBase64(encryptionKeyRaw))
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ ciphertext, iv, version: 2 }))
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

  async getMyFingerprint(): Promise<string> {
    if (!this.identityKeyPair) throw new Error('E2EE not initialized')
    return getPublicKeyFingerprint(this.identityKeyPair.publicKey)
  }

  setCallbacks(
    onNeedSendKey: (targetUserId: number, encryptedChainKey: string) => void,
    onStateChange: (ready: boolean) => void,
    onFingerprintReady?: (fingerprint: string) => void
  ): void {
    this.onNeedSendKey = onNeedSendKey
    this.onStateChange = onStateChange
    this.onFingerprintReady = onFingerprintReady
  }

  // ============= 成员管理 =============

  async addMember(userId: number, peerPublicKey: string): Promise<void> {
    if (!this.identityKeyPair) throw new Error('E2EE not initialized')

    const sharedKey = await deriveSharedKey(this.identityKeyPair.privateKey, peerPublicKey)
    this.pairwiseSharedKeys.set(userId, sharedKey)
    this.memberPublicKeys.set(userId, peerPublicKey)
    this.currentRoomMembers.add(userId)

    console.log(`[E2EE] Established shared key with user ${userId}`)
    void this.refreshFingerprintCache()
    this.scheduleSave()
    this.checkReadyState()
  }

  removeMember(userId: number): void {
    this.pairwiseSharedKeys.delete(userId)
    this.memberPublicKeys.delete(userId)
    for (const key of this.peerSenderChains.keys()) {
      if (key.startsWith(`${userId}:`)) {
        this.peerSenderChains.delete(key)
      }
    }
    this.currentRoomMembers.delete(userId)
    this.scheduleSave()
    this.checkReadyState()
  }

  private checkReadyState(): void {
    const isReady = this.pairwiseSharedKeys.size > 0
    this.onStateChange?.(isReady)
  }

  isReady(): boolean {
    return this.pairwiseSharedKeys.size > 0
  }

  // 计算成员公钥指纹（SHA-256 前 8 位，格式 XXXX-XXXX）。
  // 未与该成员建立共享密钥时返回 null（UI 据此提示等待协商）。
  async getMemberFingerprintAsync(userId: number): Promise<string | null> {
    const publicKey = this.memberPublicKeys.get(userId)
    if (!publicKey) return null
    return getPublicKeyFingerprint(publicKey)
  }

  getMemberFingerprint(userId: number): string | null {
    const publicKey = this.memberPublicKeys.get(userId)
    if (!publicKey) return null
    // 同步版本：指纹计算是异步的，这里返回缓存的指纹
    return this.fingerprintCache.get(userId) ?? null
  }

  private fingerprintCache: Map<number, string> = new Map()

  private async refreshFingerprintCache(): Promise<void> {
    for (const [userId, publicKey] of this.memberPublicKeys.entries()) {
      const fingerprint = await getPublicKeyFingerprint(publicKey)
      this.fingerprintCache.set(userId, fingerprint)
    }
  }

  hasSharedKey(userId: number): boolean {
    return this.pairwiseSharedKeys.has(userId)
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

      const distributionPackage = JSON.stringify({
        chainKey: ciphertext,
        chainKeyIv: iv,
        publicKey: this.identityKeyPair!.publicKey,
        epoch: this.mySenderChain.epoch,
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

    const { chainKey: encryptedChainKey, chainKeyIv, publicKey, epoch = 0 } = JSON.parse(encryptedPackage)
    const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
    const chainKey = await aesDecrypt(
      encryptedChainKey,
      chainKeyIv,
      arrayBufferToBase64(sharedKeyRaw)
    )

    this.peerSenderChains.set(this.senderChainKey(senderId, epoch), {
      chainKey,
      messageNumber: 0,
      publicKey,
      epoch,
    })

    // 尝试解密之前缓存的消息
    this.tryProcessPendingMessages(senderId, epoch)

    console.log(`[E2EE] Installed sender key from user ${senderId}`)
    this.scheduleSave()
  }

  // ============= 消息序号跳跃/丢包处理 =============

  /**
   * 棘轮到目标序号（处理丢包/乱序）
   */
  private async ratchetToTarget(senderId: number, epoch: number, targetNumber: number): Promise<void> {
    const chain = this.peerSenderChains.get(this.senderChainKey(senderId, epoch))
    if (!chain) return

    while (chain.messageNumber < targetNumber) {
      const { nextChainKey } = await ratchetStep(chain.chainKey)
      chain.chainKey = nextChainKey
      chain.messageNumber++
    }
  }

  /**
   * 添加消息到待处理缓存
   */
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

    // 缓存最多保留 50 条，超时自动清理
    if (this.pendingMessages.get(key)!.length > 50) {
      const oldest = this.pendingMessages.get(key)!.shift()
      oldest?.reject(new Error('Message expired in cache'))
    }
  }

  /**
   * 尝试处理缓存中的消息
   */
  private async tryProcessPendingMessages(senderId: number, epoch: number): Promise<void> {
    const key = this.senderChainKey(senderId, epoch)
    const pending = this.pendingMessages.get(key)
    if (!pending || pending.length === 0) return

    const chain = this.peerSenderChains.get(key)
    if (!chain) return

    // 按序号排序
    pending.sort((a, b) => a.msg.messageNumber - b.msg.messageNumber)

    // 尝试处理所有可以解密的消息
    const toProcess = [...pending]
    for (const { msg, resolve } of toProcess) {
      try {
        await this.ratchetToTarget(senderId, msg.epoch ?? 0, msg.messageNumber)
        const { messageKey } = await ratchetStep(chain.chainKey)
        const plaintext = await aesDecrypt(msg.ciphertext, msg.iv, messageKey)
        chain.chainKey = (await ratchetStep(chain.chainKey)).nextChainKey
        chain.messageNumber = msg.messageNumber + 1
        resolve(plaintext)

        // 从缓存移除
        const index = this.pendingMessages.get(key)!.findIndex(p => p.msg === msg)
        if (index > -1) {
          this.pendingMessages.get(key)!.splice(index, 1)
        }
      } catch {
        // 还不能解密，继续留在缓存
        continue
      }
    }
  }

  // ============= 加密/解密 =============

  async encryptMessage(plaintext: string, myUserId: number): Promise<EncryptedMessage> {
    if (!this.mySenderChain) throw new Error('Sender chain not initialized')

    const { nextChainKey, messageKey } = await ratchetStep(this.mySenderChain.chainKey)
    const currentMessageNumber = this.mySenderChain.messageNumber

    this.mySenderChain.chainKey = nextChainKey
    this.mySenderChain.messageNumber++

    const { ciphertext, iv } = await aesEncrypt(plaintext, messageKey)

    const message: EncryptedMessage = {
      version: 1,
      senderId: myUserId,
      epoch: this.mySenderChain.epoch,
      messageNumber: currentMessageNumber,
      ciphertext,
      iv,
    }

    // 如果是首次发送，分发 Sender Key
    if (currentMessageNumber === 0) {
      const encryptedKeys = await this.distributeSenderKey()
      for (const [userId, encryptedChainKey] of encryptedKeys.entries()) {
        this.onNeedSendKey?.(userId, encryptedChainKey)
      }
    }

    return message
  }

  async decryptMessage(message: EncryptedMessage): Promise<string> {
    const epoch = message.epoch ?? 0

    // 重放检查：拒绝重复 messageNumber
    if (!this.checkAndUpdateReplay(message.senderId, epoch, message.messageNumber)) {
      console.warn('[E2EE] Replay detected, dropping message', {
        senderId: message.senderId,
        epoch,
        messageNumber: message.messageNumber,
      })
      throw new Error('Replay detected')
    }

    let senderChain = this.peerSenderChains.get(this.senderChainKey(message.senderId, epoch))

    // 如果没有 Sender Chain，先缓存这条消息
    if (!senderChain) {
      return new Promise<string>((resolve, reject) => {
        this.addPendingMessage(message.senderId, message, resolve, reject)
        console.log(`[E2EE] Message #${message.messageNumber} from user ${message.senderId} cached, waiting for sender key`)
      })
    }

    await this.ratchetToTarget(message.senderId, epoch, message.messageNumber)
    const { nextChainKey, messageKey } = await ratchetStep(senderChain.chainKey)
    const plaintext = await aesDecrypt(message.ciphertext, message.iv, messageKey)

    senderChain.chainKey = nextChainKey
    senderChain.messageNumber = message.messageNumber + 1

    return plaintext
  }

  // ============= 清理 =============

  clear(): void {
    this.identityKeyPair = null
    this.pairwiseSharedKeys.clear()
    this.mySenderChain = null
    this.peerSenderChains.clear()
    this.currentRoomMembers.clear()
    this.currentEpoch = 0
    this.pendingMessages.clear()
    this.replayWindows.clear()
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
