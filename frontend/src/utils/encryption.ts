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
}

export interface EncryptedMessage {
  version: number
  senderId: number
  messageNumber: number
  ciphertext: string
  iv: string
  senderKeyDistribution?: {
    encryptedChainKey: string
    signature: string
  }
}

// ==================== 基础工具 ====================

const arrayBufferToBase64 = (buffer: ArrayBuffer): string => {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary)
}

const base64ToArrayBuffer = (base64: string): ArrayBuffer => {
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

// ==================== 群聊 E2EE 管理器 (Sender Keys / MLS) ====================

export class GroupE2EEManager {
  private identityKeyPair: KeyPair | null = null
  private pairwiseSharedKeys: Map<number, CryptoKey> = new Map()
  private mySenderChain: SenderChain | null = null
  private peerSenderChains: Map<number, SenderChain> = new Map()
  private currentRoomMembers: Set<number> = new Set()

  // 消息缓存：用于处理序号跳跃/丢包
  private pendingMessages: Map<number, Array<{ msg: EncryptedMessage; resolve: (s: string) => void; reject: (e: Error) => void }>> = new Map()

  private onNeedSendKey?: (targetUserId: number, encryptedChainKey: string) => void
  private onStateChange?: (ready: boolean) => void
  private onFingerprintReady?: (fingerprint: string) => void

  // ============= 初始化 =============

  async init(userId?: number): Promise<void> {
    // 如果提供了 userId，尝试从存储恢复
    if (userId !== undefined) {
      const restored = await this.tryRestoreFromStorage(userId)
      if (restored) {
        console.log('[E2EE] Keys restored from storage')
        this.onStateChange?.(true)
        return
      }
    }

    // 否则生成新密钥
    this.identityKeyPair = await generateKeyPair()
    console.log('[E2EE] New identity key pair generated')

    const initialChainKey = arrayBufferToBase64(toArrayBuffer(crypto.getRandomValues(new Uint8Array(32))))
    this.mySenderChain = {
      chainKey: initialChainKey,
      messageNumber: 0,
      publicKey: this.identityKeyPair.publicKey,
    }

    this.onStateChange?.(true)

    // 通知指纹已就绪
    const fingerprint = await getPublicKeyFingerprint(this.identityKeyPair.publicKey)
    this.onFingerprintReady?.(fingerprint)
  }

  /**
   * 尝试从存储恢复密钥（简化实现：用固定密码，实际应该用用户登录密码）
   */
  private async tryRestoreFromStorage(userId: number): Promise<boolean> {
    // 简化：用 userId 作为临时密码，实际应该用用户的登录密码
    const stored = await loadKeyStore(`user_${userId}_key`)
    if (!stored) return false

    try {
      // 恢复私钥
      const privateKeyRaw = base64ToArrayBuffer(stored.myIdentityPrivateKey)
      const privateKey = await crypto.subtle.importKey(
        'pkcs8',
        privateKeyRaw,
        { name: 'ECDH', namedCurve: 'P-256' },
        true,
        ['deriveKey']
      )

      const publicKeyRaw = await crypto.subtle.exportKey('raw', privateKey)
      this.identityKeyPair = {
        publicKey: arrayBufferToBase64(publicKeyRaw),
        privateKey,
      }

      // 恢复 Sender Chain
      this.mySenderChain = stored.mySenderChain

      // 恢复其他用户的 Sender Chain
      stored.peerSenderChains.forEach(({ userId, chain }) => {
        this.peerSenderChains.set(userId, chain)
      })

      return true
    } catch {
      return false
    }
  }

  /**
   * 保存当前密钥状态到存储
   */
  async saveToStorage(userId: number): Promise<void> {
    if (!this.identityKeyPair || !this.mySenderChain) return

    const privateKeyRaw = await crypto.subtle.exportKey('pkcs8', this.identityKeyPair.privateKey)

    const peerChains: Array<{ userId: number; chain: SenderChain }> = []
    this.peerSenderChains.forEach((chain, userId) => {
      peerChains.push({ userId, chain })
    })

    await saveKeyStore(`user_${userId}_key`, {
      myIdentityPrivateKey: arrayBufferToBase64(privateKeyRaw),
      mySenderChain: this.mySenderChain,
      peerSenderChains: peerChains,
    })

    console.log('[E2EE] Keys saved to storage')
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
    if (this.pairwiseSharedKeys.has(userId)) return

    const sharedKey = await deriveSharedKey(this.identityKeyPair.privateKey, peerPublicKey)
    this.pairwiseSharedKeys.set(userId, sharedKey)
    this.currentRoomMembers.add(userId)

    console.log(`[E2EE] Established shared key with user ${userId}`)
    this.checkReadyState()
  }

  removeMember(userId: number): void {
    this.pairwiseSharedKeys.delete(userId)
    this.peerSenderChains.delete(userId)
    this.currentRoomMembers.delete(userId)
    this.checkReadyState()
  }

  private checkReadyState(): void {
    const isReady = this.pairwiseSharedKeys.size > 0
    this.onStateChange?.(isReady)
  }

  isReady(): boolean {
    return this.pairwiseSharedKeys.size > 0
  }

  getMemberFingerprint(userId: number): string | null {
    const chain = this.peerSenderChains.get(userId)
    if (!chain) return null
    return 'pending' // 需要对方公钥才能计算
  }

  // ============= Sender Key 分发 =============

  private async distributeSenderKey(): Promise<Map<number, string>> {
    if (!this.mySenderChain) throw new Error('Sender chain not initialized')

    const encryptedKeys = new Map<number, string>()

    for (const [userId, sharedKey] of this.pairwiseSharedKeys.entries()) {
      const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
      const { ciphertext, iv } = await aesEncrypt(
        this.mySenderChain.chainKey,
        arrayBufferToBase64(sharedKeyRaw)
      )

      const distributionPackage = JSON.stringify({
        chainKey: ciphertext,
        chainKeyIv: iv,
        publicKey: this.identityKeyPair!.publicKey,
      })

      encryptedKeys.set(userId, distributionPackage)
    }

    return encryptedKeys
  }

  async receiveSenderKey(senderId: number, encryptedPackage: string): Promise<void> {
    const sharedKey = this.pairwiseSharedKeys.get(senderId)
    if (!sharedKey) throw new Error(`No shared key with user ${senderId}`)

    const { chainKey: encryptedChainKey, chainKeyIv, publicKey } = JSON.parse(encryptedPackage)
    const sharedKeyRaw = await crypto.subtle.exportKey('raw', sharedKey)
    const chainKey = await aesDecrypt(
      encryptedChainKey,
      chainKeyIv,
      arrayBufferToBase64(sharedKeyRaw)
    )

    this.peerSenderChains.set(senderId, {
      chainKey,
      messageNumber: 0,
      publicKey,
    })

    // 尝试解密之前缓存的消息
    this.tryProcessPendingMessages(senderId)

    console.log(`[E2EE] Installed sender key from user ${senderId}`)
  }

  // ============= 消息序号跳跃/丢包处理 =============

  /**
   * 棘轮到目标序号（处理丢包/乱序）
   */
  private async ratchetToTarget(senderId: number, targetNumber: number): Promise<void> {
    const chain = this.peerSenderChains.get(senderId)
    if (!chain) return

    while (chain.messageNumber <= targetNumber) {
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
    if (!this.pendingMessages.has(senderId)) {
      this.pendingMessages.set(senderId, [])
    }
    this.pendingMessages.get(senderId)!.push({ msg, resolve, reject })

    // 缓存最多保留 50 条，超时自动清理
    if (this.pendingMessages.get(senderId)!.length > 50) {
      const oldest = this.pendingMessages.get(senderId)!.shift()
      oldest?.reject(new Error('Message expired in cache'))
    }
  }

  /**
   * 尝试处理缓存中的消息
   */
  private async tryProcessPendingMessages(senderId: number): Promise<void> {
    const pending = this.pendingMessages.get(senderId)
    if (!pending || pending.length === 0) return

    const chain = this.peerSenderChains.get(senderId)
    if (!chain) return

    // 按序号排序
    pending.sort((a, b) => a.msg.messageNumber - b.msg.messageNumber)

    // 尝试处理所有可以解密的消息
    const toProcess = [...pending]
    for (const { msg, resolve } of toProcess) {
      try {
        await this.ratchetToTarget(senderId, msg.messageNumber)
        const { messageKey } = await ratchetStep(chain.chainKey)
        const plaintext = await aesDecrypt(msg.ciphertext, msg.iv, messageKey)
        chain.chainKey = (await ratchetStep(chain.chainKey)).nextChainKey
        chain.messageNumber = msg.messageNumber + 1
        resolve(plaintext)

        // 从缓存移除
        const index = this.pendingMessages.get(senderId)!.findIndex(p => p.msg === msg)
        if (index > -1) {
          this.pendingMessages.get(senderId)!.splice(index, 1)
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
    let senderChain = this.peerSenderChains.get(message.senderId)

    // 如果没有 Sender Chain，先缓存这条消息
    if (!senderChain) {
      return new Promise<string>((resolve, reject) => {
        this.addPendingMessage(message.senderId, message, resolve, reject)
        console.log(`[E2EE] Message #${message.messageNumber} from user ${message.senderId} cached, waiting for sender key`)
      })
    }

    await this.ratchetToTarget(message.senderId, message.messageNumber)
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
    this.pendingMessages.clear()
  }
}

// 全局单例
export const groupE2EEManager = new GroupE2EEManager()
