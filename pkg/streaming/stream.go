// Package streaming provides interfaces and types for real-time streaming
// capabilities in Marionette, including desktop, browser, and mobile streaming.
package streaming

import (
	"context"
	"time"
)

// StreamType defines the type of stream.
type StreamType string

const (
	// StreamTypeDesktop represents a desktop video stream via WebRTC.
	StreamTypeDesktop StreamType = "desktop"

	// StreamTypeBrowser represents a browser streaming session.
	StreamTypeBrowser StreamType = "browser"

	// StreamTypeIOS represents an iOS device screen stream.
	StreamTypeIOS StreamType = "ios"

	// StreamTypeAndroid represents an Android device screen stream.
	StreamTypeAndroid StreamType = "android"
)

// StreamState represents the current state of a stream.
type StreamState string

const (
	// StreamStatePending indicates the stream is being initialized.
	StreamStatePending StreamState = "pending"

	// StreamStateStarting indicates the stream is starting up.
	StreamStateStarting StreamState = "starting"

	// StreamStateActive indicates the stream is active and available.
	StreamStateActive StreamState = "active"

	// StreamStatePaused indicates the stream is temporarily paused.
	StreamStatePaused StreamState = "paused"

	// StreamStateStopping indicates the stream is being stopped.
	StreamStateStopping StreamState = "stopping"

	// StreamStateStopped indicates the stream has been stopped.
	StreamStateStopped StreamState = "stopped"

	// StreamStateError indicates the stream encountered an error.
	StreamStateError StreamState = "error"
)

// StreamOptions contains configuration options for starting a stream.
type StreamOptions struct {
	// SessionID is the session this stream belongs to.
	SessionID string

	// RunnerID is the runner executing the stream.
	RunnerID string

	// Type specifies the stream type (desktop, browser, etc.).
	Type StreamType

	// Resolution specifies the desired video resolution.
	Resolution Resolution

	// FrameRate specifies the target frame rate (fps).
	FrameRate int

	// BitRate specifies the target video bitrate in kbps.
	BitRate int

	// EnableAudio enables audio capture (if supported).
	EnableAudio bool

	// EnableInput enables input forwarding (keyboard/mouse).
	EnableInput bool

	// Display specifies which display to capture (for multi-monitor setups).
	// Empty string means primary/default display.
	Display string

	// ICEServers specifies STUN/TURN servers for WebRTC.
	ICEServers []ICEServer

	// Timeout specifies the maximum time to wait for stream to start.
	Timeout time.Duration
}

// Resolution represents video resolution dimensions.
type Resolution struct {
	Width  int
	Height int
}

// String returns a string representation of the resolution.
func (r Resolution) String() string {
	if r.Width == 0 || r.Height == 0 {
		return "auto"
	}
	return string(rune(r.Width)) + "x" + string(rune(r.Height))
}

// ICEServer represents a STUN or TURN server configuration.
type ICEServer struct {
	// URLs specifies the server URLs (e.g., "stun:stun.l.google.com:19302").
	URLs []string

	// Username for TURN server authentication.
	Username string

	// Credential for TURN server authentication.
	Credential string
}

// StreamInfo contains information about an active stream.
type StreamInfo struct {
	// ID is the unique identifier for this stream.
	ID string

	// SessionID is the session this stream belongs to.
	SessionID string

	// RunnerID is the runner providing this stream.
	RunnerID string

	// Type specifies the stream type.
	Type StreamType

	// State is the current state of the stream.
	State StreamState

	// SignalingURL is the WebSocket URL for WebRTC signaling.
	SignalingURL string

	// ICEServers are the STUN/TURN servers to use.
	ICEServers []ICEServer

	// Resolution is the actual stream resolution.
	Resolution Resolution

	// FrameRate is the actual stream frame rate.
	FrameRate int

	// BitRate is the actual stream bitrate.
	BitRate int

	// AudioEnabled indicates if audio is being streamed.
	AudioEnabled bool

	// InputEnabled indicates if input forwarding is enabled.
	InputEnabled bool

	// StartedAt is when the stream was started.
	StartedAt time.Time

	// Error contains error details if State is StreamStateError.
	Error string

	// Metadata contains additional provider-specific information.
	Metadata map[string]string
}

// StreamProvider defines the interface for stream providers.
// Each provider implementation handles the specifics of capturing
// and encoding video for streaming.
type StreamProvider interface {
	// Name returns the provider name (e.g., "selkies", "vnc", "scrcpy").
	Name() string

	// SupportedTypes returns the stream types this provider supports.
	SupportedTypes() []StreamType

	// Start starts a new stream with the given options.
	// It returns stream information including the signaling URL.
	Start(ctx context.Context, opts StreamOptions) (*StreamInfo, error)

	// Stop stops an active stream.
	Stop(ctx context.Context, streamID string) error

	// GetStatus returns the current status of a stream.
	GetStatus(ctx context.Context, streamID string) (*StreamInfo, error)

	// UpdateOptions updates stream options for an active stream.
	// Not all options can be changed while streaming.
	UpdateOptions(ctx context.Context, streamID string, opts StreamOptions) error

	// HealthCheck verifies the provider is functioning correctly.
	HealthCheck(ctx context.Context) error
}

// InputEvent represents a keyboard or mouse input event.
type InputEvent struct {
	// Type specifies the event type.
	Type InputEventType

	// Timestamp is when the event occurred.
	Timestamp time.Time

	// Keyboard event data (for keyboard events).
	Key *KeyEvent

	// Mouse event data (for mouse events).
	Mouse *MouseEvent
}

// InputEventType specifies the type of input event.
type InputEventType string

const (
	// InputEventKeyDown indicates a key press event.
	InputEventKeyDown InputEventType = "keydown"

	// InputEventKeyUp indicates a key release event.
	InputEventKeyUp InputEventType = "keyup"

	// InputEventMouseMove indicates mouse movement.
	InputEventMouseMove InputEventType = "mousemove"

	// InputEventMouseDown indicates a mouse button press.
	InputEventMouseDown InputEventType = "mousedown"

	// InputEventMouseUp indicates a mouse button release.
	InputEventMouseUp InputEventType = "mouseup"

	// InputEventMouseWheel indicates mouse wheel scroll.
	InputEventMouseWheel InputEventType = "mousewheel"
)

// KeyEvent represents a keyboard event.
type KeyEvent struct {
	// Key is the key code (e.g., "KeyA", "Enter", "Escape").
	Key string

	// Code is the physical key code.
	Code string

	// Modifiers contains active modifier keys.
	Modifiers KeyModifiers
}

// KeyModifiers represents active keyboard modifiers.
type KeyModifiers struct {
	Ctrl  bool
	Shift bool
	Alt   bool
	Meta  bool
}

// MouseEvent represents a mouse event.
type MouseEvent struct {
	// X is the horizontal position (0.0 to 1.0, normalized).
	X float64

	// Y is the vertical position (0.0 to 1.0, normalized).
	Y float64

	// Button is the mouse button (0=left, 1=middle, 2=right).
	Button int

	// DeltaX is horizontal scroll amount.
	DeltaX float64

	// DeltaY is vertical scroll amount.
	DeltaY float64
}

// InputHandler handles input events for a stream.
type InputHandler interface {
	// HandleInput processes an input event.
	HandleInput(ctx context.Context, streamID string, event InputEvent) error

	// SetInputEnabled enables or disables input handling.
	SetInputEnabled(ctx context.Context, streamID string, enabled bool) error
}

// SignalingMessage represents a WebRTC signaling message.
type SignalingMessage struct {
	// Type specifies the message type.
	Type SignalingMessageType

	// StreamID identifies the stream this message is for.
	StreamID string

	// SessionID is the Marionette session ID.
	SessionID string

	// SDP contains the Session Description Protocol data.
	SDP string

	// Candidate contains ICE candidate information.
	Candidate *ICECandidate

	// Error contains error information if applicable.
	Error string
}

// SignalingMessageType specifies the type of signaling message.
type SignalingMessageType string

const (
	// SignalingOffer is an SDP offer message.
	SignalingOffer SignalingMessageType = "offer"

	// SignalingAnswer is an SDP answer message.
	SignalingAnswer SignalingMessageType = "answer"

	// SignalingCandidate is an ICE candidate message.
	SignalingCandidate SignalingMessageType = "candidate"

	// SignalingError indicates a signaling error.
	SignalingError SignalingMessageType = "error"

	// SignalingPing is a keepalive message.
	SignalingPing SignalingMessageType = "ping"

	// SignalingPong is a keepalive response.
	SignalingPong SignalingMessageType = "pong"
)

// ICECandidate represents an ICE candidate for WebRTC.
type ICECandidate struct {
	// Candidate is the candidate string.
	Candidate string

	// SDPMid is the media stream identification.
	SDPMid string

	// SDPMLineIndex is the media description index.
	SDPMLineIndex int
}
