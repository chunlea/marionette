import { useRef, useState, useEffect, useCallback } from 'react'
import {
  Maximize2,
  Minimize2,
  Wifi,
  WifiOff,
  Loader2,
  Play,
  Pause,
  Volume2,
  VolumeX,
  RefreshCw,
  Keyboard,
  MousePointer,
} from 'lucide-react'
import { Button } from '@/components/Button'
import { Badge } from '@/components/Badge'
import { useWebRTCStream } from '@/hooks/useWebRTCStream'
import { InputForwarder } from '@/lib/input-forwarder'
import type { ConnectionState } from '@/types/stream'

export interface DesktopViewerProps {
  streamId: string
  signalingUrl?: string
  enabled?: boolean
  showControls?: boolean
  className?: string
  onConnected?: () => void
  onDisconnected?: () => void
  onError?: (error: Error) => void
}

export function DesktopViewer({
  streamId,
  enabled = true,
  showControls = true,
  className = '',
  onConnected,
  onDisconnected,
  onError,
}: DesktopViewerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputForwarderRef = useRef<InputForwarder | null>(null)

  const [isFullscreen, setIsFullscreen] = useState(false)
  const [isMuted, setIsMuted] = useState(true)
  const [isPaused, setIsPaused] = useState(false)
  const [inputEnabled, setInputEnabled] = useState(true)
  const [videoSize, setVideoSize] = useState({ width: 0, height: 0 })

  // WebRTC connection
  const {
    dataChannel,
    connectionState,
    signalingStatus,
    isConnected,
    error,
    reconnect,
  } = useWebRTCStream({
    streamId,
    enabled,
    onConnected,
    onDisconnected,
    onError,
    onTrack: (track, streams) => {
      if (videoRef.current && streams.length > 0) {
        videoRef.current.srcObject = streams[0]
      }
    },
  })

  // Set up input forwarder
  useEffect(() => {
    if (!videoRef.current) return

    inputForwarderRef.current = new InputForwarder({
      videoElement: videoRef.current,
      dataChannel,
      enabled: inputEnabled,
    })

    inputForwarderRef.current.attach()

    return () => {
      inputForwarderRef.current?.detach()
      inputForwarderRef.current = null
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Update data channel in forwarder when it changes
  useEffect(() => {
    inputForwarderRef.current?.setDataChannel(dataChannel)
  }, [dataChannel])

  // Update input enabled state
  useEffect(() => {
    inputForwarderRef.current?.setEnabled(inputEnabled)
  }, [inputEnabled])

  // Track video dimensions
  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const handleResize = () => {
      setVideoSize({
        width: video.videoWidth,
        height: video.videoHeight,
      })
    }

    video.addEventListener('loadedmetadata', handleResize)
    video.addEventListener('resize', handleResize)

    return () => {
      video.removeEventListener('loadedmetadata', handleResize)
      video.removeEventListener('resize', handleResize)
    }
  }, [])

  // Fullscreen handling
  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(!!document.fullscreenElement)
    }

    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange)
    }
  }, [])

  const toggleFullscreen = useCallback(async () => {
    if (!containerRef.current) return

    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen()
      } else {
        await containerRef.current.requestFullscreen()
      }
    } catch (err) {
      console.error('Fullscreen error:', err)
    }
  }, [])

  const toggleMute = useCallback(() => {
    if (videoRef.current) {
      videoRef.current.muted = !videoRef.current.muted
      setIsMuted(videoRef.current.muted)
    }
  }, [])

  const togglePause = useCallback(() => {
    if (videoRef.current) {
      if (videoRef.current.paused) {
        videoRef.current.play()
      } else {
        videoRef.current.pause()
      }
      setIsPaused(videoRef.current.paused)
    }
  }, [])

  const toggleInput = useCallback(() => {
    setInputEnabled((prev) => !prev)
  }, [])

  const getStatusBadge = () => {
    if (error) {
      return <Badge variant="danger">Error</Badge>
    }

    switch (connectionState) {
      case 'new':
      case 'connecting':
        return (
          <Badge variant="warning" className="flex items-center gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            Connecting
          </Badge>
        )
      case 'connected':
        return (
          <Badge variant="success" className="flex items-center gap-1">
            <Wifi className="h-3 w-3" />
            Connected
          </Badge>
        )
      case 'disconnected':
      case 'failed':
      case 'closed':
        return (
          <Badge variant="secondary" className="flex items-center gap-1">
            <WifiOff className="h-3 w-3" />
            Disconnected
          </Badge>
        )
      default:
        return null
    }
  }

  const renderPlaceholder = () => {
    if (isConnected) return null

    return (
      <div className="absolute inset-0 flex flex-col items-center justify-center bg-gray-900/90 text-white">
        {connectionState === 'connecting' || signalingStatus === 'connecting' ? (
          <>
            <Loader2 className="h-8 w-8 animate-spin mb-3" />
            <p className="text-sm text-gray-400">Connecting to desktop stream...</p>
          </>
        ) : error ? (
          <>
            <WifiOff className="h-8 w-8 mb-3 text-red-400" />
            <p className="text-sm text-red-400 mb-3">{error.message}</p>
            <Button size="sm" variant="secondary" onClick={reconnect}>
              <RefreshCw className="h-4 w-4 mr-2" />
              Reconnect
            </Button>
          </>
        ) : (
          <>
            <WifiOff className="h-8 w-8 mb-3 text-gray-400" />
            <p className="text-sm text-gray-400 mb-3">Stream disconnected</p>
            <Button size="sm" variant="secondary" onClick={reconnect}>
              <RefreshCw className="h-4 w-4 mr-2" />
              Reconnect
            </Button>
          </>
        )}
      </div>
    )
  }

  return (
    <div
      ref={containerRef}
      className={`relative bg-black rounded-lg overflow-hidden ${className}`}
    >
      {/* Video element */}
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted={isMuted}
        className="w-full h-full object-contain"
        style={{ cursor: inputEnabled ? 'none' : 'default' }}
      />

      {/* Placeholder / loading state */}
      {renderPlaceholder()}

      {/* Controls overlay */}
      {showControls && (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 to-transparent p-4">
          <div className="flex items-center justify-between">
            {/* Left side - Status and info */}
            <div className="flex items-center gap-3">
              {getStatusBadge()}
              {isConnected && videoSize.width > 0 && (
                <span className="text-xs text-gray-400">
                  {videoSize.width}x{videoSize.height}
                </span>
              )}
            </div>

            {/* Right side - Controls */}
            <div className="flex items-center gap-2">
              {/* Input toggle */}
              <Button
                size="sm"
                variant={inputEnabled ? 'secondary' : 'ghost'}
                onClick={toggleInput}
                title={inputEnabled ? 'Disable input' : 'Enable input'}
              >
                {inputEnabled ? (
                  <MousePointer className="h-4 w-4" />
                ) : (
                  <Keyboard className="h-4 w-4" />
                )}
              </Button>

              {/* Play/Pause */}
              <Button
                size="sm"
                variant="ghost"
                onClick={togglePause}
                disabled={!isConnected}
                title={isPaused ? 'Play' : 'Pause'}
              >
                {isPaused ? (
                  <Play className="h-4 w-4" />
                ) : (
                  <Pause className="h-4 w-4" />
                )}
              </Button>

              {/* Mute/Unmute */}
              <Button
                size="sm"
                variant="ghost"
                onClick={toggleMute}
                disabled={!isConnected}
                title={isMuted ? 'Unmute' : 'Mute'}
              >
                {isMuted ? (
                  <VolumeX className="h-4 w-4" />
                ) : (
                  <Volume2 className="h-4 w-4" />
                )}
              </Button>

              {/* Reconnect */}
              <Button
                size="sm"
                variant="ghost"
                onClick={reconnect}
                title="Reconnect"
              >
                <RefreshCw className="h-4 w-4" />
              </Button>

              {/* Fullscreen */}
              <Button
                size="sm"
                variant="ghost"
                onClick={toggleFullscreen}
                title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
              >
                {isFullscreen ? (
                  <Minimize2 className="h-4 w-4" />
                ) : (
                  <Maximize2 className="h-4 w-4" />
                )}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// Export connection state utilities
export function getConnectionStateColor(state: ConnectionState): string {
  switch (state) {
    case 'connected':
      return 'text-green-500'
    case 'connecting':
      return 'text-yellow-500'
    case 'disconnected':
    case 'closed':
      return 'text-gray-500'
    case 'failed':
      return 'text-red-500'
    default:
      return 'text-gray-500'
  }
}

export function getConnectionStateLabel(state: ConnectionState): string {
  switch (state) {
    case 'new':
      return 'Initializing'
    case 'connecting':
      return 'Connecting'
    case 'connected':
      return 'Connected'
    case 'disconnected':
      return 'Disconnected'
    case 'failed':
      return 'Failed'
    case 'closed':
      return 'Closed'
    default:
      return 'Unknown'
  }
}
