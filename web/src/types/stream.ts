// Types for the FROZEN streaming subsystem (decision D1): desktop streaming
// over WebRTC, and Android device mirroring.
//
// The desktop stream shapes are derived from the admin OpenAPI document now
// that the admin API has one. That immediately corrected a drift nobody had
// noticed: the hand-written Stream declared `status`, the server has always
// sent `state`, so every check against it read undefined — the viewer never
// polled while a stream was starting, and useActiveDesktopStream could never
// find one. The UI is gated behind VITE_ENABLE_STREAMING (see ../lib/features)
// and this was one more reason it could not have worked if it were not.
//
// The Android types below stay hand-written: no server route implements them.

import type { components } from './admin.gen'

type Schemas = components['schemas']

/** A desktop stream, as the admin API describes it. */
export type Stream = Schemas['StreamResponse']

/** The lifecycle state of a stream. The field is `state`, not `status`. */
export type StreamState = Stream['state']

export type StreamList = Schemas['StreamResponseList']
export type StartStreamRequest = Schemas['StreamRequest']

/** The settings half of StreamRequest: what a caller chooses, without the
 *  session, runner and type the hook fills in. */
export type StreamSettings = Omit<StartStreamRequest, 'session_id' | 'runner_id' | 'type'>
export type StreamResolution = Schemas['ResolutionRequest']

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
