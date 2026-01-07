import { useCallback, useEffect, useRef, useState } from 'react'
import { getApiKey } from '@/api/client'

// WebRTC signaling message types
interface SignalingMessage {
  type: 'offer' | 'answer' | 'ice-candidate' | 'stream-info' | 'error'
  sdp?: string
  candidate?: RTCIceCandidateInit
  width?: number
  height?: number
  video_codec?: string
  audio_codec?: string
  has_audio?: boolean
  error?: string
}

interface StreamInfo {
  width: number
  height: number
  videoCodec: string
  audioCodec: string
  hasAudio: boolean
}

interface UseAndroidWebRTCOptions {
  streamId: string
  onStreamInfo?: (info: StreamInfo) => void
  onError?: (error: string) => void
}

interface UseAndroidWebRTCReturn {
  videoRef: React.RefObject<HTMLVideoElement | null>
  isConnecting: boolean
  isConnected: boolean
  error: string | null
  streamInfo: StreamInfo | null
  connect: () => void
  disconnect: () => void
}

export function useAndroidWebRTC(options: UseAndroidWebRTCOptions): UseAndroidWebRTCReturn {
  const { streamId, onStreamInfo, onError } = options

  const videoRef = useRef<HTMLVideoElement | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const pcRef = useRef<RTCPeerConnection | null>(null)

  const [isConnecting, setIsConnecting] = useState(false)
  const [isConnected, setIsConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [streamInfo, setStreamInfo] = useState<StreamInfo | null>(null)

  // ICE servers configuration
  const iceServers: RTCIceServer[] = [
    { urls: 'stun:stun.l.google.com:19302' },
    { urls: 'stun:stun1.l.google.com:19302' },
  ]

  const cleanup = useCallback(() => {
    if (pcRef.current) {
      pcRef.current.close()
      pcRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    setIsConnected(false)
    setIsConnecting(false)
  }, [])

  const sendSignalingMessage = useCallback((message: SignalingMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(message))
    }
  }, [])

  const handleSignalingMessage = useCallback(
    async (message: SignalingMessage) => {
      const pc = pcRef.current
      if (!pc) return

      switch (message.type) {
        case 'stream-info':
          if (message.width && message.height) {
            const info: StreamInfo = {
              width: message.width,
              height: message.height,
              videoCodec: message.video_codec || 'h264',
              audioCodec: message.audio_codec || '',
              hasAudio: message.has_audio || false,
            }
            setStreamInfo(info)
            onStreamInfo?.(info)
          }
          break

        case 'answer':
          if (message.sdp) {
            await pc.setRemoteDescription({
              type: 'answer',
              sdp: message.sdp,
            })
          }
          break

        case 'ice-candidate':
          if (message.candidate) {
            await pc.addIceCandidate(new RTCIceCandidate(message.candidate))
          }
          break

        case 'error':
          const errorMsg = message.error || 'Unknown signaling error'
          setError(errorMsg)
          onError?.(errorMsg)
          break
      }
    },
    [onStreamInfo, onError]
  )

  const connect = useCallback(async () => {
    if (isConnecting || isConnected) return

    setIsConnecting(true)
    setError(null)

    try {
      // Build WebSocket URL
      const apiKey = getApiKey()
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const host = import.meta.env.VITE_API_URL
        ? new URL(import.meta.env.VITE_API_URL).host
        : window.location.host
      const wsUrl = `${protocol}//${host}/api/v1/android/streams/${streamId}/signal`

      // Create WebSocket connection
      const ws = new WebSocket(wsUrl, apiKey ? [apiKey] : undefined)
      wsRef.current = ws

      ws.onopen = async () => {
        // Create peer connection
        const pc = new RTCPeerConnection({ iceServers })
        pcRef.current = pc

        // Handle incoming tracks
        pc.ontrack = (event) => {
          if (videoRef.current && event.streams[0]) {
            videoRef.current.srcObject = event.streams[0]
          }
        }

        // Handle ICE candidates
        pc.onicecandidate = (event) => {
          if (event.candidate) {
            sendSignalingMessage({
              type: 'ice-candidate',
              candidate: event.candidate.toJSON(),
            })
          }
        }

        // Handle connection state changes
        pc.onconnectionstatechange = () => {
          switch (pc.connectionState) {
            case 'connected':
              setIsConnecting(false)
              setIsConnected(true)
              break
            case 'disconnected':
            case 'failed':
              setIsConnected(false)
              setError('Connection lost')
              break
            case 'closed':
              setIsConnected(false)
              break
          }
        }

        // Add transceivers for receiving media
        pc.addTransceiver('video', { direction: 'recvonly' })
        pc.addTransceiver('audio', { direction: 'recvonly' })

        // Create and send offer
        const offer = await pc.createOffer()
        await pc.setLocalDescription(offer)

        sendSignalingMessage({
          type: 'offer',
          sdp: offer.sdp,
        })
      }

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as SignalingMessage
          handleSignalingMessage(message)
        } catch {
          console.error('Failed to parse signaling message')
        }
      }

      ws.onerror = () => {
        setError('WebSocket connection error')
        setIsConnecting(false)
      }

      ws.onclose = () => {
        if (isConnecting) {
          setError('WebSocket connection closed')
          setIsConnecting(false)
        }
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to connect'
      setError(errorMsg)
      onError?.(errorMsg)
      setIsConnecting(false)
    }
  }, [streamId, isConnecting, isConnected, handleSignalingMessage, sendSignalingMessage, onError])

  const disconnect = useCallback(() => {
    cleanup()
    setStreamInfo(null)
    setError(null)
  }, [cleanup])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      cleanup()
    }
  }, [cleanup])

  return {
    videoRef,
    isConnecting,
    isConnected,
    error,
    streamInfo,
    connect,
    disconnect,
  }
}
