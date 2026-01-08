import { Button } from '@/components/Button'
import {
  Maximize2,
  Minimize2,
  Pause,
  Play,
  RefreshCw,
  X,
} from 'lucide-react'
import type { StreamStats, StreamConnectionStatus } from './types'

interface StreamControlsProps {
  status: StreamConnectionStatus
  stats: StreamStats | null
  isPaused: boolean
  isFullscreen: boolean
  onPause: () => void
  onResume: () => void
  onFullscreen: () => void
  onExitFullscreen: () => void
  onReconnect: () => void
  onClose?: () => void
  showStats?: boolean
}

export function StreamControls({
  status,
  stats,
  isPaused,
  isFullscreen,
  onPause,
  onResume,
  onFullscreen,
  onExitFullscreen,
  onReconnect,
  onClose,
  showStats = true,
}: StreamControlsProps) {
  return (
    <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/80 to-transparent p-4">
      <div className="flex items-center justify-between">
        {/* Left side - connection status and stats */}
        <div className="flex items-center gap-4">
          <ConnectionIndicator status={status} />
          {showStats && stats && (
            <div className="flex items-center gap-4 text-xs text-gray-400">
              <span>{Math.round(stats.currentFps)} FPS</span>
              <span>{stats.framesDelivered} frames</span>
              {stats.framesDropped > 0 && (
                <span className="text-yellow-400">
                  {stats.framesDropped} dropped
                </span>
              )}
            </div>
          )}
        </div>

        {/* Right side - controls */}
        <div className="flex items-center gap-2">
          {/* Pause/Resume */}
          <Button
            variant="ghost"
            size="sm"
            onClick={isPaused ? onResume : onPause}
            className="text-white hover:bg-white/20"
            title={isPaused ? 'Resume' : 'Pause'}
          >
            {isPaused ? (
              <Play className="h-4 w-4" />
            ) : (
              <Pause className="h-4 w-4" />
            )}
          </Button>

          {/* Reconnect (only show when disconnected) */}
          {(status === 'disconnected' || status === 'error') && (
            <Button
              variant="ghost"
              size="sm"
              onClick={onReconnect}
              className="text-white hover:bg-white/20"
              title="Reconnect"
            >
              <RefreshCw className="h-4 w-4" />
            </Button>
          )}

          {/* Fullscreen toggle */}
          <Button
            variant="ghost"
            size="sm"
            onClick={isFullscreen ? onExitFullscreen : onFullscreen}
            className="text-white hover:bg-white/20"
            title={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? (
              <Minimize2 className="h-4 w-4" />
            ) : (
              <Maximize2 className="h-4 w-4" />
            )}
          </Button>

          {/* Close button */}
          {onClose && (
            <Button
              variant="ghost"
              size="sm"
              onClick={onClose}
              className="text-white hover:bg-white/20"
              title="Close"
            >
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

function ConnectionIndicator({ status }: { status: StreamConnectionStatus }) {
  switch (status) {
    case 'connected':
      return (
        <div className="flex items-center gap-2">
          <div className="h-2 w-2 rounded-full bg-green-500" />
          <span className="text-xs text-green-400">Live</span>
        </div>
      )
    case 'connecting':
      return (
        <div className="flex items-center gap-2">
          <div className="h-2 w-2 animate-pulse rounded-full bg-yellow-500" />
          <span className="text-xs text-yellow-400">Connecting...</span>
        </div>
      )
    case 'error':
      return (
        <div className="flex items-center gap-2">
          <div className="h-2 w-2 rounded-full bg-red-500" />
          <span className="text-xs text-red-400">Error</span>
        </div>
      )
    case 'disconnected':
    default:
      return (
        <div className="flex items-center gap-2">
          <div className="h-2 w-2 rounded-full bg-gray-500" />
          <span className="text-xs text-gray-400">Disconnected</span>
        </div>
      )
  }
}

export default StreamControls
