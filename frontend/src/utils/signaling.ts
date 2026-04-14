import { SignalingMessage } from '@/types'

const token = localStorage.getItem('token')
const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
const host = window.location.host
const wsUrl = `${protocol}//${host}/ws?token=${token}`

export class SignalingClient {
  private ws: WebSocket | null = null
  private handlers: Map<string, ((data: any) => void)[]> = new Map()

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(wsUrl)

      this.ws.onopen = () => {
        resolve()
      }

      this.ws.onerror = (error) => {
        reject(error)
      }

      this.ws.onmessage = (event) => {
        this.handleMessage(event)
      }

      this.ws.onclose = () => {
        setTimeout(() => {
          this.connect()
        }, 3000)
      }
    })
  }

  disconnect() {
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
  }

  send(message: SignalingMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
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
      console.error('Failed to parse signaling message', error)
    }
  }
}

export default SignalingClient
