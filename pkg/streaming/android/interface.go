// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package android

import (
	"context"
	"io"
)

// Provider defines the interface for Android streaming providers.
// Implementations handle device discovery, screen streaming, and input forwarding.
type Provider interface {
	// ListDevices returns all connected Android devices.
	ListDevices(ctx context.Context) ([]Device, error)

	// GetDevice returns a specific device by serial number.
	GetDevice(ctx context.Context, serial string) (*Device, error)

	// StartStream starts screen streaming for a device.
	// Returns a StreamInfo containing the stream details and local port.
	StartStream(ctx context.Context, opts StreamOptions) (*StreamInfo, error)

	// StopStream stops an active stream.
	StopStream(ctx context.Context, streamID string) error

	// GetStream returns information about an active stream.
	GetStream(ctx context.Context, streamID string) (*StreamInfo, error)

	// ListStreams returns all active streams.
	ListStreams(ctx context.Context) ([]*StreamInfo, error)

	// SendInput sends an input event to a device.
	SendInput(ctx context.Context, serial string, event InputEvent) error

	// Close releases all resources and stops all streams.
	Close() error
}

// VideoSink defines the interface for consuming video data from a stream.
// Implementations can forward to WebSocket, WebRTC, or other transports.
type VideoSink interface {
	// OnVideoData is called when video data is available.
	// The data is typically H.264 NAL units.
	OnVideoData(data []byte) error

	// OnVideoConfig is called when video configuration changes.
	// This includes SPS/PPS for H.264.
	OnVideoConfig(width, height int, codec string, config []byte) error

	// OnAudioData is called when audio data is available (if enabled).
	OnAudioData(data []byte) error

	// OnAudioConfig is called when audio configuration changes.
	OnAudioConfig(sampleRate, channels int, codec string, config []byte) error

	// OnError is called when an error occurs.
	OnError(err error)

	// OnClose is called when the stream closes.
	OnClose()
}

// StreamReader provides access to the raw video/audio stream.
type StreamReader interface {
	// Read reads video data from the stream.
	io.Reader

	// VideoConfig returns the current video configuration.
	VideoConfig() *VideoConfig

	// AudioConfig returns the current audio configuration (may be nil).
	AudioConfig() *AudioConfig

	// Close closes the reader.
	Close() error
}

// VideoConfig contains video stream configuration.
type VideoConfig struct {
	// Width is the video width in pixels.
	Width int `json:"width"`

	// Height is the video height in pixels.
	Height int `json:"height"`

	// Codec is the video codec (e.g., "h264", "h265").
	Codec string `json:"codec"`

	// SPS is the H.264 Sequence Parameter Set (for h264).
	SPS []byte `json:"sps,omitempty"`

	// PPS is the H.264 Picture Parameter Set (for h264).
	PPS []byte `json:"pps,omitempty"`
}

// AudioConfig contains audio stream configuration.
type AudioConfig struct {
	// SampleRate is the audio sample rate in Hz.
	SampleRate int `json:"sample_rate"`

	// Channels is the number of audio channels.
	Channels int `json:"channels"`

	// Codec is the audio codec (e.g., "opus", "aac").
	Codec string `json:"codec"`

	// Config is codec-specific configuration data.
	Config []byte `json:"config,omitempty"`
}

// DeviceWatcher provides device connect/disconnect notifications.
type DeviceWatcher interface {
	// Watch starts watching for device changes.
	// Returns a channel that receives device events.
	Watch(ctx context.Context) (<-chan DeviceEvent, error)

	// Close stops watching.
	Close() error
}

// DeviceEvent represents a device connect/disconnect event.
type DeviceEvent struct {
	// Type is the event type.
	Type DeviceEventType `json:"type"`

	// Device is the device that changed.
	Device Device `json:"device"`
}

// DeviceEventType represents the type of device event.
type DeviceEventType string

const (
	// DeviceEventConnected indicates a device was connected.
	DeviceEventConnected DeviceEventType = "connected"
	// DeviceEventDisconnected indicates a device was disconnected.
	DeviceEventDisconnected DeviceEventType = "disconnected"
	// DeviceEventChanged indicates device state changed.
	DeviceEventChanged DeviceEventType = "changed"
)

// String returns the string representation of the device event type.
func (t DeviceEventType) String() string {
	return string(t)
}

// InputHandler defines the interface for handling input from web clients.
// This converts web coordinates to device coordinates.
type InputHandler interface {
	// HandleTap handles a tap event at the given coordinates.
	// Coordinates are relative to the displayed video size.
	HandleTap(ctx context.Context, x, y int) error

	// HandleSwipe handles a swipe gesture.
	HandleSwipe(ctx context.Context, startX, startY, endX, endY, durationMs int) error

	// HandleLongPress handles a long press gesture.
	HandleLongPress(ctx context.Context, x, y, durationMs int) error

	// HandleText handles text input.
	HandleText(ctx context.Context, text string) error

	// HandleKey handles a key press.
	HandleKey(ctx context.Context, keyCode int) error

	// HandleBack handles the back button.
	HandleBack(ctx context.Context) error

	// HandleHome handles the home button.
	HandleHome(ctx context.Context) error

	// HandleRecent handles the recent apps button.
	HandleRecent(ctx context.Context) error

	// SetDisplaySize sets the displayed video size for coordinate mapping.
	SetDisplaySize(width, height int)
}

// Manager defines the server-side interface for managing Android streams.
// This is used by the server to track streams across sessions.
type Manager interface {
	// CreateStream creates a new stream record.
	CreateStream(ctx context.Context, input CreateStreamInput) (*StreamRecord, error)

	// GetStream retrieves a stream record by ID.
	GetStream(ctx context.Context, id string) (*StreamRecord, error)

	// GetStreamBySession retrieves the active stream for a session.
	GetStreamBySession(ctx context.Context, sessionID string) (*StreamRecord, error)

	// ListStreams lists streams with filters.
	ListStreams(ctx context.Context, opts ListStreamsOptions) ([]*StreamRecord, error)

	// UpdateStream updates a stream record.
	UpdateStream(ctx context.Context, id string, updates StreamRecordUpdates) error

	// DeleteStream deletes a stream record.
	DeleteStream(ctx context.Context, id string) error
}

// CreateStreamInput contains input for creating a stream record.
type CreateStreamInput struct {
	SessionID    string
	RunnerID     string
	TunnelID     string
	DeviceSerial string
	Options      *StreamOptions
	LocalPort    int
	TenantID     *string
}

// ListStreamsOptions contains options for listing streams.
type ListStreamsOptions struct {
	SessionID    string
	RunnerID     string
	DeviceSerial string
	State        StreamState
	TenantID     *string
	Limit        int
	Offset       int
}
