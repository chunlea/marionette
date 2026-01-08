import { Loader2, WifiOff, AlertCircle, Monitor } from 'lucide-react'
import { Button } from '@/components/Button'
import type { StreamConnectionStatus } from './types'

interface StreamOverlayProps {
  status: StreamConnectionStatus
  hasFrame: boolean
  onReconnect?: () => void
  errorMessage?: string
}

export function StreamOverlay({
  status,
  hasFrame,
  onReconnect,
  errorMessage,
}: StreamOverlayProps) {
  // Don't show overlay if connected and has frames
  if (status === 'connected' && hasFrame) {
    return null
  }

  return (
    <div className="absolute inset-0 flex items-center justify-center bg-gray-900/90">
      <div className="text-center">
        <OverlayContent
          status={status}
          hasFrame={hasFrame}
          onReconnect={onReconnect}
          errorMessage={errorMessage}
        />
      </div>
    </div>
  )
}

function OverlayContent({
  status,
  hasFrame,
  onReconnect,
  errorMessage,
}: StreamOverlayProps) {
  switch (status) {
    case 'connecting':
      return (
        <>
          <Loader2 className="mx-auto h-12 w-12 animate-spin text-blue-500" />
          <p className="mt-4 text-lg text-gray-300">Connecting to stream...</p>
          <p className="mt-2 text-sm text-gray-500">
            Please wait while we establish a connection
          </p>
        </>
      )

    case 'connected':
      // Connected but no frame yet
      if (!hasFrame) {
        return (
          <>
            <Monitor className="mx-auto h-12 w-12 text-gray-500" />
            <p className="mt-4 text-lg text-gray-300">Waiting for frames...</p>
            <p className="mt-2 text-sm text-gray-500">
              The browser is starting up
            </p>
          </>
        )
      }
      return null

    case 'error':
      return (
        <>
          <AlertCircle className="mx-auto h-12 w-12 text-red-500" />
          <p className="mt-4 text-lg text-gray-300">Connection Error</p>
          <p className="mt-2 text-sm text-gray-500">
            {errorMessage || 'Failed to connect to the stream'}
          </p>
          {onReconnect && (
            <Button onClick={onReconnect} className="mt-4">
              Try Again
            </Button>
          )}
        </>
      )

    case 'disconnected':
    default:
      return (
        <>
          <WifiOff className="mx-auto h-12 w-12 text-gray-500" />
          <p className="mt-4 text-lg text-gray-300">Disconnected</p>
          <p className="mt-2 text-sm text-gray-500">
            The stream connection was lost
          </p>
          {onReconnect && (
            <Button onClick={onReconnect} className="mt-4">
              Reconnect
            </Button>
          )}
        </>
      )
  }
}

export default StreamOverlay
