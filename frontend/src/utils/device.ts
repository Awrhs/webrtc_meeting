// 本浏览器的稳定设备标识：首次生成后存 localStorage，随每个 API 请求以 X-Device-Id 发送。
// 服务端据此记录登录设备，并把登录会话绑定到设备：在"登录设备"里删除某台设备，
// 就能真正吊销那台设备上的全部会话。清除站点数据会生成新的设备 ID（等同于一台新设备）。
const STORAGE_KEY = 'device_id'

let cached: string | null = null

export function getDeviceId(): string {
  if (cached) return cached
  try {
    const existing = localStorage.getItem(STORAGE_KEY)
    if (existing) {
      cached = existing
      return existing
    }
  } catch {
    // localStorage 不可用（隐私模式等）：退化为会话内随机 ID
  }
  const fresh = generateId()
  try {
    localStorage.setItem(STORAGE_KEY, fresh)
  } catch {
    // 忽略写入失败
  }
  cached = fresh
  return fresh
}

function generateId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
}
