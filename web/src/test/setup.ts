import '@testing-library/react'
import { vi } from 'vitest'

// Mock window.location for WebSocket URL building
Object.defineProperty(window, 'location', {
  value: {
    protocol: 'http:',
    host: 'localhost:5173',
    hostname: 'localhost',
    port: '5173',
    pathname: '/',
    search: '',
    hash: '',
    href: 'http://localhost:5173/',
  },
  writable: true,
})

// Mock WebSocket
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

  constructor(url: string) {
    this.url = url
    // Simulate async connection
    setTimeout(() => {
      this.readyState = MockWebSocket.OPEN
      this.onopen?.(new Event('open'))
    }, 0)
  }

  send(_data: string): void {
    // Capture sent data for testing
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }
}

// @ts-expect-error - MockWebSocket doesn't implement full WebSocket interface
global.WebSocket = MockWebSocket

// buildWebSocketUrl falls back to window.location.host when VITE_WS_HOST is
// unset, which is what the URL tests exercise. Assigning to import.meta.env
// wholesale is not something Vitest 4 can rewrite; stubEnv is the supported
// way to pin one variable.
vi.stubEnv('VITE_WS_HOST', '')
