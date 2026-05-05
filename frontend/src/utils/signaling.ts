import { SignalingMessage } from '@/types'

export class SignalingClient {
  private ws: WebSocket | null = null
  private handlers: Map<string, ((data: any) => void)[]> = new Map()
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private reconnectDelay = 3000

  // 动态构建 WebSocket URL
  private getWsUrl(): string {
    const token = localStorage.getItem('token')
    const apiHost = import.meta.env.VITE_API_HOST || 'http://localhost:8080'
    const wsProtocol = apiHost.startsWith('https') ? 'wss:' : 'ws:'
    const wsHost = apiHost.replace(/^https?:\/\//, '')
    return `${wsProtocol}//${wsHost}/ws?token=${token}`
  }

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const token = localStorage.getItem('token')
      if (!token || token === 'null') {
        reject(new Error('No authentication token found'))
        return
      }

      // 超过最大重连次数，不再重试
      if (this.reconnectAttempts >= this.maxReconnectAttempts) {
        reject(new Error('Max reconnect attempts reached'))
        return
      }

      const wsUrl = this.getWsUrl()
      console.log('[Signaling] Connecting to:', wsUrl.replace(/token=([^&]+)/, 'token=***'))
      this.ws = new WebSocket(wsUrl)

      this.ws.onopen = () => {
        console.log('[Signaling] WebSocket connected')
        this.reconnectAttempts = 0
        resolve()
      }

      this.ws.onerror = (error) => {
        console.error('[Signaling] WebSocket error:', error)
        reject(error)
      }

      this.ws.onmessage = (event) => {
        this.handleMessage(event)
      }

      this.ws.onclose = (event) => {
        console.log('[Signaling] WebSocket closed, code:', event.code)
        // 只有正常断开过的连接才尝试重连
        if (event.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
          this.reconnectAttempts++
          console.log(`[Signaling] Reconnecting... (${this.reconnectAttempts}/${this.maxReconnectAttempts})`)
          setTimeout(() => {
            this.connect().catch(() => {})
          }, this.reconnectDelay)
        }
      }
    })
  }

  disconnect() {
    if (this.ws) {
      this.ws.close(1000, 'User disconnected')
      this.ws = null
    }
  }

  send(message: SignalingMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    } else {
      console.warn('[Signaling] WebSocket not connected, message not sent')
    }
  }

  on(type: string, handler: (data: any) => void) {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, [])
    }
    this.handlers.get(type)!.push(handler)
  }

  off(type: string, handler: (data: any) => void) {
    if (this.handlers.has(type)) {
      const handlers = this.handlers.get(type)!
      const index = handlers.indexOf(handler)
      if (index !== -1) {
        handlers.splice(index, 1)
      }
    }
  }

  private handleMessage(event: MessageEvent) {
    try {
      const message = JSON.parse(event.data) as SignalingMessage
      const handlers = this.handlers.get(message.type)
      if (handlers) {
        handlers.forEach(handler => handler(message))
      }
    } catch (error) {
      console.error('[Signaling] Failed to parse message:', error)
    }
  }
}

export default SignalingClient
