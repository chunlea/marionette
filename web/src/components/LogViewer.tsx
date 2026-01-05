import { useEffect, useRef, useState, useCallback } from 'react'
import { useLogStream, type LogEntry } from '@/hooks/useLogStream'
import LogLine from './LogLine'
import { Button } from './Button'
import { Badge } from './Badge'
import {
  Play,
  Pause,
  Trash2,
  Download,
  ArrowDown,
  Wifi,
  WifiOff,
  Loader2,
} from 'lucide-react'

interface LogViewerProps {
  taskId: string
  initialLogs?: LogEntry[]
  maxHeight?: string
  showControls?: boolean
  showTimestamps?: boolean
}

export function LogViewer({
  taskId,
  initialLogs = [],
  maxHeight = '500px',
  showControls = true,
  showTimestamps = false,
}: LogViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)
  const [isPaused, setIsPaused] = useState(false)
  const [localLogs, setLocalLogs] = useState<LogEntry[]>(initialLogs)

  const handleNewLog = useCallback((log: LogEntry) => {
    if (!isPaused) {
      setLocalLogs((prev) => [...prev, log])
    }
  }, [isPaused])

  const { logs: streamLogs, status, isConnected, clearLogs } = useLogStream({
    taskId,
    enabled: !isPaused,
    onNewLog: handleNewLog,
  })

  // Combine initial logs with stream logs
  const allLogs = [...localLogs, ...streamLogs.filter(
    (log) => !localLogs.some((l) => l.id === log.id)
  )]

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [allLogs.length, autoScroll])

  // Detect manual scroll
  const handleScroll = () => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 50
    setAutoScroll(isAtBottom)
  }

  const handleClear = () => {
    setLocalLogs([])
    clearLogs()
  }

  const handleDownload = () => {
    const content = allLogs.map((log) => {
      const ts = new Date(log.created_at).toISOString()
      return `[${ts}] [${log.level}] [${log.stream}] ${log.content}`
    }).join('\n')

    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `task-${taskId}-logs.txt`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  const scrollToBottom = () => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
      setAutoScroll(true)
    }
  }

  return (
    <div className="flex flex-col rounded-lg border border-gray-200 bg-gray-900">
      {/* Header */}
      {showControls && (
        <div className="flex items-center justify-between border-b border-gray-700 px-4 py-2">
          <div className="flex items-center gap-2">
            <ConnectionStatus status={status} />
            <span className="text-sm text-gray-400">
              {allLogs.length} lines
            </span>
          </div>
          <div className="flex items-center gap-2">
            {!autoScroll && (
              <Button
                variant="ghost"
                size="sm"
                onClick={scrollToBottom}
                className="text-gray-400 hover:text-white"
              >
                <ArrowDown className="h-4 w-4" />
              </Button>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setIsPaused(!isPaused)}
              className="text-gray-400 hover:text-white"
            >
              {isPaused ? (
                <Play className="h-4 w-4" />
              ) : (
                <Pause className="h-4 w-4" />
              )}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleClear}
              className="text-gray-400 hover:text-white"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleDownload}
              className="text-gray-400 hover:text-white"
            >
              <Download className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}

      {/* Log content */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="overflow-auto p-4"
        style={{ maxHeight }}
      >
        {allLogs.length === 0 ? (
          <div className="flex items-center justify-center py-8 text-gray-500">
            {isConnected ? 'Waiting for logs...' : 'Connecting...'}
          </div>
        ) : (
          <div className="space-y-0.5">
            {allLogs.map((log, index) => (
              <LogLine
                key={log.id || index}
                content={log.content}
                level={log.level}
                stream={log.stream}
                timestamp={log.created_at}
                showTimestamp={showTimestamps}
              />
            ))}
          </div>
        )}
      </div>

      {/* Footer - paused indicator */}
      {isPaused && (
        <div className="flex items-center justify-center border-t border-gray-700 py-2">
          <Badge variant="warning" className="flex items-center gap-1">
            <Pause className="h-3 w-3" />
            Paused
          </Badge>
        </div>
      )}
    </div>
  )
}

function ConnectionStatus({ status }: { status: string }) {
  switch (status) {
    case 'connected':
      return (
        <div className="flex items-center gap-1 text-green-400">
          <Wifi className="h-4 w-4" />
          <span className="text-xs">Live</span>
        </div>
      )
    case 'connecting':
      return (
        <div className="flex items-center gap-1 text-yellow-400">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span className="text-xs">Connecting</span>
        </div>
      )
    case 'error':
    case 'disconnected':
    default:
      return (
        <div className="flex items-center gap-1 text-red-400">
          <WifiOff className="h-4 w-4" />
          <span className="text-xs">Disconnected</span>
        </div>
      )
  }
}

export default LogViewer
