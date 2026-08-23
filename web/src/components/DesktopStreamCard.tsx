import { useState } from 'react'
import { Monitor, Play, Square, Loader2, Maximize2, AlertCircle } from 'lucide-react'
import { Card, CardHeader, CardBody } from '@/components/Card'
import { Button } from '@/components/Button'
import { Badge } from '@/components/Badge'
import { Dialog } from '@/components/Dialog'
import { DesktopViewer } from '@/components/DesktopViewer'
import {
  useActiveDesktopStream,
  useStartDesktopStream,
  useStopDesktopStream,
} from '@/api/hooks/useStreams'
import type { StreamStatus } from '@/types/stream'

export interface DesktopStreamCardProps {
  sessionId: string
  sessionStatus: string
  runnerId?: string
}

export function DesktopStreamCard({
  sessionId,
  sessionStatus,
  runnerId,
}: DesktopStreamCardProps) {
  const [isViewerOpen, setIsViewerOpen] = useState(false)

  const { data: activeStream, isLoading } = useActiveDesktopStream(sessionId)
  const startStream = useStartDesktopStream()
  const stopStream = useStopDesktopStream()

  const canStartStream = sessionStatus === 'active' && !!runnerId && !activeStream
  const canStopStream = !!activeStream && activeStream.status !== 'stopping'

  const handleStartStream = async () => {
    try {
      await startStream.mutateAsync({
        sessionId,
        runnerId,
        config: {
          config: {
            width: 1920,
            height: 1080,
            frame_rate: 30,
            input_enabled: true,
          },
        },
      })
    } catch (error) {
      console.error('Failed to start stream:', error)
    }
  }

  const handleStopStream = async () => {
    if (!activeStream) return
    try {
      await stopStream.mutateAsync(activeStream.id)
    } catch (error) {
      console.error('Failed to stop stream:', error)
    }
  }

  const renderStreamStatus = () => {
    if (isLoading) {
      return (
        <Badge variant="default" className="flex items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" />
          Loading
        </Badge>
      )
    }

    if (!activeStream) {
      return <Badge variant="default">No Stream</Badge>
    }

    return <StreamStatusBadge status={activeStream.status} />
  }

  const renderContent = () => {
    if (sessionStatus !== 'active') {
      return (
        <div className="py-8 text-center text-sm text-gray-500">
          <Monitor className="mx-auto h-8 w-8 text-gray-400 mb-2" />
          <p>Desktop streaming is only available for active sessions.</p>
          <p className="text-xs mt-1">Current status: {sessionStatus}</p>
        </div>
      )
    }

    if (!runnerId) {
      return (
        <div className="py-8 text-center text-sm text-gray-500">
          <Monitor className="mx-auto h-8 w-8 text-gray-400 mb-2" />
          <p>No runner attached to this session.</p>
          <p className="text-xs mt-1">Attach a runner to enable streaming.</p>
        </div>
      )
    }

    if (activeStream) {
      return (
        <div className="space-y-4">
          {/* Stream Info */}
          <dl className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <dt className="font-medium text-gray-500">Stream ID</dt>
              <dd className="mt-1 font-mono text-xs truncate">{activeStream.id}</dd>
            </div>
            <div>
              <dt className="font-medium text-gray-500">Provider</dt>
              <dd className="mt-1">{activeStream.provider || 'Unknown'}</dd>
            </div>
            {activeStream.config && (
              <>
                <div>
                  <dt className="font-medium text-gray-500">Resolution</dt>
                  <dd className="mt-1">
                    {activeStream.config.width}x{activeStream.config.height}
                  </dd>
                </div>
                <div>
                  <dt className="font-medium text-gray-500">Frame Rate</dt>
                  <dd className="mt-1">{activeStream.config.frame_rate} fps</dd>
                </div>
              </>
            )}
          </dl>

          {/* Preview (mini viewer) */}
          {activeStream.status === 'active' && (
            <div className="relative aspect-video bg-gray-900 rounded-lg overflow-hidden">
              <DesktopViewer
                streamId={activeStream.id}
                showControls={false}
                className="w-full h-full"
              />
              <button
                onClick={() => setIsViewerOpen(true)}
                className="absolute inset-0 flex items-center justify-center bg-black/30 opacity-0 hover:opacity-100 transition-opacity"
              >
                <Maximize2 className="h-8 w-8 text-white" />
              </button>
            </div>
          )}

          {/* Actions */}
          <div className="flex gap-2">
            {activeStream.status === 'active' && (
              <Button variant="secondary" onClick={() => setIsViewerOpen(true)}>
                <Maximize2 className="mr-2 h-4 w-4" />
                Open Viewer
              </Button>
            )}
            <Button
              variant="danger"
              onClick={handleStopStream}
              disabled={!canStopStream || stopStream.isPending}
            >
              {stopStream.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Square className="mr-2 h-4 w-4" />
              )}
              Stop Stream
            </Button>
          </div>
        </div>
      )
    }

    // No active stream - show start button
    return (
      <div className="py-6 text-center">
        <Monitor className="mx-auto h-12 w-12 text-gray-400 mb-3" />
        <p className="text-sm text-gray-500 mb-4">
          Start a desktop stream to view and interact with the runner's desktop.
        </p>
        <Button
          onClick={handleStartStream}
          disabled={!canStartStream || startStream.isPending}
        >
          {startStream.isPending ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <Play className="mr-2 h-4 w-4" />
          )}
          Start Desktop Stream
        </Button>
        {startStream.isError && (
          <p className="mt-2 text-sm text-red-600 flex items-center justify-center gap-1">
            <AlertCircle className="h-4 w-4" />
            Failed to start stream
          </p>
        )}
      </div>
    )
  }

  return (
    <>
      <Card>
        <CardHeader
          icon={<Monitor className="h-4 w-4" />}
          action={renderStreamStatus()}
        >
          Desktop Stream
        </CardHeader>
        <CardBody>{renderContent()}</CardBody>
      </Card>

      {/* Full-screen viewer dialog */}
      <Dialog open={isViewerOpen} onClose={() => setIsViewerOpen(false)}>
        {activeStream && (
          <div className="h-[calc(100vh-120px)]">
            <DesktopViewer
              streamId={activeStream.id}
              showControls={true}
              className="w-full h-full"
              onError={(error) => console.error('Viewer error:', error)}
            />
          </div>
        )}
      </Dialog>
    </>
  )
}

function StreamStatusBadge({ status }: { status: StreamStatus }) {
  switch (status) {
    case 'active':
      return (
        <Badge variant="success" className="flex items-center gap-1">
          <Play className="h-3 w-3" />
          Active
        </Badge>
      )
    case 'starting':
    case 'pending':
      return (
        <Badge variant="warning" className="flex items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" />
          Starting
        </Badge>
      )
    case 'stopping':
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" />
          Stopping
        </Badge>
      )
    case 'stopped':
      return (
        <Badge variant="default" className="flex items-center gap-1">
          <Square className="h-3 w-3" />
          Stopped
        </Badge>
      )
    case 'error':
      return (
        <Badge variant="danger" className="flex items-center gap-1">
          <AlertCircle className="h-3 w-3" />
          Error
        </Badge>
      )
    default:
      return <Badge>{status}</Badge>
  }
}
