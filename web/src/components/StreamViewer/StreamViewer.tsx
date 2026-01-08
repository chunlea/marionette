import { useState, useCallback, useRef, useEffect } from 'react'
import { useStreamConnection } from './useStreamConnection'
import { StreamCanvas } from './StreamCanvas'
import { StreamControls } from './StreamControls'
import { StreamOverlay } from './StreamOverlay'
import type { StreamViewerProps, WSInputEvent } from './types'

export function StreamViewer({
  tunnelId,
  token,
  onDisconnect,
  onError,
  className = '',
  showControls = true,
  showStats = true,
}: StreamViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [isPaused, setIsPaused] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [showControlsOverlay, setShowControlsOverlay] = useState(true)

  const {
    status,
    isConnected,
    stats,
    lastFrame,
    sendInput,
    sendControl,
    reconnect,
    disconnect,
  } = useStreamConnection({
    tunnelId,
    token,
    enabled: !isPaused,
    onError,
  })

  // Handle pause/resume
  const handlePause = useCallback(() => {
    setIsPaused(true)
    sendControl('pause')
  }, [sendControl])

  const handleResume = useCallback(() => {
    setIsPaused(false)
    sendControl('resume')
  }, [sendControl])

  // Handle fullscreen
  const handleFullscreen = useCallback(() => {
    if (containerRef.current) {
      containerRef.current.requestFullscreen?.()
      setIsFullscreen(true)
    }
  }, [])

  const handleExitFullscreen = useCallback(() => {
    document.exitFullscreen?.()
    setIsFullscreen(false)
  }, [])

  // Listen for fullscreen changes
  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(!!document.fullscreenElement)
    }

    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange)
    }
  }, [])

  // Handle close/disconnect
  const handleClose = useCallback(() => {
    disconnect()
    onDisconnect?.()
  }, [disconnect, onDisconnect])

  // Handle input events from canvas
  const handleInput = useCallback(
    (event: WSInputEvent) => {
      if (isPaused) return
      sendInput(event)
    },
    [isPaused, sendInput]
  )

  // Auto-hide controls after inactivity
  useEffect(() => {
    if (!showControls) return

    let timeout: number

    const showAndHide = () => {
      setShowControlsOverlay(true)
      clearTimeout(timeout)
      timeout = window.setTimeout(() => {
        setShowControlsOverlay(false)
      }, 3000)
    }

    const container = containerRef.current
    if (container) {
      container.addEventListener('mousemove', showAndHide)
      container.addEventListener('mouseenter', showAndHide)
    }

    // Show initially
    showAndHide()

    return () => {
      clearTimeout(timeout)
      if (container) {
        container.removeEventListener('mousemove', showAndHide)
        container.removeEventListener('mouseenter', showAndHide)
      }
    }
  }, [showControls])

  return (
    <div
      ref={containerRef}
      className={`relative overflow-hidden bg-black ${className}`}
      style={{ minHeight: '400px' }}
    >
      {/* Canvas for rendering frames */}
      <StreamCanvas
        frame={lastFrame}
        onInput={handleInput}
        interactive={isConnected && !isPaused}
        className="h-full w-full"
      />

      {/* Overlay for connection states */}
      <StreamOverlay
        status={status}
        hasFrame={!!lastFrame}
        onReconnect={reconnect}
      />

      {/* Controls overlay */}
      {showControls && showControlsOverlay && (
        <StreamControls
          status={status}
          stats={stats}
          isPaused={isPaused}
          isFullscreen={isFullscreen}
          onPause={handlePause}
          onResume={handleResume}
          onFullscreen={handleFullscreen}
          onExitFullscreen={handleExitFullscreen}
          onReconnect={reconnect}
          onClose={onDisconnect ? handleClose : undefined}
          showStats={showStats}
        />
      )}

      {/* Paused indicator */}
      {isPaused && (
        <div className="absolute left-1/2 top-4 -translate-x-1/2 rounded-full bg-yellow-500/80 px-4 py-1 text-sm font-medium text-black">
          Paused
        </div>
      )}
    </div>
  )
}

export default StreamViewer
