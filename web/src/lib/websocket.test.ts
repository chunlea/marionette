import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { WebSocketClient, buildWebSocketUrl } from './websocket'

// Mock WebSocket with controllable behavior
class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  url: string
  readyState: number = MockWebSocket.CONNECTING
  onopen: ((event: Event) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  sentMessages: string[] = []

  constructor(url: string) {
    this.url = url
    mockWebSocketInstances.push(this)
  }

  send(data: string): void {
    this.sentMessages.push(data)
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }

  // Helper to simulate connection open
  simulateOpen(): void {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.(new Event('open'))
  }

  // Helper to simulate receiving a message
  simulateMessage(data: unknown): void {
    const messageEvent = new MessageEvent('message', {
      data: typeof data === 'string' ? data : JSON.stringify(data),
    })
    this.onmessage?.(messageEvent)
  }

  // Helper to simulate error
  simulateError(): void {
    this.onerror?.(new Event('error'))
  }

  // Helper to simulate close
  simulateClose(): void {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }
}

let mockWebSocketInstances: MockWebSocket[] = []
const originalWebSocket = global.WebSocket

describe('WebSocketClient', () => {
  beforeEach(() => {
    mockWebSocketInstances = []
    // @ts-expect-error - MockWebSocket doesn't implement full WebSocket interface
    global.WebSocket = MockWebSocket
    vi.useFakeTimers()
  })

  afterEach(() => {
    global.WebSocket = originalWebSocket
    vi.useRealTimers()
  })

  describe('constructor', () => {
    it('should store configuration options', () => {
      const onMessage = vi.fn()
      const onStatusChange = vi.fn()

      const client = new WebSocketClient({
        url: 'ws://test.com',
        onMessage,
        onStatusChange,
        reconnectDelay: 2000,
        maxReconnectAttempts: 5,
      })

      expect(client).toBeDefined()
    })

    it('should use default values for optional options', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      expect(client).toBeDefined()
      expect(client.status).toBe('disconnected')
    })
  })

  describe('connect', () => {
    it('should call onStatusChange with connecting', () => {
      const onStatusChange = vi.fn()
      const client = new WebSocketClient({
        url: 'ws://test.com',
        onStatusChange,
      })

      client.connect()

      expect(onStatusChange).toHaveBeenCalledWith('connecting')
    })

    it('should call onStatusChange with connected on open', () => {
      const onStatusChange = vi.fn()
      const client = new WebSocketClient({
        url: 'ws://test.com',
        onStatusChange,
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()

      expect(onStatusChange).toHaveBeenCalledWith('connected')
    })

    it('should not create new connection if already connected', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      client.connect()

      expect(mockWebSocketInstances.length).toBe(1)
    })
  })

  describe('message handling', () => {
    it('should parse JSON messages and call onMessage', () => {
      const onMessage = vi.fn()
      const client = new WebSocketClient({
        url: 'ws://test.com',
        onMessage,
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      mockWebSocketInstances[0].simulateMessage({ type: 'test', value: 123 })

      expect(onMessage).toHaveBeenCalledWith({ type: 'test', value: 123 })
    })

    it('should pass raw data for non-JSON messages', () => {
      const onMessage = vi.fn()
      const client = new WebSocketClient({
        url: 'ws://test.com',
        onMessage,
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()

      // Create a message event with raw string data
      const messageEvent = new MessageEvent('message', { data: 'raw string data' })
      mockWebSocketInstances[0].onmessage?.(messageEvent)

      expect(onMessage).toHaveBeenCalledWith('raw string data')
    })
  })

  describe('send', () => {
    it('should send string data directly', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      client.send('test message')

      expect(mockWebSocketInstances[0].sentMessages).toContain('test message')
    })

    it('should JSON stringify objects', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      client.send({ type: 'test', value: 123 })

      expect(mockWebSocketInstances[0].sentMessages).toContain(
        JSON.stringify({ type: 'test', value: 123 })
      )
    })

    it('should not send when not connected', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      client.send('test message')

      expect(mockWebSocketInstances.length).toBe(0)
    })
  })

  describe('disconnect', () => {
    it('should close WebSocket and prevent reconnection', () => {
      const onStatusChange = vi.fn()
      const client = new WebSocketClient({
        url: 'ws://test.com',
        onStatusChange,
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      client.disconnect()

      expect(client.status).toBe('disconnected')
    })
  })

  describe('reconnection', () => {
    it('should attempt reconnection on close', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
        reconnectDelay: 1000,
        maxReconnectAttempts: 3,
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      mockWebSocketInstances[0].simulateClose()

      vi.advanceTimersByTime(1000)

      expect(mockWebSocketInstances.length).toBe(2)
    })

    it('should stop reconnecting after max attempts', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
        reconnectDelay: 1000,
        maxReconnectAttempts: 2,
      })

      client.connect()

      // Close and attempt reconnect twice
      for (let i = 0; i < 3; i++) {
        mockWebSocketInstances[mockWebSocketInstances.length - 1].simulateClose()
        vi.advanceTimersByTime(30000) // Max delay
      }

      // Should stop at maxReconnectAttempts + 1 (initial connect + 2 reconnects)
      expect(mockWebSocketInstances.length).toBe(3)
    })

    it('should not reconnect after disconnect is called', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
        reconnectDelay: 1000,
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()
      client.disconnect()

      vi.advanceTimersByTime(5000)

      expect(mockWebSocketInstances.length).toBe(1)
    })
  })

  describe('error handling', () => {
    it('should call onStatusChange with error on WebSocket error', () => {
      const onStatusChange = vi.fn()
      const client = new WebSocketClient({
        url: 'ws://test.com',
        onStatusChange,
      })

      client.connect()
      mockWebSocketInstances[0].simulateError()

      expect(onStatusChange).toHaveBeenCalledWith('error')
    })
  })

  describe('status', () => {
    it('should return disconnected when no WebSocket', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      expect(client.status).toBe('disconnected')
    })

    it('should return connecting when connecting', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      client.connect()

      expect(client.status).toBe('connecting')
    })

    it('should return connected when open', () => {
      const client = new WebSocketClient({
        url: 'ws://test.com',
      })

      client.connect()
      mockWebSocketInstances[0].simulateOpen()

      expect(client.status).toBe('connected')
    })
  })
})

describe('buildWebSocketUrl', () => {
  const originalLocation = window.location

  beforeEach(() => {
    // Reset location mock
    Object.defineProperty(window, 'location', {
      value: {
        protocol: 'http:',
        host: 'localhost:5173',
      },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
    })
  })

  it('should build ws URL for http protocol', () => {
    const url = buildWebSocketUrl('/api/v1/streams/123/connect')

    expect(url).toBe('ws://localhost:5173/api/v1/streams/123/connect')
  })

  it('should build wss URL for https protocol', () => {
    Object.defineProperty(window, 'location', {
      value: {
        protocol: 'https:',
        host: 'example.com',
      },
      writable: true,
    })

    const url = buildWebSocketUrl('/api/v1/streams/123/connect')

    expect(url).toBe('wss://example.com/api/v1/streams/123/connect')
  })
})
