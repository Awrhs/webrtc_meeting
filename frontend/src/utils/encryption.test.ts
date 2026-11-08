// @vitest-environment node
//
// 用 Node 自带的 WebCrypto 跑真实的加解密流程（jsdom 没有 crypto.subtle）。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  GroupE2EEManager,
  decodePublicKeyBundle,
  encodePublicKeyBundle,
  generateSigningKeyPair,
  getPublicKeyFingerprint,
  messageSigningPayload,
  signPayload,
  type EncryptedMessage,
} from './encryption'

// 不持久化、不恢复的 localStorage 占位：两个管理器共用全局对象，
// 若真的保存/恢复，第二个管理器会读到第一个的密钥。
beforeEach(() => {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: () => null,
      setItem: () => undefined,
      removeItem: () => undefined,
      key: () => null,
      length: 0,
    },
  })
  vi.spyOn(console, 'log').mockImplementation(() => undefined)
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
})

// Alice 与 Bob 完成公钥交换，并把 Alice 的 Sender Key 分发给 Bob
async function pair() {
  const alice = new GroupE2EEManager()
  const bob = new GroupE2EEManager()
  await alice.init(1)
  await bob.init(2)
  await alice.addMember(2, bob.getIdentityPublicKey(), bob.getSigningPublicKey())
  await bob.addMember(1, alice.getIdentityPublicKey(), alice.getSigningPublicKey())
  const packages = await alice.distributeCurrentSenderKey([2])
  await bob.receiveSenderKey(1, packages.get(2)!)
  return { alice, bob }
}

describe('GroupE2EEManager: Sender Keys 加解密', () => {
  it('加密后对方能解出原文，消息带 v2 签名', async () => {
    const { alice, bob } = await pair()
    const message = await alice.encryptMessage('hello bob', 1)
    expect(message.version).toBe(2)
    expect(message.signature).toBeTruthy()
    expect(await bob.decryptMessage(message)).toBe('hello bob')
  })

  it('乱序到达的消息都能解密（跳过的消息密钥被缓存）', async () => {
    const { alice, bob } = await pair()
    const m0 = await alice.encryptMessage('zero', 1)
    const m1 = await alice.encryptMessage('one', 1)
    const m2 = await alice.encryptMessage('two', 1)
    expect(await bob.decryptMessage(m2)).toBe('two')
    expect(await bob.decryptMessage(m0)).toBe('zero')
    expect(await bob.decryptMessage(m1)).toBe('one')
  })

  it('同一条消息第二次投递被当作重放拒绝', async () => {
    const { alice, bob } = await pair()
    const m0 = await alice.encryptMessage('once', 1)
    expect(await bob.decryptMessage(m0)).toBe('once')
    await expect(bob.decryptMessage(m0)).rejects.toThrow(/重复|过期/)
  })

  it('并发解密不会破坏链状态', async () => {
    const { alice, bob } = await pair()
    const messages: EncryptedMessage[] = []
    for (let i = 0; i < 10; i++) {
      messages.push(await alice.encryptMessage(`msg-${i}`, 1))
    }
    // 同时发起全部解密（Chat 组件渲染时就是这样做的）
    const results = await Promise.all(messages.map(m => bob.decryptMessage(m)))
    expect(results).toEqual(messages.map((_, i) => `msg-${i}`))
  })

  it('密文被篡改 → 签名无效，且不影响后续真正消息的解密', async () => {
    const { alice, bob } = await pair()
    const m0 = await alice.encryptMessage('real', 1)
    const forged: EncryptedMessage = { ...m0, ciphertext: m0.ciphertext.replace(/.$/, c => (c === 'A' ? 'B' : 'A')) }
    await expect(bob.decryptMessage(forged)).rejects.toThrow('签名无效')
    // 伪造失败没有推进棘轮：真正的 m0 仍可解密
    expect(await bob.decryptMessage(m0)).toBe('real')
  })

  it('用别人的签名密钥签名的消息被拒绝', async () => {
    const { alice, bob } = await pair()
    const eve = await generateSigningKeyPair()
    const m0 = await alice.encryptMessage('from alice?', 1)
    const resigned: EncryptedMessage = { ...m0, signature: await signPayload(eve.privateKey, messageSigningPayload(m0)) }
    await expect(bob.decryptMessage(resigned)).rejects.toThrow('签名无效')
  })

  it('缺少签名的 v2 消息被拒绝', async () => {
    const { alice, bob } = await pair()
    const m0 = await alice.encryptMessage('unsigned', 1)
    const withoutSignature: EncryptedMessage = { ...m0 }
    delete withoutSignature.signature
    await expect(bob.decryptMessage(withoutSignature)).rejects.toThrow('缺少签名')
  })

  it('序号跳跃超过上限被拒绝（防止伪造序号拖垮 CPU）', async () => {
    const { alice, bob } = await pair()
    ;(bob as unknown as { maxSkip: number }).maxSkip = 5
    const messages: EncryptedMessage[] = []
    for (let i = 0; i < 10; i++) {
      messages.push(await alice.encryptMessage(`m${i}`, 1))
    }
    await expect(bob.decryptMessage(messages[9])).rejects.toThrow('跳跃过大')
    // 上限以内的仍然正常
    expect(await bob.decryptMessage(messages[3])).toBe('m3')
  })

  it('Sender Key 晚于消息到达：消息先挂起，密钥到达后自动解出', async () => {
    const alice = new GroupE2EEManager()
    const carol = new GroupE2EEManager()
    await alice.init(1)
    await carol.init(3)
    await alice.addMember(3, carol.getIdentityPublicKey(), carol.getSigningPublicKey())
    await carol.addMember(1, alice.getIdentityPublicKey(), alice.getSigningPublicKey())

    // 网络乱序：分发包（位置 0）先于消息生成，却晚于消息到达
    const packages = await alice.distributeCurrentSenderKey([3])
    const m0 = await alice.encryptMessage('early', 1)
    let settled = false
    const pending = carol.decryptMessage(m0).then(text => {
      settled = true
      return text
    })
    await new Promise(r => setTimeout(r, 20))
    expect(settled).toBe(false)

    await carol.receiveSenderKey(1, packages.get(3)!)
    expect(await pending).toBe('early')
  })

  it('分发包携带链位置：中途加入的成员只能解之后的消息（回归：先推进再分发的错位 bug）', async () => {
    const alice = new GroupE2EEManager()
    const late = new GroupE2EEManager()
    await alice.init(1)
    await late.init(5)
    await alice.addMember(5, late.getIdentityPublicKey(), late.getSigningPublicKey())
    await late.addMember(1, alice.getIdentityPublicKey(), alice.getSigningPublicKey())

    const m0 = await alice.encryptMessage('before join', 1)
    const m1 = await alice.encryptMessage('also before', 1)
    // 此时分发：链位置应为 2
    const packages = await alice.distributeCurrentSenderKey([5])
    expect(JSON.parse(packages.get(5)!).messageNumber).toBe(2)
    await late.receiveSenderKey(1, packages.get(5)!)

    const m2 = await alice.encryptMessage('after join', 1)
    expect(await late.decryptMessage(m2)).toBe('after join')
    // 加入前的消息没有对应密钥（前向保密），被拒绝而不是解出乱码
    await expect(late.decryptMessage(m0)).rejects.toThrow(/重复|过期/)
    await expect(late.decryptMessage(m1)).rejects.toThrow(/重复|过期/)
  })

  it('重复收到同一 epoch 的旧位置分发包不会把链拉回去', async () => {
    const { alice, bob } = await pair()
    const stale = await alice.distributeCurrentSenderKey([2]) // 位置 0 的包
    const m0 = await alice.encryptMessage('a', 1)
    const m1 = await alice.encryptMessage('b', 1)
    expect(await bob.decryptMessage(m0)).toBe('a')
    expect(await bob.decryptMessage(m1)).toBe('b')
    // Bob 的链已在位置 2；再次收到位置 0 的包必须被忽略
    await bob.receiveSenderKey(1, stale.get(2)!)
    const m2 = await alice.encryptMessage('c', 1)
    expect(await bob.decryptMessage(m2)).toBe('c')
  })

  it('签名公钥晚于消息到达：消息先挂起，公钥到达后自动解出', async () => {
    const alice = new GroupE2EEManager()
    const dave = new GroupE2EEManager()
    await alice.init(1)
    await dave.init(4)
    await alice.addMember(4, dave.getIdentityPublicKey(), dave.getSigningPublicKey())
    // Dave 只拿到 Alice 的 ECDH 公钥（旧客户端式的公钥记录）
    await dave.addMember(1, alice.getIdentityPublicKey())
    const packages = await alice.distributeCurrentSenderKey([4])
    await dave.receiveSenderKey(1, packages.get(4)!)

    const m0 = await alice.encryptMessage('needs signing key', 1)
    const pending = dave.decryptMessage(m0)
    await new Promise(r => setTimeout(r, 20))
    await dave.addMember(1, alice.getIdentityPublicKey(), alice.getSigningPublicKey())
    expect(await pending).toBe('needs signing key')
  })

  it('已知成员的身份公钥变化时触发 onKeyChanged 告警', async () => {
    const alice = new GroupE2EEManager()
    const bob = new GroupE2EEManager()
    const impostor = new GroupE2EEManager()
    await alice.init(1)
    await bob.init(2)
    await impostor.init(99)

    const changes: Array<[number, string, string]> = []
    alice.setCallbacks(() => undefined, () => undefined, undefined, (userId, oldFp, newFp) => {
      changes.push([userId, oldFp, newFp])
    })

    await alice.addMember(2, bob.getIdentityPublicKey(), bob.getSigningPublicKey())
    await alice.addMember(2, bob.getIdentityPublicKey(), bob.getSigningPublicKey()) // 相同公钥：不告警
    expect(changes).toHaveLength(0)

    await alice.addMember(2, impostor.getIdentityPublicKey(), impostor.getSigningPublicKey())
    expect(changes).toHaveLength(1)
    expect(changes[0][0]).toBe(2)
    expect(changes[0][1]).not.toBe(changes[0][2])
  })
})

describe('公钥包与指纹', () => {
  it('bundle 编解码往返，旧格式裸公钥兼容', () => {
    const bundle = encodePublicKeyBundle('ECDH', 'ECDSA')
    expect(decodePublicKeyBundle(bundle)).toEqual({ ecdh: 'ECDH', ecdsa: 'ECDSA' })
    expect(decodePublicKeyBundle('legacy-raw-key')).toEqual({ ecdh: 'legacy-raw-key' })
    expect(decodePublicKeyBundle('{not json')).toEqual({ ecdh: '{not json' })
  })

  it('指纹覆盖签名公钥：替换签名公钥会改变指纹', async () => {
    const mgr = new GroupE2EEManager()
    await mgr.init(1)
    const other = await generateSigningKeyPair()
    const fpWithOwn = await getPublicKeyFingerprint(mgr.getIdentityPublicKey(), mgr.getSigningPublicKey())
    const fpWithOther = await getPublicKeyFingerprint(mgr.getIdentityPublicKey(), other.publicKey)
    const fpWithout = await getPublicKeyFingerprint(mgr.getIdentityPublicKey())
    expect(fpWithOwn).toMatch(/^[0-9A-F]{8}-[0-9A-F]{8}-[0-9A-F]{8}-[0-9A-F]{8}$/)
    expect(fpWithOwn).not.toBe(fpWithOther)
    expect(fpWithOwn).not.toBe(fpWithout)
    expect(await mgr.getMyFingerprint()).toBe(fpWithOwn)
  })
})
