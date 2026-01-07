// Stream-related types for desktop streaming

export type StreamStatus = 'pending' | 'starting' | 'active' | 'stopping' | 'stopped' | 'error'

export interface StreamConfig {
  width?: number
  height?: number
  frame_rate?: number
  bitrate?: number
  video_codec?: string
  audio_enabled?: boolean
  input_enabled?: boolean
  display?: string
  hw_accel?: string
}

export interface Stream {
  id: string
  session_id: string
  runner_id?: string
  status: StreamStatus
  signaling_url?: string
  config?: StreamConfig
  provider?: string
  started_at?: string
  stopped_at?: string
}

export interface StartStreamRequest {
  config?: StreamConfig
}

export interface StreamList {
  items: Stream[]
  total_count: number
}

// WebRTC Signaling message types
export type SignalingMessageType = 'offer' | 'answer' | 'candidate' | 'error'

export interface ICECandidateJSON {
  candidate: string
  sdpMid?: string
  sdpMLineIndex?: number
  usernameFragment?: string
}

export interface SignalingMessage {
  type: SignalingMessageType
  stream_id?: string
  peer_id?: string
  sdp?: string
  candidate?: ICECandidateJSON
  error?: string
}

// Connection states
export type ConnectionState =
  | 'new'
  | 'connecting'
  | 'connected'
  | 'disconnected'
  | 'failed'
  | 'closed'

// Input event types for forwarding
export type InputEventType = 'keydown' | 'keyup' | 'mousemove' | 'mousedown' | 'mouseup' | 'wheel'

export interface KeyboardInputEvent {
  type: 'keydown' | 'keyup'
  key: string
  code: string
  modifiers: {
    ctrl: boolean
    alt: boolean
    shift: boolean
    meta: boolean
  }
}

export interface MouseInputEvent {
  type: 'mousemove' | 'mousedown' | 'mouseup'
  x: number
  y: number
  button?: number
}

export interface WheelInputEvent {
  type: 'wheel'
  x: number
  y: number
  deltaX: number
  deltaY: number
}

export type InputEvent = KeyboardInputEvent | MouseInputEvent | WheelInputEvent
