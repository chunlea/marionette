import { useState, useEffect, useCallback, useRef } from 'react'
import { WebSocketClient, WebSocketStatus, buildWebSocketUrl } from '@/lib/websocket'
import { getApiKey } from '@/api/client'
import type { SignalingMessage, ConnectionState } from '@/types/stream'

export interface UseWebRTCStreamOptions {
  streamId: string
  enabled?: boolean
  onConnected?: () => void
  onDisconnected?: () => void
  onError?: (error: Error) => void
  onTrack?: (track: MediaStreamTrack, streams: readonly MediaStream[]) => void
}

export interface UseWebRTCStreamResult {
  peerConnection: RTCPeerConnection | null
  dataChannel: RTCDataChannel | null
  connectionState: ConnectionState
  signalingStatus: WebSocketStatus
  isConnected: boolean
  error: Error | null
  connect: () => void
  disconnect: () => void
  reconnect: () => void
}

const defaultIceServers: RTCIceServer[] = [
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:stun1.l.google.com:19302' },
]

export function useWebRTCStream({
  streamId,
  enabled = true,
  onConnected,
  onDisconnected,
  onError,
  onTrack,
}: UseWebRTCStreamOptions): UseWebRTCStreamResult {
  const [connectionState, setConnectionState] = useState<ConnectionState>('new')
  const [signalingStatus, setSignalingStatus] = useState<WebSocketStatus>('disconnected')
  const [error, setError] = useState<Error | null>(null)

  const peerConnectionRef = useRef<RTCPeerConnection | null>(null)
  const dataChannelRef = useRef<RTCDataChannel | null>(null)
  const wsClientRef = useRef<WebSocketClient | null>(null)
  const pendingCandidatesRef = useRef<RTCIceCandidateInit[]>([])

  // Handle signaling messages
  const handleSignalingMessage = useCallback(
    async (data: unknown) => {
      const msg = data as SignalingMessage
      const pc = peerConnectionRef.current

      if (!pc) {
        console.warn('Received signaling message but no peer connection')
        return
      }

      try {
        switch (msg.type) {
          case 'offer':
            // Server sends offer for subscriber (browser)
            if (msg.sdp) {
              await pc.setRemoteDescription({
                type: 'offer',
                sdp: msg.sdp,
              })

              // Add any pending ICE candidates
              for (const candidate of pendingCandidatesRef.current) {
                await pc.addIceCandidate(candidate)
              }
              pendingCandidatesRef.current = []

              // Create and send answer
              const answer = await pc.createAnswer()
              await pc.setLocalDescription(answer)

              wsClientRef.current?.send({
                type: 'answer',
                sdp: answer.sdp,
              } as SignalingMessage)
            }
            break

          case 'candidate':
            // ICE candidate from server
            if (msg.candidate) {
              const candidate: RTCIceCandidateInit = {
                candidate: msg.candidate.candidate,
                sdpMid: msg.candidate.sdpMid,
                sdpMLineIndex: msg.candidate.sdpMLineIndex,
              }

              if (pc.remoteDescription) {
                await pc.addIceCandidate(candidate)
              } else {
                // Queue if remote description not set yet
                pendingCandidatesRef.current.push(candidate)
              }
            }
            break

          case 'error':
            console.error('Signaling error:', msg.error)
            setError(new Error(msg.error ?? 'Signaling error'))
            onError?.(new Error(msg.error ?? 'Signaling error'))
            break
        }
      } catch (err) {
        console.error('Error handling signaling message:', err)
        setError(err instanceof Error ? err : new Error('Signaling error'))
        onError?.(err instanceof Error ? err : new Error('Signaling error'))
      }
    },
    [onError]
  )

  // Create peer connection
  const createPeerConnection = useCallback(() => {
    const config: RTCConfiguration = {
      iceServers: defaultIceServers,
      iceTransportPolicy: 'all',
    }

    const pc = new RTCPeerConnection(config)

    // Handle connection state changes
    pc.onconnectionstatechange = () => {
      const state = pc.connectionState as ConnectionState
      setConnectionState(state)

      if (state === 'connected') {
        onConnected?.()
      } else if (state === 'disconnected' || state === 'failed' || state === 'closed') {
        onDisconnected?.()
      }
    }

    // Handle ICE connection state
    pc.oniceconnectionstatechange = () => {
      console.log('ICE connection state:', pc.iceConnectionState)
    }

    // Handle ICE candidates
    pc.onicecandidate = (event) => {
      if (event.candidate) {
        wsClientRef.current?.send({
          type: 'candidate',
          candidate: {
            candidate: event.candidate.candidate,
            sdpMid: event.candidate.sdpMid ?? undefined,
            sdpMLineIndex: event.candidate.sdpMLineIndex ?? undefined,
          },
        } as SignalingMessage)
      }
    }

    // Handle incoming tracks
    pc.ontrack = (event) => {
      console.log('Received track:', event.track.kind)
      onTrack?.(event.track, event.streams)
    }

    // Handle data channel (for input forwarding)
    pc.ondatachannel = (event) => {
      console.log('Received data channel:', event.channel.label)
      dataChannelRef.current = event.channel
    }

    peerConnectionRef.current = pc
    return pc
  }, [onConnected, onDisconnected, onTrack])

  // Connect to the stream
  const connect = useCallback(() => {
    if (!streamId || !enabled) return

    const apiKey = getApiKey()
    if (!apiKey) {
      setError(new Error('API key not configured'))
      return
    }

    // Create peer connection
    createPeerConnection()

    // Connect to signaling server
    const url = buildWebSocketUrl(
      `/admin/api/v1/streams/${streamId}/signaling?token=${encodeURIComponent(apiKey)}`
    )

    wsClientRef.current = new WebSocketClient({
      url,
      onMessage: handleSignalingMessage,
      onStatusChange: (status) => {
        setSignalingStatus(status)
        if (status === 'error') {
          setError(new Error('Signaling connection failed'))
          onError?.(new Error('Signaling connection failed'))
        }
      },
    })

    wsClientRef.current.connect()
    setConnectionState('connecting')
    setError(null)
  }, [streamId, enabled, createPeerConnection, handleSignalingMessage, onError])

  // Disconnect from the stream
  const disconnect = useCallback(() => {
    // Close WebSocket
    wsClientRef.current?.disconnect()
    wsClientRef.current = null

    // Close data channel
    if (dataChannelRef.current) {
      dataChannelRef.current.close()
      dataChannelRef.current = null
    }

    // Close peer connection
    if (peerConnectionRef.current) {
      peerConnectionRef.current.close()
      peerConnectionRef.current = null
    }

    pendingCandidatesRef.current = []
    setConnectionState('closed')
    setSignalingStatus('disconnected')
  }, [])

  // Reconnect
  const reconnect = useCallback(() => {
    disconnect()
    // Small delay before reconnecting
    setTimeout(() => {
      connect()
    }, 500)
  }, [connect, disconnect])

  // Auto-connect on mount if enabled
  useEffect(() => {
    if (enabled && streamId) {
      connect()
    }
    return () => {
      disconnect()
    }
  }, [enabled, streamId]) // eslint-disable-line react-hooks/exhaustive-deps

  return {
    peerConnection: peerConnectionRef.current,
    dataChannel: dataChannelRef.current,
    connectionState,
    signalingStatus,
    isConnected: connectionState === 'connected',
    error,
    connect,
    disconnect,
    reconnect,
  }
}
