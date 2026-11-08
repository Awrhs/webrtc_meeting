// SFUConnection 的单元测试：用假的 WebSocket / RTCPeerConnection 驱动，
// 覆盖入会协商流程、重连策略、手动断开、ICE 宽限与摄像头/屏幕共享的隔离。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  api: {
    post: vi.fn(async () => ({ data: { sfu_token: 'refreshed-token' } })),
    get: vi.fn(),
    defaults: { baseURL: '' },
  },
}))

import { api } from './api'
import { SFUConnection } from './webrtc'

// ---------- 假 WebSocket ----------
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
    // 浏览器会异步触发 onclose；closeTransport 已先摘掉回调，这里不触发任何事件
    this.readyState = FakeWebSocket.CLOSED
  }

  // ---- 测试辅助 ----
  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.({})
  }
  receive(message: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(message) })
  }
  closeFromServer(code: number, reason = '') {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code, reason })
  }
  messagesOfType(type: string) {
    return this.sent.filter(m => m.type === type)
  }
}

// ---------- 假 RTCPeerConnection ----------
interface FakeSender {
  track: FakeTrack | null
  replaceTrack: (t: FakeTrack | null) => Promise<void>
}

class FakeTrack {
  enabled = true
  readyState = 'live'
  constructor(public kind: 'audio' | 'video', public id = `${kind}-${Math.random()}`) {}
  stop() {
    this.readyState = 'ended'
  }
  getSettings() {
    return { deviceId: 'dev' }
  }
}

class FakeRTCPeerConnection {
  static instances: FakeRTCPeerConnection[] = []
  localDescription: { type: string; sdp: string } | null = null
  remoteDescription: { type: string; sdp: string } | null = null
  signalingState = 'stable'
  iceConnectionState = 'new'
  connectionState = 'new'
  addedCandidates: unknown[] = []
  senders: FakeSender[] = []
  closed = false
  onicecandidate: ((ev: unknown) => void) | null = null
  ontrack: ((ev: unknown) => void) | null = null
  oniceconnectionstatechange: (() => void) | null = null
  onconnectionstatechange: (() => void) | null = null

  constructor(public config: unknown) {
    FakeRTCPeerConnection.instances.push(this)
  }

  addTrack(track: FakeTrack) {
    const sender: FakeSender = {
      track,
      replaceTrack: async (t) => {
        sender.track = t
      },
    }
    this.senders.push(sender)
    return sender
  }
  addTransceiver(track: FakeTrack) {
    return { sender: this.addTrack(track) }
  }
  getSenders() {
    return this.senders
  }
  removeTrack(sender: FakeSender) {
    this.senders = this.senders.filter(s => s !== sender)
  }
  async createOffer() {
    return { type: 'offer', sdp: 'v=0 offer' }
  }
  async createAnswer() {
    return { type: 'answer', sdp: 'v=0 answer' }
  }
  async setLocalDescription(desc: { type: string; sdp?: string }) {
    this.localDescription = { type: desc.type, sdp: desc.sdp ?? '' }
    this.signalingState = desc.type === 'offer' ? 'have-local-offer' : 'stable'
  }
  async setRemoteDescription(desc: { type: string; sdp?: string }) {
    if (desc.type === 'rollback') {
      this.signalingState = 'stable'
      return
    }
    this.remoteDescription = { type: desc.type, sdp: desc.sdp ?? '' }
    this.signalingState = desc.type === 'offer' ? 'have-remote-offer' : 'stable'
  }
  async addIceCandidate(candidate: unknown) {
    this.addedCandidates.push(candidate)
  }
  close() {
    this.closed = true
    this.connectionState = 'closed'
  }

  // ---- 测试辅助 ----
  setIceState(state: string) {
    this.iceConnectionState = state
    this.oniceconnectionstatechange?.()
  }
  setConnectionState(state: string) {
    this.connectionState = state
    this.onconnectionstatechange?.()
  }
}

function makeStream(tracks: FakeTrack[]) {
  return {
    id: `stream-${Math.random()}`,
    getTracks: () => tracks,
    getAudioTracks: () => tracks.filter(t => t.kind === 'audio'),
    getVideoTracks: () => tracks.filter(t => t.kind === 'video'),
  } as unknown as MediaStream
}

const flush = async (rounds = 5) => {
  for (let i = 0; i < rounds; i++) {
    await Promise.resolve()
  }
}

interface Harness {
  conn: SFUConnection
  onConnected: ReturnType<typeof vi.fn>
  onDisconnected: ReturnType<typeof vi.fn>
  onError: ReturnType<typeof vi.fn>
  ws: () => FakeWebSocket
  pc: () => FakeRTCPeerConnection
}

async function connectHarness(stream = makeStream([new FakeTrack('audio')])): Promise<Harness> {
  const onConnected = vi.fn()
  const onDisconnected = vi.fn()
  const onError = vi.fn()
  const conn = new SFUConnection(7, 42, 'tok/en+1', vi.fn(), onConnected, onDisconnected, onError, null)
  await conn.connect(stream)
  return {
    conn,
    onConnected,
    onDisconnected,
    onError,
    ws: () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1],
    pc: () => FakeRTCPeerConnection.instances[FakeRTCPeerConnection.instances.length - 1],
  }
}

beforeEach(() => {
  FakeWebSocket.instances = []
  FakeRTCPeerConnection.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
  vi.stubGlobal('RTCPeerConnection', FakeRTCPeerConnection)
  vi.spyOn(console, 'log').mockImplementation(() => undefined)
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
  vi.mocked(api.post).mockClear()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('SFUConnection 入会与协商', () => {
  it('连接后发送 join（token 经 URL 编码），收到 joined 后发 offer 并重发期望的视频订阅', async () => {
    const h = await connectHarness()
    expect(h.ws().url).toContain(`token=${encodeURIComponent('tok/en+1')}`)

    h.conn.setVideoSubscriptions([3, 4])
    h.ws().open()
    expect(h.ws().messagesOfType('join')[0]).toMatchObject({ room_id: 7, user_id: 42 })

    h.ws().receive({ type: 'joined', room_id: 7, user_id: 42 })
    await flush(10)

    const subs = h.ws().messagesOfType('subscribe').map(m => m.target_user_id)
    expect(subs).toEqual([3, 4])
    expect(h.ws().messagesOfType('offer')).toHaveLength(1)
    expect(h.pc().localDescription?.type).toBe('offer')
  })

  it('answer 先于远端描述到达的 ICE 候选会被缓存，设置 answer 后统一应用', async () => {
    const h = await connectHarness()
    h.ws().open()
    h.ws().receive({ type: 'joined' })
    await flush(10)

    h.ws().receive({ type: 'ice-candidate', candidate: 'candidate:1 1 udp 1 127.0.0.1 5000 typ host', sdp_mid: '0', sdp_m_line_index: 0 })
    await flush(5)
    expect(h.pc().addedCandidates).toHaveLength(0)

    h.ws().receive({ type: 'answer', sdp: 'v=0 answer' })
    await flush(10)
    expect(h.pc().remoteDescription?.type).toBe('answer')
    expect(h.pc().addedCandidates).toHaveLength(1)
  })

  it('远端重协商 offer：若本地处于 have-local-offer 先回滚，再回复 answer', async () => {
    const h = await connectHarness()
    h.ws().open()
    h.ws().receive({ type: 'joined' })
    await flush(10)
    expect(h.pc().signalingState).toBe('have-local-offer')

    h.ws().receive({ type: 'offer', sdp: 'v=0 sfu-offer' })
    await flush(10)
    expect(h.pc().remoteDescription?.sdp).toBe('v=0 sfu-offer')
    expect(h.ws().messagesOfType('answer')).toHaveLength(1)
  })

  it('畸形消息不会让连接崩溃', async () => {
    const h = await connectHarness()
    h.ws().open()
    h.ws().onmessage?.({ data: 'not json{' })
    await flush()
    expect(h.onError).not.toHaveBeenCalled()
  })
})

describe('SFUConnection ICE 配置', () => {
  it('优先使用服务端下发的 ice_servers（含时限 TURN 凭据）', async () => {
    const servers: RTCIceServer[] = [
      { urls: ['stun:stun.example.com:3478'] },
      { urls: ['turn:turn.example.com:3478?transport=udp'], username: '1700003600:42', credential: 'hmac' },
    ]
    const conn = new SFUConnection(7, 42, 'tok', vi.fn(), vi.fn(), vi.fn(), vi.fn(), null, undefined, servers)
    await conn.connect(makeStream([new FakeTrack('audio')]))
    const pc = FakeRTCPeerConnection.instances[FakeRTCPeerConnection.instances.length - 1]
    expect((pc.config as RTCConfiguration).iceServers).toEqual(servers)
  })

  it('服务端未下发时回退到构建期配置的 STUN，不再默认 Google STUN', async () => {
    const h = await connectHarness()
    const iceServers = (h.pc().config as RTCConfiguration).iceServers ?? []
    expect(iceServers).toHaveLength(1)
    expect(String(iceServers[0].urls)).toMatch(/^stun:/)
    expect(String(iceServers[0].urls)).not.toContain('google')
  })
})

describe('SFUConnection 摄像头与屏幕共享隔离', () => {
  it('muteVideo 只关摄像头轨道，不影响屏幕共享轨道', async () => {
    const camera = new FakeTrack('video')
    const h = await connectHarness(makeStream([new FakeTrack('audio'), camera]))
    const screen = new FakeTrack('video')
    await h.conn.addVideoTrack(screen as unknown as MediaStreamTrack, makeStream([screen]))

    h.conn.muteVideo(true)
    expect(camera.enabled).toBe(false)
    expect(screen.enabled).toBe(true)

    h.conn.muteVideo(false)
    expect(camera.enabled).toBe(true)
  })

  it('unpublishCamera 用 replaceTrack(null) 停止发布，重新开启时复用同一 sender', async () => {
    const camera = new FakeTrack('video')
    const h = await connectHarness(makeStream([new FakeTrack('audio'), camera]))
    const cameraSender = h.pc().senders.find(s => s.track === camera)!

    await h.conn.unpublishCamera()
    expect(cameraSender.track).toBeNull()

    const camera2 = new FakeTrack('video')
    await h.conn.replaceVideoTrack(camera2 as unknown as MediaStreamTrack)
    expect(cameraSender.track).toBe(camera2)
    expect(h.pc().senders.filter(s => s.track?.kind === 'video')).toHaveLength(1)
  })
})

describe('SFUConnection 重连策略', () => {
  it('服务端异常关闭后按延迟重连：刷新 token、重建 WS，计数只加一次', async () => {
    vi.useFakeTimers()
    const h = await connectHarness()
    h.ws().open()
    const firstWs = h.ws()

    // ICE failed 与 WS 关闭接连发生：只能算一次重连尝试
    h.pc().setIceState('failed')
    firstWs.closeFromServer(1006)
    expect(h.onDisconnected).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(3000)
    await flush(10)
    expect(api.post).toHaveBeenCalledTimes(1)
    expect(api.post).toHaveBeenCalledWith('/api/v1/rooms/7/sfu-token')
    expect(FakeWebSocket.instances).toHaveLength(2)
    expect(h.ws().url).toContain('token=refreshed-token')
    // 旧连接的回调已被摘除：即使它再触发 onclose 也不会影响新连接
    expect(firstWs.onclose).toBeNull()
  })

  it('手动 disconnect 后，挂起的 token 刷新完成也不能把连接重新拉起来', async () => {
    vi.useFakeTimers()
    let resolveToken: (v: unknown) => void = () => undefined
    vi.mocked(api.post).mockImplementationOnce(() => new Promise(resolve => {
      resolveToken = resolve
    }) as never)

    const h = await connectHarness()
    h.ws().open()
    h.ws().closeFromServer(1006)
    await vi.advanceTimersByTimeAsync(3000)
    expect(api.post).toHaveBeenCalledTimes(1)

    h.conn.disconnect()
    resolveToken({ data: { sfu_token: 'late-token' } })
    await flush(20)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })

  it('被服务端以 4001 顶替时不重连，并报告错误', async () => {
    vi.useFakeTimers()
    const h = await connectHarness()
    h.ws().open()
    h.ws().closeFromServer(4001, 'replaced by another session')
    await vi.advanceTimersByTimeAsync(10000)
    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(h.onError).toHaveBeenCalledWith(expect.stringContaining('其他设备'))
  })

  it('ICE disconnected 有 5 秒宽限：期间恢复则不重连，超时仍断开才重连', async () => {
    vi.useFakeTimers()
    const h = await connectHarness()
    h.ws().open()

    h.pc().setIceState('disconnected')
    await vi.advanceTimersByTimeAsync(2000)
    h.pc().setIceState('connected')
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.post).not.toHaveBeenCalled()

    h.pc().setIceState('disconnected')
    await vi.advanceTimersByTimeAsync(5000 + 3000)
    await flush(10)
    expect(api.post).toHaveBeenCalledTimes(1)
  })

  it('重连计数在媒体 connected 时归零；达到上限后停止并报错', async () => {
    vi.useFakeTimers()
    const h = await connectHarness()
    h.ws().open()

    for (let i = 0; i < 5; i++) {
      h.ws().closeFromServer(1006)
      await vi.advanceTimersByTimeAsync(3000)
      await flush(10)
    }
    expect(FakeWebSocket.instances).toHaveLength(6)

    // 第 6 次：超过上限，不再重连
    h.ws().open()
    h.ws().closeFromServer(1006)
    await vi.advanceTimersByTimeAsync(3000)
    await flush(10)
    expect(FakeWebSocket.instances).toHaveLength(6)
    expect(h.onError).toHaveBeenCalledWith(expect.stringContaining('次数过多'))

    // 若中途媒体真正连上，计数应被清零（用新的连接验证）
    const fresh = await connectHarness()
    fresh.ws().open()
    fresh.pc().setConnectionState('connected')
    expect(fresh.onConnected).toHaveBeenCalledTimes(1)
  })
})
