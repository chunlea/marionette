import { useCallback, useEffect, useRef, useState } from 'react'

// Frame message from the server
export interface FrameMessage {
  type: 'frame'
  data: string // base64 encoded
  format: string
  width: number
  height: number
  sequence: number
  timestamp: number
}

// Stats message from the server
export interface StatsMessage {
  type: 'stats'
  frames_received: number
  frames_delivered: number
  frames_dropped: number
  current_fps: number
  subscriber_count: number
}

// State message from the server
export interface StateMessage {
  type: 'state'
  state: string
  message?: string
}

// Connection status
export type ConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'error'

// Stats exposed to the consumer
export interface StreamStats {
  framesReceived: number
  framesDelivered: number
  framesDropped: number
  currentFps: number
  subscriberCount: number
}

// Input event to send to the agent
export interface InputEvent {
  event_type: string
  mouse?: {
    x: number
    y: number
    button?: 'left' | 'middle' | 'right'
    click_count?: number
    delta_x?: number
    delta_y?: number
  }
  keyboard?: {
    key: string
    code?: string
    text?: string
    modifiers?: {
      alt?: boolean
      ctrl?: boolean
      meta?: boolean
      shift?: boolean
    }
  }
}

// Hook props
export interface UseStreamConnectionProps {
  streamId: string
  token: string
  enabled: boolean
  onFrame?: (frame: FrameMessage) => void
  onStats?: (stats: StreamStats) => void
  onError?: (error: Error) => void
}

// Hook return type
export interface UseStreamConnectionReturn {
  status: ConnectionStatus
  isConnected: boolean
  lastFrame: FrameMessage | null
  stats: StreamStats | null
  sendInput: (event: InputEvent) => void
  sendControl: (command: string, payload?: unknown) => void
  disconnect: () => void
  reconnect: () => void
}

export function useStreamConnection({
  streamId,
  token,
  enabled,
  onFrame,
  onStats,
  onError,
}: UseStreamConnectionProps): UseStreamConnectionReturn {
  const [status, setStatus] = useState<ConnectionStatus>('disconnected')
  const [lastFrame, setLastFrame] = useState<FrameMessage | null>(null)
  const [stats, setStats] = useState<StreamStats | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    setStatus('disconnected')
  }, [])

  const connect = useCallback(() => {
    if (!streamId || !token || !enabled) {
      return
    }

    // Build WebSocket URL
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/v1/streams/${streamId}/ws?token=${encodeURIComponent(token)}`

    setStatus('connecting')

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      setStatus('connected')
    }

    ws.onclose = () => {
      if (wsRef.current === ws) {
        setStatus('disconnected')
      }
    }

    ws.onerror = () => {
      setStatus('error')
      onError?.(new Error('WebSocket connection error'))
    }

    ws.onmessage = (event: MessageEvent) => {
      try {
        const message = JSON.parse(event.data)
        if (!message || typeof message !== 'object') {
          return
        }

        switch (message.type) {
          case 'frame':
            setLastFrame(message as FrameMessage)
            onFrame?.(message as FrameMessage)
            break

          case 'stats': {
            const statsMsg = message as StatsMessage
            const newStats: StreamStats = {
              framesReceived: statsMsg.frames_received,
              framesDelivered: statsMsg.frames_delivered,
              framesDropped: statsMsg.frames_dropped,
              currentFps: statsMsg.current_fps,
              subscriberCount: statsMsg.subscriber_count,
            }
            setStats(newStats)
            onStats?.(newStats)
            break
          }

          case 'state':
            // Handle state messages (logging, etc.)
            break

          default:
            // Unknown message type, ignore
            break
        }
      } catch {
        // Invalid JSON, ignore
      }
    }
  }, [streamId, token, enabled, onFrame, onStats, onError])

  const reconnect = useCallback(() => {
    disconnect()
    // Small delay before reconnecting
    reconnectTimeoutRef.current = setTimeout(() => {
      connect()
    }, 100)
  }, [disconnect, connect])

  const sendInput = useCallback((event: InputEvent) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      const message = {
        type: 'input',
        event: {
          ...event,
          timestamp_ms: Date.now(),
        },
      }
      wsRef.current.send(JSON.stringify(message))
    }
  }, [])

  const sendControl = useCallback((command: string, payload?: unknown) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      const message: Record<string, unknown> = {
        type: 'control',
        command,
      }
      if (payload !== undefined) {
        message.payload = payload
      }
      wsRef.current.send(JSON.stringify(message))
    }
  }, [])

  // Connect when enabled and have required params
  useEffect(() => {
    if (enabled && streamId && token) {
      connect()
    }
    return () => {
      disconnect()
    }
  }, [enabled, streamId, token, connect, disconnect])

  return {
    status,
    isConnected: status === 'connected',
    lastFrame,
    stats,
    sendInput,
    sendControl,
    disconnect,
    reconnect,
  }
}
