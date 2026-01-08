// Types for browser streaming WebSocket messages

export interface WSFrameMessage {
  type: 'frame'
  data: string // base64 encoded
  format: 'jpeg' | 'png' | 'webp'
  width: number
  height: number
  sequence: number
  timestamp: number // Unix milliseconds
}

export interface WSInputMessage {
  type: 'input'
  event: WSInputEvent
}

export interface WSInputEvent {
  event_type: string // mouseDown, mouseUp, mouseMove, keyDown, keyUp, etc.
  mouse?: WSMouseEvent
  keyboard?: WSKeyboardEvent
  timestamp_ms?: number
}

export interface WSMouseEvent {
  x: number
  y: number
  button?: 'left' | 'right' | 'middle'
  delta_x?: number
  delta_y?: number
}

export interface WSKeyboardEvent {
  key: string
  code: string
  text?: string
}

export interface WSControlMessage {
  type: 'control'
  command: 'pause' | 'resume' | 'navigate' | 'switchTab'
  payload?: Record<string, unknown>
}

export interface WSStatsMessage {
  type: 'stats'
  frames_received: number
  frames_delivered: number
  frames_dropped: number
  current_fps: number
  subscriber_count: number
}

export interface WSStateMessage {
  type: 'state'
  state: string
  message?: string
}

export type WSMessage =
  | WSFrameMessage
  | WSStatsMessage
  | WSStateMessage

export interface StreamStats {
  framesReceived: number
  framesDelivered: number
  framesDropped: number
  currentFps: number
  subscriberCount: number
  latency?: number
}

export interface StreamViewerProps {
  tunnelId: string
  token: string
  onDisconnect?: () => void
  onError?: (error: Error) => void
  className?: string
  showControls?: boolean
  showStats?: boolean
}

export type StreamConnectionStatus =
  | 'connecting'
  | 'connected'
  | 'disconnected'
  | 'error'
