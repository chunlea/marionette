// WebSocket client for real-time log streaming and event handling

export type WebSocketStatus = 'connecting' | 'connected' | 'disconnected' | 'error'

export interface WebSocketClientOptions {
  url: string
  onMessage?: (data: unknown) => void
  onStatusChange?: (status: WebSocketStatus) => void
  reconnectDelay?: number
  maxReconnectAttempts?: number
}

export class WebSocketClient {
  private ws: WebSocket | null = null
  private url: string
  private reconnectDelay: number
  private maxReconnectAttempts: number
  private reconnectAttempts: number = 0
  private shouldReconnect: boolean = true
  private onMessage?: (data: unknown) => void
  private onStatusChange?: (status: WebSocketStatus) => void

  constructor(options: WebSocketClientOptions) {
    this.url = options.url
    this.onMessage = options.onMessage
    this.onStatusChange = options.onStatusChange
    this.reconnectDelay = options.reconnectDelay ?? 1000
    this.maxReconnectAttempts = options.maxReconnectAttempts ?? 10
  }

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      return
    }

    this.onStatusChange?.('connecting')

    try {
      this.ws = new WebSocket(this.url)

      this.ws.onopen = () => {
        this.reconnectAttempts = 0
        this.onStatusChange?.('connected')
      }

      this.ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          this.onMessage?.(data)
        } catch {
          // If not JSON, pass raw data
          this.onMessage?.(event.data)
        }
      }

      this.ws.onclose = () => {
        this.onStatusChange?.('disconnected')
        this.attemptReconnect()
      }

      this.ws.onerror = () => {
        this.onStatusChange?.('error')
      }
    } catch (error) {
      console.error('WebSocket connection error:', error)
      this.onStatusChange?.('error')
      this.attemptReconnect()
    }
  }

  private attemptReconnect(): void {
    if (!this.shouldReconnect) {
      return
    }

    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error('Max reconnect attempts reached')
      return
    }

    this.reconnectAttempts++
    const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1)

    setTimeout(() => {
      if (this.shouldReconnect) {
        this.connect()
      }
    }, Math.min(delay, 30000))
  }

  send(data: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(typeof data === 'string' ? data : JSON.stringify(data))
    }
  }

  disconnect(): void {
    this.shouldReconnect = false
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
  }

  get status(): WebSocketStatus {
    if (!this.ws) return 'disconnected'
    switch (this.ws.readyState) {
      case WebSocket.CONNECTING:
        return 'connecting'
      case WebSocket.OPEN:
        return 'connected'
      default:
        return 'disconnected'
    }
  }
}

// Helper to build WebSocket URLs
export function buildWebSocketUrl(path: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = import.meta.env.VITE_WS_HOST || window.location.host
  return `${protocol}//${host}${path}`
}
