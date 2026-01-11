import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useStreamConnection } from './useStreamConnection'

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
  sentMessages: unknown[] = []

  constructor(url: string) {
    this.url = url
    mockWebSocketInstances.push(this)
    // Auto-connect
    setTimeout(() => this.simulateOpen(), 0)
  }

  send(data: string): void {
    this.sentMessages.push(JSON.parse(data))
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }

  simulateOpen(): void {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.(new Event('open'))
  }

  simulateMessage(data: unknown): void {
    const messageEvent = new MessageEvent('message', {
      data: JSON.stringify(data),
    })
    this.onmessage?.(messageEvent)
  }

  simulateError(): void {
    this.onerror?.(new Event('error'))
  }
}

let mockWebSocketInstances: MockWebSocket[] = []
const originalWebSocket = global.WebSocket

describe('useStreamConnection', () => {
  beforeEach(() => {
    mockWebSocketInstances = []
    // @ts-expect-error - MockWebSocket doesn't implement full WebSocket interface
    global.WebSocket = MockWebSocket
    vi.useFakeTimers()

    // Mock window.location
    Object.defineProperty(window, 'location', {
      value: {
        protocol: 'http:',
        host: 'localhost:5173',
      },
      writable: true,
    })
  })

  afterEach(() => {
    global.WebSocket = originalWebSocket
    vi.useRealTimers()
  })

  describe('initial state', () => {
    it('should start with disconnected status', () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: false,
        })
      )

      expect(result.current.status).toBe('disconnected')
      expect(result.current.isConnected).toBe(false)
      expect(result.current.stats).toBeNull()
      expect(result.current.lastFrame).toBeNull()
    })
  })

  describe('connection', () => {
    it('should connect when enabled', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      // Run pending timers to trigger WebSocket connection
      await act(async () => {
        vi.runAllTimers()
      })

      expect(result.current.status).toBe('connected')
      expect(result.current.isConnected).toBe(true)
    })

    it('should not connect when disabled', () => {
      renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: false,
        })
      )

      expect(mockWebSocketInstances.length).toBe(0)
    })

    it('should not connect without streamId', () => {
      renderHook(() =>
        useStreamConnection({
          streamId: '',
          token: 'test-token',
          enabled: true,
        })
      )

      expect(mockWebSocketInstances.length).toBe(0)
    })

    it('should not connect without token', () => {
      renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: '',
          enabled: true,
        })
      )

      expect(mockWebSocketInstances.length).toBe(0)
    })
  })

  describe('frame handling', () => {
    it('should update lastFrame on frame message', async () => {
      const onFrame = vi.fn()
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
          onFrame,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      const frameMessage = {
        type: 'frame',
        data: 'base64data',
        format: 'jpeg',
        width: 1280,
        height: 720,
        sequence: 1,
        timestamp: Date.now(),
      }

      act(() => {
        mockWebSocketInstances[0].simulateMessage(frameMessage)
      })

      expect(result.current.lastFrame).toEqual(frameMessage)
      expect(onFrame).toHaveBeenCalledWith(frameMessage)
    })
  })

  describe('stats handling', () => {
    it('should update stats on stats message', async () => {
      const onStats = vi.fn()
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
          onStats,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      const statsMessage = {
        type: 'stats',
        frames_received: 100,
        frames_delivered: 95,
        frames_dropped: 5,
        current_fps: 30,
        subscriber_count: 1,
      }

      act(() => {
        mockWebSocketInstances[0].simulateMessage(statsMessage)
      })

      expect(result.current.stats).toEqual({
        framesReceived: 100,
        framesDelivered: 95,
        framesDropped: 5,
        currentFps: 30,
        subscriberCount: 1,
      })
      expect(onStats).toHaveBeenCalled()
    })
  })

  describe('sendInput', () => {
    it('should send input message with timestamp', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      const inputEvent = {
        event_type: 'mouseDown',
        mouse: { x: 100, y: 200, button: 'left' as const },
      }

      act(() => {
        result.current.sendInput(inputEvent)
      })

      const sentMessage = mockWebSocketInstances[0].sentMessages[0] as {
        type: string
        event: { event_type: string; timestamp_ms: number }
      }
      expect(sentMessage.type).toBe('input')
      expect(sentMessage.event.event_type).toBe('mouseDown')
      expect(sentMessage.event.timestamp_ms).toBeDefined()
    })
  })

  describe('sendControl', () => {
    it('should send control message', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      act(() => {
        result.current.sendControl('pause')
      })

      const sentMessage = mockWebSocketInstances[0].sentMessages[0] as {
        type: string
        command: string
      }
      expect(sentMessage.type).toBe('control')
      expect(sentMessage.command).toBe('pause')
    })

    it('should send control message with payload', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      act(() => {
        result.current.sendControl('navigate', { url: 'https://example.com' })
      })

      const sentMessage = mockWebSocketInstances[0].sentMessages[0] as {
        type: string
        command: string
        payload: { url: string }
      }
      expect(sentMessage.type).toBe('control')
      expect(sentMessage.command).toBe('navigate')
      expect(sentMessage.payload).toEqual({ url: 'https://example.com' })
    })
  })

  describe('disconnect', () => {
    it('should disconnect and update status', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      expect(result.current.isConnected).toBe(true)

      act(() => {
        result.current.disconnect()
      })

      expect(result.current.status).toBe('disconnected')
      expect(result.current.isConnected).toBe(false)
    })
  })

  describe('reconnect', () => {
    it('should disconnect and reconnect', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      const initialInstance = mockWebSocketInstances[0]

      act(() => {
        result.current.reconnect()
      })

      await act(async () => {
        vi.advanceTimersByTime(200)
      })

      // Should have created a new WebSocket instance
      expect(mockWebSocketInstances.length).toBeGreaterThan(1)
      expect(mockWebSocketInstances[mockWebSocketInstances.length - 1]).not.toBe(
        initialInstance
      )
    })
  })

  describe('error handling', () => {
    it('should call onError when status changes to error', async () => {
      const onError = vi.fn()
      renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
          onError,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      act(() => {
        mockWebSocketInstances[0].simulateError()
      })

      expect(onError).toHaveBeenCalled()
    })
  })

  describe('cleanup', () => {
    it('should disconnect on unmount', async () => {
      const { unmount } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      const ws = mockWebSocketInstances[0]
      expect(ws.readyState).toBe(MockWebSocket.OPEN)

      unmount()

      expect(ws.readyState).toBe(MockWebSocket.CLOSED)
    })
  })

  describe('state message handling', () => {
    it('should handle state messages without error', async () => {
      const { result } = renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      // Should not throw when receiving state message
      act(() => {
        mockWebSocketInstances[0].simulateMessage({
          type: 'state',
          state: 'connected',
          message: 'Browser ready',
        })
      })

      // Hook should still be working
      expect(result.current.isConnected).toBe(true)
    })
  })

  describe('invalid message handling', () => {
    it('should handle null messages gracefully', async () => {
      const onFrame = vi.fn()
      renderHook(() =>
        useStreamConnection({
          streamId: 'test-tunnel',
          token: 'test-token',
          enabled: true,
          onFrame,
        })
      )

      await act(async () => {
        vi.advanceTimersByTime(10)
      })

      // Should not throw when receiving null/undefined
      act(() => {
        const messageEvent = new MessageEvent('message', {
          data: 'null',
        })
        mockWebSocketInstances[0].onmessage?.(messageEvent)
      })

      expect(onFrame).not.toHaveBeenCalled()
    })
  })
})
