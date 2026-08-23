import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useSession } from '@/api/hooks/useSessions'
import {
  useAndroidStreams,
  useAndroidStream,
  useAndroidDevices,
  useStartAndroidStream,
  useStopAndroidStream,
} from '@/api/hooks/useAndroidStreams'
import { AndroidViewer, DeviceSelector } from '@/components/android'
import { Button } from '@/components/Button'
import { Card, CardHeader, CardBody } from '@/components/Card'
import type { AndroidDevice } from '@/types/stream'
import { streamingEnabled } from '@/lib/features'

export const Route = createFileRoute('/_layout/sessions/$sessionId/android')({
  component: AndroidRoute,
})

// Android device mirroring is part of the frozen streaming subsystem
// (decision D1) and no server route implements it yet, so the page is only
// reachable when streaming is explicitly enabled.
function AndroidRoute() {
  if (!streamingEnabled) {
    return <StreamingDisabled />
  }
  return <AndroidPage />
}

function StreamingDisabled() {
  return (
    <Card>
      <CardHeader>Android streaming is disabled</CardHeader>
      <CardBody>
        <p className="text-sm text-gray-600">
          Device mirroring is part of the frozen streaming subsystem. Rebuild with
          <code className="mx-1 rounded bg-gray-100 px-1">VITE_ENABLE_STREAMING=true</code>
          to enable it.
        </p>
      </CardBody>
    </Card>
  )
}

function AndroidPage() {
  const { sessionId } = Route.useParams()
  const [selectedDevice, setSelectedDevice] = useState<AndroidDevice | null>(null)
  const [activeStreamId, setActiveStreamId] = useState<string | null>(null)

  const { data: session, isLoading: sessionLoading } = useSession(sessionId)
  const { data: streamsData } = useAndroidStreams(sessionId)
  const { data: devicesData, isLoading: devicesLoading } = useAndroidDevices(sessionId)
  const { data: activeStream } = useAndroidStream(sessionId, activeStreamId || '')

  const startStreamMutation = useStartAndroidStream(sessionId)
  const stopStreamMutation = useStopAndroidStream(sessionId)

  // Find active stream if any
  const streams = streamsData?.items || []
  const activeStreams = streams.filter(
    (s) => s.state === 'active' || s.state === 'starting'
  )
  const devices = devicesData?.items || []

  const handleStartStream = async () => {
    if (!selectedDevice) return

    try {
      const stream = await startStreamMutation.mutateAsync({
        device_serial: selectedDevice.serial,
        max_width: 1080,
        max_height: 1920,
        max_fps: 30,
        bitrate: 8000000,
        audio_enabled: true,
      })
      setActiveStreamId(stream.id)
    } catch (error) {
      console.error('Failed to start stream:', error)
    }
  }

  const handleStopStream = async (streamId: string) => {
    try {
      await stopStreamMutation.mutateAsync(streamId)
      if (activeStreamId === streamId) {
        setActiveStreamId(null)
      }
    } catch (error) {
      console.error('Failed to stop stream:', error)
    }
  }

  if (sessionLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (!session) {
    return (
      <div className="text-center py-12">
        <h2 className="text-xl font-semibold text-gray-300 mb-2">Session not found</h2>
        <Link to="/sessions" className="text-blue-400 hover:text-blue-300">
          Back to sessions
        </Link>
      </div>
    )
  }

  const canStartStream =
    session.status === 'active' && selectedDevice && !startStreamMutation.isPending

  return (
    <div className="max-w-6xl mx-auto p-4">
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-center gap-2 text-sm text-gray-400 mb-2">
          <Link to="/sessions" className="hover:text-blue-400">
            Sessions
          </Link>
          <span>/</span>
          <Link
            to="/sessions/$sessionId"
            params={{ sessionId }}
            className="hover:text-blue-400"
          >
            {session.name || sessionId}
          </Link>
          <span>/</span>
          <span className="text-gray-300">Android</span>
        </div>
        <h1 className="text-2xl font-bold text-white">Android Screen Streaming</h1>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left panel: Device selection and controls */}
        <div className="lg:col-span-1 space-y-4">
          {/* Device selector */}
          <Card>
            <CardHeader>Device</CardHeader>
            <CardBody>
              <DeviceSelector
                devices={devices}
                selectedSerial={selectedDevice?.serial}
                onSelect={setSelectedDevice}
                isLoading={devicesLoading}
                disabled={activeStreams.length > 0}
              />

              <div className="mt-4">
                {activeStreams.length === 0 ? (
                  <Button
                    onClick={handleStartStream}
                    disabled={!canStartStream}
                    className="w-full"
                  >
                    {startStreamMutation.isPending ? 'Starting...' : 'Start Stream'}
                  </Button>
                ) : (
                  <Button
                    variant="danger"
                    onClick={() => handleStopStream(activeStreams[0].id)}
                    disabled={stopStreamMutation.isPending}
                    className="w-full"
                  >
                    {stopStreamMutation.isPending ? 'Stopping...' : 'Stop Stream'}
                  </Button>
                )}
              </div>

              {session.status !== 'active' && (
                <p className="mt-2 text-sm text-yellow-500">
                  Session must be active to start streaming
                </p>
              )}
            </CardBody>
          </Card>

          {/* Active streams list */}
          {streams.length > 0 && (
            <Card>
              <CardHeader>Streams</CardHeader>
              <CardBody>
                <ul className="space-y-2">
                  {streams.map((stream) => (
                    <li
                      key={stream.id}
                      className={`
                        p-3 rounded-lg cursor-pointer
                        ${activeStreamId === stream.id ? 'bg-blue-900/50 border border-blue-500' : 'bg-gray-800 hover:bg-gray-700'}
                      `}
                      onClick={() => setActiveStreamId(stream.id)}
                    >
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium text-white">
                          {stream.device_serial}
                        </span>
                        <span
                          className={`
                            text-xs px-2 py-1 rounded
                            ${stream.state === 'active' ? 'bg-green-900 text-green-300' : ''}
                            ${stream.state === 'starting' ? 'bg-yellow-900 text-yellow-300' : ''}
                            ${stream.state === 'failed' ? 'bg-red-900 text-red-300' : ''}
                            ${stream.state === 'closed' ? 'bg-gray-700 text-gray-400' : ''}
                          `}
                        >
                          {stream.state}
                        </span>
                      </div>
                      {stream.width && stream.height && (
                        <div className="text-xs text-gray-400 mt-1">
                          {stream.width}x{stream.height}
                          {stream.video_codec && ` - ${stream.video_codec}`}
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
              </CardBody>
            </Card>
          )}

          {/* Session info */}
          <Card>
            <CardHeader>Session</CardHeader>
            <CardBody>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt className="text-gray-400">Status</dt>
                  <dd className="text-white">{session.status}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-gray-400">Agent</dt>
                  <dd className="text-white">{session.agent}</dd>
                </div>
                {session.runner_id && (
                  <div className="flex justify-between">
                    <dt className="text-gray-400">Runner</dt>
                    <dd className="text-white font-mono text-xs">
                      {session.runner_id}
                    </dd>
                  </div>
                )}
              </dl>
            </CardBody>
          </Card>
        </div>

        {/* Right panel: Video viewer */}
        <div className="lg:col-span-2">
          <Card className="h-full">
            <CardBody className="p-4 h-full min-h-[600px] flex items-center justify-center">
              {activeStream && (activeStream.state === 'active' || activeStream.state === 'starting') ? (
                <AndroidViewer
                  sessionId={sessionId}
                  stream={activeStream}
                  className="w-full"
                />
              ) : (
                <div className="text-center text-gray-400">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={1}
                    className="w-16 h-16 mx-auto mb-4 opacity-50"
                  >
                    <rect x="5" y="2" width="14" height="20" rx="2" />
                    <circle cx="12" cy="18" r="1" />
                  </svg>
                  <p className="text-lg mb-2">No active stream</p>
                  <p className="text-sm">
                    Select a device and click "Start Stream" to begin
                  </p>
                </div>
              )}
            </CardBody>
          </Card>
        </div>
      </div>
    </div>
  )
}
