// Package browser provides browser streaming infrastructure for Marionette.
// It enables real-time streaming of browser content from agents to clients
// using Chrome DevTools Protocol (CDP).
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// StreamState represents the current state of a browser stream.
type StreamState string

const (
	// StreamStateIdle indicates the stream is created but not started.
	StreamStateIdle StreamState = "idle"
	// StreamStateStarting indicates the stream is in the process of starting.
	StreamStateStarting StreamState = "starting"
	// StreamStateActive indicates the stream is actively sending frames.
	StreamStateActive StreamState = "active"
	// StreamStatePaused indicates the stream is temporarily paused.
	StreamStatePaused StreamState = "paused"
	// StreamStateStopping indicates the stream is in the process of stopping.
	StreamStateStopping StreamState = "stopping"
	// StreamStateStopped indicates the stream has been stopped.
	StreamStateStopped StreamState = "stopped"
	// StreamStateError indicates the stream encountered an error.
	StreamStateError StreamState = "error"
)

// IsValid returns true if the state is a valid StreamState.
func (s StreamState) IsValid() bool {
	switch s {
	case StreamStateIdle, StreamStateStarting, StreamStateActive,
		StreamStatePaused, StreamStateStopping, StreamStateStopped, StreamStateError:
		return true
	default:
		return false
	}
}

// String returns the string representation of the stream state.
func (s StreamState) String() string {
	return string(s)
}

// FrameFormat specifies the format of captured frames.
type FrameFormat string

const (
	// FormatJPEG indicates JPEG format for frames (recommended for streaming).
	FormatJPEG FrameFormat = "jpeg"
	// FormatPNG indicates PNG format for frames (lossless, larger size).
	FormatPNG FrameFormat = "png"
	// FormatWebP indicates WebP format for frames (good compression, modern).
	FormatWebP FrameFormat = "webp"
)

// IsValid returns true if the format is a valid FrameFormat.
func (f FrameFormat) IsValid() bool {
	switch f {
	case FormatJPEG, FormatPNG, FormatWebP:
		return true
	default:
		return false
	}
}

// String returns the string representation of the frame format.
func (f FrameFormat) String() string {
	return string(f)
}

// StreamOptions configures browser stream behavior.
type StreamOptions struct {
	// TargetURL optionally specifies a specific tab URL to stream.
	// If empty, streams the active tab.
	TargetURL string `json:"target_url,omitempty"`

	// Quality specifies the JPEG/WebP quality (1-100).
	// Higher values produce better quality but larger frames.
	// Default: 80
	Quality int `json:"quality,omitempty"`

	// MaxFPS limits the maximum frames per second.
	// CDP may send fewer frames if the page is static.
	// Default: 30
	MaxFPS int `json:"max_fps,omitempty"`

	// Format specifies the image format for frames.
	// Default: FormatJPEG
	Format FrameFormat `json:"format,omitempty"`

	// MaxWidth limits the maximum width of captured frames.
	// 0 means no limit (use browser viewport width).
	MaxWidth int `json:"max_width,omitempty"`

	// MaxHeight limits the maximum height of captured frames.
	// 0 means no limit (use browser viewport height).
	MaxHeight int `json:"max_height,omitempty"`

	// EveryNthFrame captures every Nth frame to reduce bandwidth.
	// 1 = capture all frames, 2 = capture every other frame, etc.
	// Default: 1
	EveryNthFrame int `json:"every_nth_frame,omitempty"`
}

// Validate validates the stream options and sets defaults.
func (o *StreamOptions) Validate() error {
	if o.Quality < 0 || o.Quality > 100 {
		return ErrInvalidQuality
	}
	if o.Quality == 0 {
		o.Quality = DefaultQuality
	}

	if o.MaxFPS < 0 {
		return ErrInvalidFPS
	}
	if o.MaxFPS == 0 {
		o.MaxFPS = DefaultMaxFPS
	}
	if o.MaxFPS > MaxAllowedFPS {
		o.MaxFPS = MaxAllowedFPS
	}

	if o.Format == "" {
		o.Format = FormatJPEG
	}
	if !o.Format.IsValid() {
		return ErrInvalidFormat
	}

	if o.MaxWidth < 0 || o.MaxHeight < 0 {
		return ErrInvalidDimensions
	}

	if o.EveryNthFrame < 0 {
		return ErrInvalidEveryNthFrame
	}
	if o.EveryNthFrame == 0 {
		o.EveryNthFrame = 1
	}

	return nil
}

// Clone creates a deep copy of the options.
func (o *StreamOptions) Clone() *StreamOptions {
	if o == nil {
		return nil
	}
	return &StreamOptions{
		TargetURL:     o.TargetURL,
		Quality:       o.Quality,
		MaxFPS:        o.MaxFPS,
		Format:        o.Format,
		MaxWidth:      o.MaxWidth,
		MaxHeight:     o.MaxHeight,
		EveryNthFrame: o.EveryNthFrame,
	}
}

// Frame represents a single captured browser frame.
type Frame struct {
	// Data contains the encoded image data.
	Data []byte `json:"data"`

	// Format indicates the image format.
	Format FrameFormat `json:"format"`

	// Width is the frame width in pixels.
	Width int `json:"width"`

	// Height is the frame height in pixels.
	Height int `json:"height"`

	// Timestamp is when the frame was captured.
	Timestamp time.Time `json:"timestamp"`

	// Sequence is the frame sequence number (monotonically increasing).
	Sequence uint64 `json:"sequence"`

	// Metadata contains optional frame metadata.
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// Size returns the size of the frame data in bytes.
func (f *Frame) Size() int {
	if f == nil {
		return 0
	}
	return len(f.Data)
}

// Clone creates a deep copy of the frame.
func (f *Frame) Clone() *Frame {
	if f == nil {
		return nil
	}
	data := make([]byte, len(f.Data))
	copy(data, f.Data)

	var metadata json.RawMessage
	if f.Metadata != nil {
		metadata = make(json.RawMessage, len(f.Metadata))
		copy(metadata, f.Metadata)
	}

	return &Frame{
		Data:      data,
		Format:    f.Format,
		Width:     f.Width,
		Height:    f.Height,
		Timestamp: f.Timestamp,
		Sequence:  f.Sequence,
		Metadata:  metadata,
	}
}

// StreamInfo provides information about a browser stream.
type StreamInfo struct {
	// ID is the unique stream identifier.
	ID string `json:"id"`

	// SessionID is the associated session.
	SessionID string `json:"session_id"`

	// RunnerID is the runner providing the stream.
	RunnerID string `json:"runner_id,omitempty"`

	// State is the current stream state.
	State StreamState `json:"state"`

	// Options are the stream configuration options.
	Options *StreamOptions `json:"options,omitempty"`

	// URL is the WebSocket URL for connecting to the stream.
	URL string `json:"url,omitempty"`

	// Stats contains stream statistics.
	Stats *StreamStats `json:"stats,omitempty"`

	// Error contains error information if State is StreamStateError.
	Error string `json:"error,omitempty"`

	// StartedAt is when the stream was started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// StoppedAt is when the stream was stopped.
	StoppedAt *time.Time `json:"stopped_at,omitempty"`

	// CreatedAt is when the stream record was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the stream record was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// StreamStats contains statistics about a browser stream.
type StreamStats struct {
	// FramesSent is the total number of frames sent.
	FramesSent uint64 `json:"frames_sent"`

	// FramesDropped is the number of frames dropped due to backpressure.
	FramesDropped uint64 `json:"frames_dropped"`

	// BytesSent is the total bytes sent.
	BytesSent uint64 `json:"bytes_sent"`

	// AverageFPS is the average frames per second.
	AverageFPS float64 `json:"average_fps"`

	// CurrentFPS is the current frames per second.
	CurrentFPS float64 `json:"current_fps"`

	// AverageFrameSize is the average frame size in bytes.
	AverageFrameSize int `json:"average_frame_size"`

	// LastFrameAt is when the last frame was sent.
	LastFrameAt *time.Time `json:"last_frame_at,omitempty"`

	// Latency is the estimated latency in milliseconds.
	Latency int `json:"latency_ms"`
}

// Clone creates a deep copy of the stats.
func (s *StreamStats) Clone() *StreamStats {
	if s == nil {
		return nil
	}
	stats := *s
	if s.LastFrameAt != nil {
		t := *s.LastFrameAt
		stats.LastFrameAt = &t
	}
	return &stats
}

// InputEvent represents an input event to forward to the browser.
type InputEvent struct {
	// Type is the event type.
	Type InputEventType `json:"type"`

	// Timestamp is when the event occurred on the client.
	Timestamp time.Time `json:"timestamp"`

	// Mouse contains mouse event data (for mouse events).
	Mouse *MouseEvent `json:"mouse,omitempty"`

	// Keyboard contains keyboard event data (for keyboard events).
	Keyboard *KeyboardEvent `json:"keyboard,omitempty"`
}

// InputEventType specifies the type of input event.
type InputEventType string

const (
	// InputEventMouseMove indicates a mouse move event.
	InputEventMouseMove InputEventType = "mouseMove"
	// InputEventMouseDown indicates a mouse button press.
	InputEventMouseDown InputEventType = "mouseDown"
	// InputEventMouseUp indicates a mouse button release.
	InputEventMouseUp InputEventType = "mouseUp"
	// InputEventMouseWheel indicates a mouse wheel scroll.
	InputEventMouseWheel InputEventType = "mouseWheel"
	// InputEventKeyDown indicates a key press.
	InputEventKeyDown InputEventType = "keyDown"
	// InputEventKeyUp indicates a key release.
	InputEventKeyUp InputEventType = "keyUp"
)

// IsValid returns true if the event type is valid.
func (t InputEventType) IsValid() bool {
	switch t {
	case InputEventMouseMove, InputEventMouseDown, InputEventMouseUp,
		InputEventMouseWheel, InputEventKeyDown, InputEventKeyUp:
		return true
	default:
		return false
	}
}

// MouseButton represents a mouse button.
type MouseButton string

const (
	// MouseButtonLeft is the left mouse button.
	MouseButtonLeft MouseButton = "left"
	// MouseButtonMiddle is the middle mouse button.
	MouseButtonMiddle MouseButton = "middle"
	// MouseButtonRight is the right mouse button.
	MouseButtonRight MouseButton = "right"
)

// IsValid returns true if the button is valid.
func (b MouseButton) IsValid() bool {
	switch b {
	case MouseButtonLeft, MouseButtonMiddle, MouseButtonRight, "":
		return true
	default:
		return false
	}
}

// MouseEvent represents a mouse input event.
type MouseEvent struct {
	// X is the horizontal position in viewport coordinates.
	X float64 `json:"x"`

	// Y is the vertical position in viewport coordinates.
	Y float64 `json:"y"`

	// Button is the mouse button (for click events).
	Button MouseButton `json:"button,omitempty"`

	// ClickCount is the number of clicks (1=single, 2=double).
	ClickCount int `json:"click_count,omitempty"`

	// DeltaX is the horizontal scroll delta (for wheel events).
	DeltaX float64 `json:"delta_x,omitempty"`

	// DeltaY is the vertical scroll delta (for wheel events).
	DeltaY float64 `json:"delta_y,omitempty"`

	// Modifiers contains modifier key states.
	Modifiers *Modifiers `json:"modifiers,omitempty"`
}

// KeyboardEvent represents a keyboard input event.
type KeyboardEvent struct {
	// Key is the key value (e.g., "a", "Enter", "Escape").
	Key string `json:"key"`

	// Code is the physical key code (e.g., "KeyA", "Enter").
	Code string `json:"code,omitempty"`

	// Text is the text generated by the key.
	Text string `json:"text,omitempty"`

	// Modifiers contains modifier key states.
	Modifiers *Modifiers `json:"modifiers,omitempty"`
}

// Modifiers represents keyboard modifier key states.
type Modifiers struct {
	// Alt is true if Alt/Option is pressed.
	Alt bool `json:"alt,omitempty"`

	// Ctrl is true if Control is pressed.
	Ctrl bool `json:"ctrl,omitempty"`

	// Meta is true if Meta/Command is pressed.
	Meta bool `json:"meta,omitempty"`

	// Shift is true if Shift is pressed.
	Shift bool `json:"shift,omitempty"`
}

// ToCDPModifiers converts modifiers to CDP modifier flags.
func (m *Modifiers) ToCDPModifiers() int {
	if m == nil {
		return 0
	}
	var flags int
	if m.Alt {
		flags |= 1
	}
	if m.Ctrl {
		flags |= 2
	}
	if m.Meta {
		flags |= 4
	}
	if m.Shift {
		flags |= 8
	}
	return flags
}

// NavigateRequest represents a request to navigate the browser.
type NavigateRequest struct {
	// URL is the URL to navigate to.
	URL string `json:"url"`

	// Referrer is an optional referrer URL.
	Referrer string `json:"referrer,omitempty"`

	// TransitionType specifies how the navigation was initiated.
	TransitionType string `json:"transition_type,omitempty"`
}

// Validate validates the navigate request.
func (r *NavigateRequest) Validate() error {
	if r.URL == "" {
		return ErrEmptyURL
	}
	return nil
}

// BrowserInfo provides information about the browser.
type BrowserInfo struct {
	// Product is the browser product name (e.g., "Chrome/120.0.0.0").
	Product string `json:"product"`

	// UserAgent is the browser user agent string.
	UserAgent string `json:"user_agent"`

	// JSVersion is the JavaScript/V8 version.
	JSVersion string `json:"js_version,omitempty"`

	// ProtocolVersion is the CDP protocol version.
	ProtocolVersion string `json:"protocol_version"`

	// Revision is the browser revision.
	Revision string `json:"revision,omitempty"`
}

// TabInfo provides information about a browser tab.
type TabInfo struct {
	// ID is the tab/target ID.
	ID string `json:"id"`

	// Title is the page title.
	Title string `json:"title"`

	// URL is the current page URL.
	URL string `json:"url"`

	// Type is the target type (e.g., "page", "background_page").
	Type string `json:"type"`

	// Attached indicates if CDP is attached to this target.
	Attached bool `json:"attached"`

	// FaviconURL is the page favicon URL.
	FaviconURL string `json:"favicon_url,omitempty"`
}

// FrameHandler is a callback function for handling received frames.
type FrameHandler func(ctx context.Context, frame *Frame) error

// StateHandler is a callback function for handling state changes.
type StateHandler func(ctx context.Context, oldState, newState StreamState, err error)

// Common errors.
var (
	// ErrStreamNotFound indicates the stream was not found.
	ErrStreamNotFound = errors.New("stream not found")

	// ErrStreamAlreadyExists indicates a stream already exists.
	ErrStreamAlreadyExists = errors.New("stream already exists")

	// ErrStreamNotActive indicates the stream is not active.
	ErrStreamNotActive = errors.New("stream not active")

	// ErrStreamAlreadyActive indicates the stream is already active.
	ErrStreamAlreadyActive = errors.New("stream already active")

	// ErrInvalidQuality indicates an invalid quality value.
	ErrInvalidQuality = errors.New("quality must be between 0 and 100")

	// ErrInvalidFPS indicates an invalid FPS value.
	ErrInvalidFPS = errors.New("max_fps must be non-negative")

	// ErrInvalidFormat indicates an invalid frame format.
	ErrInvalidFormat = errors.New("invalid frame format")

	// ErrInvalidDimensions indicates invalid dimension values.
	ErrInvalidDimensions = errors.New("dimensions must be non-negative")

	// ErrInvalidEveryNthFrame indicates an invalid every_nth_frame value.
	ErrInvalidEveryNthFrame = errors.New("every_nth_frame must be non-negative")

	// ErrBrowserNotConnected indicates the browser is not connected.
	ErrBrowserNotConnected = errors.New("browser not connected")

	// ErrBrowserConnectionFailed indicates browser connection failed.
	ErrBrowserConnectionFailed = errors.New("browser connection failed")

	// ErrEmptyURL indicates an empty URL was provided.
	ErrEmptyURL = errors.New("URL cannot be empty")

	// ErrInvalidInputEvent indicates an invalid input event.
	ErrInvalidInputEvent = errors.New("invalid input event")

	// ErrBufferFull indicates the frame buffer is full.
	ErrBufferFull = errors.New("frame buffer full")

	// ErrProviderClosed indicates the provider has been closed.
	ErrProviderClosed = errors.New("provider closed")
)

// Default values.
const (
	// DefaultQuality is the default JPEG quality.
	DefaultQuality = 80

	// DefaultMaxFPS is the default maximum frames per second.
	DefaultMaxFPS = 30

	// MaxAllowedFPS is the maximum allowed frames per second.
	MaxAllowedFPS = 60

	// DefaultBufferSize is the default frame buffer size.
	DefaultBufferSize = 10

	// MaxBufferSize is the maximum frame buffer size.
	MaxBufferSize = 100
)
