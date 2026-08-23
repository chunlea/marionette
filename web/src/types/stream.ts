// Types for the FROZEN streaming subsystem (decision D1): desktop streaming
// over WebRTC, and Android device mirroring.
//
// These are hand-written and stay hand-written: the desktop stream endpoints
// live on the admin API, which has no generated spec, and the Android
// endpoints have no server implementation at all. Nothing here is part of the
// public contract in ./api, and the UI that uses it is gated behind
// VITE_ENABLE_STREAMING (see ../lib/features).

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

// ---------------------------------------------------------------------------
// Android device mirroring
//
// No server route implements any of this yet — the handlers these hooks call
// exist nowhere in pkg/server. The UI stays compiled but unreachable until the
// streaming subsystem is unfrozen.
// ---------------------------------------------------------------------------

export type AndroidStreamState = 'starting' | 'active' | 'paused' | 'closing' | 'closed' | 'failed'

export interface AndroidStream {
  id: string
  session_id: string
  runner_id?: string
  device_serial: string
  state: AndroidStreamState
  width?: number
  height?: number
  video_codec?: string
  audio_codec?: string
  error_message?: string
  created_at: string
  started_at?: string
  closed_at?: string
}

export interface AndroidStreamList {
  items: AndroidStream[]
  next_cursor?: string
}

export interface CreateAndroidStreamRequest {
  device_serial: string
  max_width?: number
  max_height?: number
  max_fps?: number
  bitrate?: number
  audio_enabled?: boolean
}

export interface AndroidDevice {
  serial: string
  state: string
  product?: string
  model?: string
  device?: string
}

export interface AndroidDeviceList {
  items: AndroidDevice[]
}

export interface AndroidInputEvent {
  type: 'touch' | 'key' | 'text'
  action?: 'down' | 'up' | 'move'
  x?: number
  y?: number
  key_code?: number
  key_action?: 'down' | 'up'
  text?: string
}

export interface AndroidStreamsQueryParams {
  limit?: number
  cursor?: string
  state?: AndroidStreamState[]
  include_closed?: boolean
}
