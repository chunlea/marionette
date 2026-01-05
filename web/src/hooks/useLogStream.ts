import { useState, useEffect, useCallback, useRef } from 'react'
import { WebSocketClient, WebSocketStatus, buildWebSocketUrl } from '@/lib/websocket'
import { getApiKey } from '@/api/client'
import type { Log } from '@/types/api'

export interface LogEntry extends Log {
  raw?: boolean
}

export interface UseLogStreamOptions {
  taskId: string
  enabled?: boolean
  maxLogs?: number
  onNewLog?: (log: LogEntry) => void
}

export interface UseLogStreamResult {
  logs: LogEntry[]
  status: WebSocketStatus
  isConnected: boolean
  clearLogs: () => void
  reconnect: () => void
}

export function useLogStream({
  taskId,
  enabled = true,
  maxLogs = 1000,
  onNewLog,
}: UseLogStreamOptions): UseLogStreamResult {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [status, setStatus] = useState<WebSocketStatus>('disconnected')
  const clientRef = useRef<WebSocketClient | null>(null)

  const handleMessage = useCallback(
    (data: unknown) => {
      if (!data || typeof data !== 'object') return

      const logEntry = data as LogEntry

      setLogs((prev) => {
        const newLogs = [...prev, logEntry]
        // Trim logs if exceeding max
        if (newLogs.length > maxLogs) {
          return newLogs.slice(-maxLogs)
        }
        return newLogs
      })

      onNewLog?.(logEntry)
    },
    [maxLogs, onNewLog]
  )

  const connect = useCallback(() => {
    if (!taskId || !enabled) return

    const apiKey = getApiKey()
    if (!apiKey) return

    const url = buildWebSocketUrl(`/api/v1/tasks/${taskId}/logs/stream?token=${encodeURIComponent(apiKey)}`)

    clientRef.current = new WebSocketClient({
      url,
      onMessage: handleMessage,
      onStatusChange: setStatus,
    })

    clientRef.current.connect()
  }, [taskId, enabled, handleMessage])

  const disconnect = useCallback(() => {
    clientRef.current?.disconnect()
    clientRef.current = null
  }, [])

  const reconnect = useCallback(() => {
    disconnect()
    connect()
  }, [connect, disconnect])

  const clearLogs = useCallback(() => {
    setLogs([])
  }, [])

  useEffect(() => {
    connect()
    return () => disconnect()
  }, [connect, disconnect])

  return {
    logs,
    status,
    isConnected: status === 'connected',
    clearLogs,
    reconnect,
  }
}
