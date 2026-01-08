import { useState, useEffect, useCallback, useRef } from 'react'
import { WebSocketClient, WebSocketStatus, buildWebSocketUrl } from '@/lib/websocket'
import type {
  WSFrameMessage,
  WSInputMessage,
  WSControlMessage,
  WSStatsMessage,
  WSMessage,
  StreamStats,
  StreamConnectionStatus,
} from './types'

export interface UseStreamConnectionOptions {
  tunnelId: string
  token: string
  enabled?: boolean
  onFrame?: (frame: WSFrameMessage) => void
  onStats?: (stats: StreamStats) => void
  onError?: (error: Error) => void
}

export interface UseStreamConnectionResult {
  status: StreamConnectionStatus
  isConnected: boolean
  stats: StreamStats | null
  lastFrame: WSFrameMessage | null
  sendInput: (event: WSInputMessage['event']) => void
  sendControl: (command: WSControlMessage['command'], payload?: Record<string, unknown>) => void
  reconnect: () => void
  disconnect: () => void
}

export function useStreamConnection({
  tunnelId,
  token,
  enabled = true,
  onFrame,
  onStats,
  onError,
}: UseStreamConnectionOptions): UseStreamConnectionResult {
  const [status, setStatus] = useState<StreamConnectionStatus>('disconnected')
  const [stats, setStats] = useState<StreamStats | null>(null)
  const [lastFrame, setLastFrame] = useState<WSFrameMessage | null>(null)
  const clientRef = useRef<WebSocketClient | null>(null)

  const handleStatusChange = useCallback((wsStatus: WebSocketStatus) => {
    switch (wsStatus) {
      case 'connecting':
        setStatus('connecting')
        break
      case 'connected':
        setStatus('connected')
        break
      case 'error':
        setStatus('error')
        break
      case 'disconnected':
      default:
        setStatus('disconnected')
        break
    }
  }, [])

  const handleMessage = useCallback(
    (data: unknown) => {
      if (!data || typeof data !== 'object') return

      const message = data as WSMessage

      switch (message.type) {
        case 'frame':
          setLastFrame(message as WSFrameMessage)
          onFrame?.(message as WSFrameMessage)
          break
        case 'stats': {
          const statsMsg = message as WSStatsMessage
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
          // Handle state updates if needed
          break
      }
    },
    [onFrame, onStats]
  )

  const connect = useCallback(() => {
    if (!tunnelId || !token || !enabled) return

    const url = buildWebSocketUrl(
      `/api/v1/streams/${tunnelId}/connect?token=${encodeURIComponent(token)}`
    )

    clientRef.current = new WebSocketClient({
      url,
      onMessage: handleMessage,
      onStatusChange: handleStatusChange,
      reconnectDelay: 2000,
      maxReconnectAttempts: 5,
    })

    clientRef.current.connect()
  }, [tunnelId, token, enabled, handleMessage, handleStatusChange])

  const disconnect = useCallback(() => {
    clientRef.current?.disconnect()
    clientRef.current = null
    setStatus('disconnected')
  }, [])

  const reconnect = useCallback(() => {
    disconnect()
    setTimeout(() => connect(), 100)
  }, [connect, disconnect])

  const sendInput = useCallback((event: WSInputMessage['event']) => {
    const message: WSInputMessage = {
      type: 'input',
      event: {
        ...event,
        timestamp_ms: Date.now(),
      },
    }
    clientRef.current?.send(message)
  }, [])

  const sendControl = useCallback(
    (command: WSControlMessage['command'], payload?: Record<string, unknown>) => {
      const message: WSControlMessage = {
        type: 'control',
        command,
        payload,
      }
      clientRef.current?.send(message)
    },
    []
  )

  // Connect on mount, disconnect on unmount
  useEffect(() => {
    if (enabled) {
      connect()
    }
    return () => disconnect()
  }, [connect, disconnect, enabled])

  // Report errors
  useEffect(() => {
    if (status === 'error' && onError) {
      onError(new Error('WebSocket connection error'))
    }
  }, [status, onError])

  return {
    status,
    isConnected: status === 'connected',
    stats,
    lastFrame,
    sendInput,
    sendControl,
    reconnect,
    disconnect,
  }
}
