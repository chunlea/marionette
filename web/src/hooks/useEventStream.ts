import { useState, useEffect, useCallback, useRef } from 'react'
import { WebSocketClient, WebSocketStatus, buildWebSocketUrl } from '@/lib/websocket'
import { getApiKey } from '@/api/client'
import type { PermissionRequest } from '@/types/api'

export type EventType = 'permission_request' | 'session_update' | 'task_update' | 'runner_update'

export interface StreamEvent {
  type: EventType
  data: unknown
  timestamp: string
}

export interface PermissionEvent extends StreamEvent {
  type: 'permission_request'
  data: PermissionRequest
}

export interface UseEventStreamOptions {
  enabled?: boolean
  onEvent?: (event: StreamEvent) => void
  onPermission?: (permission: PermissionRequest) => void
}

export interface UseEventStreamResult {
  status: WebSocketStatus
  isConnected: boolean
  reconnect: () => void
}

export function useEventStream({
  enabled = true,
  onEvent,
  onPermission,
}: UseEventStreamOptions = {}): UseEventStreamResult {
  const [status, setStatus] = useState<WebSocketStatus>('disconnected')
  const clientRef = useRef<WebSocketClient | null>(null)

  const handleMessage = useCallback(
    (data: unknown) => {
      if (!data || typeof data !== 'object') return

      const event = data as StreamEvent
      onEvent?.(event)

      // Handle specific event types
      if (event.type === 'permission_request') {
        onPermission?.(event.data as PermissionRequest)
      }
    },
    [onEvent, onPermission]
  )

  const connect = useCallback(() => {
    if (!enabled) return

    const apiKey = getApiKey()
    if (!apiKey) return

    const url = buildWebSocketUrl(`/api/v1/events?token=${encodeURIComponent(apiKey)}`)

    clientRef.current = new WebSocketClient({
      url,
      onMessage: handleMessage,
      onStatusChange: setStatus,
    })

    clientRef.current.connect()
  }, [enabled, handleMessage])

  const disconnect = useCallback(() => {
    clientRef.current?.disconnect()
    clientRef.current = null
  }, [])

  const reconnect = useCallback(() => {
    disconnect()
    connect()
  }, [connect, disconnect])

  useEffect(() => {
    connect()
    return () => disconnect()
  }, [connect, disconnect])

  return {
    status,
    isConnected: status === 'connected',
    reconnect,
  }
}
