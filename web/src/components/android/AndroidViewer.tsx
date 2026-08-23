import { useCallback, useEffect, useState } from 'react'
import { useAndroidWebRTC } from '@/hooks/useAndroidWebRTC'
import { useAndroidInput } from '@/hooks/useAndroidInput'
import { useSendAndroidInput } from '@/api/hooks/useAndroidStreams'
import { AndroidControls } from './AndroidControls'
import type { AndroidInputEvent, AndroidStream } from '@/types/stream'

interface AndroidViewerProps {
  sessionId: string
  stream: AndroidStream
  className?: string
}

export function AndroidViewer({ sessionId, stream, className = '' }: AndroidViewerProps) {
  const [deviceDimensions, setDeviceDimensions] = useState({
    width: stream.width || 1080,
    height: stream.height || 1920,
  })

  const sendInputMutation = useSendAndroidInput(sessionId, stream.id)

  const handleInput = useCallback(
    (event: AndroidInputEvent) => {
      sendInputMutation.mutate(event)
    },
    [sendInputMutation]
  )

  const {
    videoRef,
    isConnecting,
    isConnected,
    error,
    streamInfo,
    connect,
    disconnect,
  } = useAndroidWebRTC({
    streamId: stream.id,
    onStreamInfo: (info) => {
      setDeviceDimensions({
        width: info.width,
        height: info.height,
      })
    },
    onError: (err) => {
      console.error('WebRTC error:', err)
    },
  })

  const {
    handleTouchStart,
    handleTouchMove,
    handleTouchEnd,
    handleKeyDown,
    handleKeyUp,
    sendBack,
    sendHome,
    sendRecent,
  } = useAndroidInput({
    deviceWidth: deviceDimensions.width,
    deviceHeight: deviceDimensions.height,
    onInput: handleInput,
  })

  // Auto-connect when stream is active
  useEffect(() => {
    if (stream.state === 'active' && !isConnected && !isConnecting) {
      connect()
    }
    return () => {
      if (isConnected) {
        disconnect()
      }
    }
  }, [stream.state, isConnected, isConnecting, connect, disconnect])

  // Update dimensions when stream info changes
  useEffect(() => {
    if (stream.width && stream.height) {
      setDeviceDimensions({
        width: stream.width,
        height: stream.height,
      })
    }
  }, [stream.width, stream.height])

  // Calculate aspect ratio for responsive sizing
  const aspectRatio = deviceDimensions.height / deviceDimensions.width

  return (
    <div className={`flex flex-col items-center gap-4 ${className}`}>
      {/* Connection status */}
      <div className="flex items-center gap-2 text-sm">
        {isConnecting && (
          <span className="text-yellow-500">Connecting...</span>
        )}
        {isConnected && (
          <span className="text-green-500">Connected</span>
        )}
        {error && (
          <span className="text-red-500">{error}</span>
        )}
        {streamInfo && (
          <span className="text-gray-400">
            {streamInfo.width}x{streamInfo.height} ({streamInfo.videoCodec})
          </span>
        )}
      </div>

      {/* Video container */}
      <div
        className="relative bg-black rounded-lg overflow-hidden"
        style={{
          maxWidth: '100%',
          aspectRatio: `1 / ${aspectRatio}`,
        }}
      >
        <video
          ref={videoRef as React.RefObject<HTMLVideoElement>}
          autoPlay
          playsInline
          muted={false}
          className="w-full h-full object-contain cursor-pointer"
          tabIndex={0}
          onTouchStart={handleTouchStart}
          onTouchMove={handleTouchMove}
          onTouchEnd={handleTouchEnd}
          onMouseDown={handleTouchStart}
          onMouseMove={handleTouchMove}
          onMouseUp={handleTouchEnd}
          onMouseLeave={handleTouchEnd}
          onKeyDown={handleKeyDown}
          onKeyUp={handleKeyUp}
        />

        {/* Loading overlay */}
        {!isConnected && stream.state === 'active' && (
          <div className="absolute inset-0 flex items-center justify-center bg-gray-900/80">
            <div className="text-white text-center">
              {isConnecting ? (
                <div className="flex flex-col items-center gap-2">
                  <div className="w-8 h-8 border-2 border-white border-t-transparent rounded-full animate-spin" />
                  <span>Connecting to stream...</span>
                </div>
              ) : error ? (
                <div className="flex flex-col items-center gap-2">
                  <span className="text-red-400">{error}</span>
                  <button
                    onClick={connect}
                    className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-sm"
                  >
                    Retry
                  </button>
                </div>
              ) : (
                <span>Waiting for stream...</span>
              )}
            </div>
          </div>
        )}

        {/* Stream not active overlay */}
        {stream.state !== 'active' && (
          <div className="absolute inset-0 flex items-center justify-center bg-gray-900/80">
            <div className="text-white text-center">
              {stream.state === 'starting' && (
                <div className="flex flex-col items-center gap-2">
                  <div className="w-8 h-8 border-2 border-white border-t-transparent rounded-full animate-spin" />
                  <span>Starting stream...</span>
                </div>
              )}
              {stream.state === 'failed' && (
                <div className="flex flex-col items-center gap-2">
                  <span className="text-red-400">Stream failed</span>
                  {stream.error_message && (
                    <span className="text-sm text-gray-400">{stream.error_message}</span>
                  )}
                </div>
              )}
              {stream.state === 'closed' && (
                <span className="text-gray-400">Stream closed</span>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Controls */}
      <AndroidControls
        onBack={sendBack}
        onHome={sendHome}
        onRecent={sendRecent}
        disabled={!isConnected}
      />
    </div>
  )
}
