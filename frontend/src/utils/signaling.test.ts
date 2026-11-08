// SignalingClient 的单元测试：假 WebSocket 驱动，E2EE 管理器用桩替代（jsdom 没有 WebCrypto）。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  api: {
    post: vi.fn(async () => ({ data: {} })),
    get: vi.fn(async () => ({ data: { exists: false } })),
    delete: vi.fn(async () => ({ data: {} })),
    defaults: { baseURL: '' },
  },
}))

vi.mock('./encryption', () => ({
  groupE2EEManager: {
    init: vi.fn(async () => undefined),
    getIdentityPublicKey: () => 'ECDH-PUB',
    getSigningPublicKey: () => 'ECDSA-PUB',
    getPublicKeyBundle: () => JSON.stringify({ v: 2, ecdh: 'ECDH-PUB', ecdsa: 'ECDSA-PUB' }),
    addMember: vi.fn(async () => undefined),
    removeMember: vi.fn(),
    isReady: () => true,
    rotateSenderChain: vi.fn(async () => 1),
    distributeCurrentSenderKey: vi.fn(async () => new Map<number, string>()),
  },
  decodePublicKeyBundle: (value: string) => {
    try {
      const parsed = JSON.parse(value)
      return { ecdh: parsed.ecdh, ecdsa: parsed.ecdsa }
    } catch {
      return { ecdh: value }
    }
  },
}))

import { api } from './api'
import { groupE2EEManager } from './encryption'
import { SignalingClient } from './signaling'

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3
  readyState = FakeWebSocket.CONNECTING
  sent: Array<Record<string, unknown>> = []
  onopen: ((ev: unknown) => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onerror: ((ev: unknown) => void) | null = null
  onclose: ((ev: { code: number; reason: string }) => void) | null = null

  constructor(public url: string) {
    FakeWebSocket.instances.push(this)
  }
  send(data: string) {
    this.sent.push(JSON.parse(data))
  }
  close() {
    this.readyState = FakeWebSocket.CLOSED
  }
  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.({})
  }
  receive(message: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(message) })
  }
  closeFromServer(code: number) {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code, reason: '' })
  }
  messagesOfType(type: string) {
    return this.sent.filter(m => m.type === type)
  }
}

const latestWs = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
const flush = async (rounds = 10) => {
  for (let i = 0; i < rounds; i++) {
    await Promise.resolve()
  }
}

beforeEach(() => {
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
  localStorage.setItem('token', 'jwt/with+special=chars')
  vi.spyOn(console, 'log').mockImplementation(() => undefined)
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  vi.mocked(groupE2EEManager.addMember).mockClear()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  localStorage.clear()
})

// connect() 在 onopen 时才 resolve：先拿到 promise，再触发 open
async function connectClient() {
  const client = new SignalingClient()
  const connecting = client.connect()
  await flush()
  latestWs().open()
  await connecting
  return client
}

describe('SignalingClient 连接与入会', () => {
  it('URL 中的 token 经过编码，joinRoom 发送 join_room 与带签名公钥的 public_key', async () => {
    const client = await connectClient()
    expect(latestWs().url).toContain(`token=${encodeURIComponent('jwt/with+special=chars')}`)

    await client.joinRoom(9, 1, [{ user_id: 2 } as never, { user_id: 1 } as never])
    expect(latestWs().messagesOfType('join_room')[0]).toMatchObject({ room_id: 9, user_id: 1 })
    const pk = latestWs().messagesOfType('public_key')[0] as unknown as { data: { public_key: string; signing_key: string } }
    expect(pk.data.public_key).toBe('ECDH-PUB')
    expect(pk.data.signing_key).toBe('ECDSA-PUB')
    // 公钥包以 bundle 形式上传到服务端
    expect(api.post).toHaveBeenCalledWith('/api/v1/e2ee/public-key', {
      public_key: JSON.stringify({ v: 2, ecdh: 'ECDH-PUB', ecdsa: 'ECDSA-PUB' }),
      key_type: 'bundle_v2',
    })
    // 会为其他成员拉取公钥（排除自己）
    expect(api.get).toHaveBeenCalledWith('/api/v1/e2ee/public-key/2')
    expect(api.get).not.toHaveBeenCalledWith('/api/v1/e2ee/public-key/1')
  })

  it('收到 public_key 消息时把 ECDH 与签名公钥一并登记', async () => {
    const client = await connectClient()
    await client.joinRoom(9, 1, [])
    latestWs().receive({ type: 'public_key', room_id: 9, user_id: 5, data: { public_key: 'P5', signing_key: 'S5' } })
    await flush()
    expect(groupE2EEManager.addMember).toHaveBeenCalledWith(5, 'P5', 'S5')
  })

  it('on/off 注册与注销消息处理器', async () => {
    const client = await connectClient()
    const handler = vi.fn()
    client.on('chat_message', handler)
    latestWs().receive({ type: 'chat_message', room_id: 9, user_id: 2, data: { id: 1 } })
    await flush()
    expect(handler).toHaveBeenCalledTimes(1)

    client.off('chat_message', handler)
    latestWs().receive({ type: 'chat_message', room_id: 9, user_id: 2, data: { id: 2 } })
    await flush()
    expect(handler).toHaveBeenCalledTimes(1)
  })
})

describe('SignalingClient 重连', () => {
  it('异常断开后自动重连，并重新 join_room、重新广播公钥', async () => {
    vi.useFakeTimers()
    const client = await connectClient()
    await client.joinRoom(9, 1, [])
    const first = latestWs()

    first.closeFromServer(1006)
    await vi.advanceTimersByTimeAsync(3000)
    expect(FakeWebSocket.instances).toHaveLength(2)

    latestWs().open()
    await flush()
    expect(latestWs().messagesOfType('join_room')[0]).toMatchObject({ room_id: 9, user_id: 1 })
    expect(latestWs().messagesOfType('public_key')).toHaveLength(1)
  })

  it('主动 disconnect 后不再重连，也不会重新入会', async () => {
    vi.useFakeTimers()
    const client = await connectClient()
    await client.joinRoom(9, 1, [])
    client.disconnect()
    await vi.advanceTimersByTimeAsync(10000)
    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(api.delete).toHaveBeenCalledWith('/api/v1/e2ee/public-key')
  })

  it('收到 server_shutdown 后停止自动重连', async () => {
    vi.useFakeTimers()
    const client = await connectClient()
    await client.joinRoom(9, 1, [])
    latestWs().receive({ type: 'server_shutdown', room_id: 0, user_id: 0, data: { reason: 'maintenance' } })
    await flush()
    latestWs().closeFromServer(1006)
    await vi.advanceTimersByTimeAsync(10000)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })
})
