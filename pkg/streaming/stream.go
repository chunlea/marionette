// Package streaming provides unified streaming infrastructure for Marionette.
// It supports desktop, browser, iOS, and Android streaming through a common
// SFU (Selective Forwarding Unit) architecture.
package streaming

import (
	"slices"
	"time"
)

// StreamType represents the type of stream.
type StreamType string

const (
	// StreamTypeDesktop is for desktop screen streaming (e.g., Selkies).
	StreamTypeDesktop StreamType = "desktop"

	// StreamTypeBrowser is for browser streaming (e.g., CDP).
	StreamTypeBrowser StreamType = "browser"

	// StreamTypeIOS is for iOS device streaming.
	StreamTypeIOS StreamType = "ios"

	// StreamTypeAndroid is for Android device streaming (e.g., scrcpy).
	StreamTypeAndroid StreamType = "android"
)

// ValidStreamTypes is the list of valid stream types.
var ValidStreamTypes = []StreamType{
	StreamTypeDesktop,
	StreamTypeBrowser,
	StreamTypeIOS,
	StreamTypeAndroid,
}

// IsValid returns true if the stream type is valid.
func (t StreamType) IsValid() bool {
	return slices.Contains(ValidStreamTypes, t)
}

// String returns the string representation of the stream type.
func (t StreamType) String() string {
	return string(t)
}

// StreamState represents the state of a stream.
type StreamState string

const (
	// StreamStatePending is the initial state when a stream is created.
	StreamStatePending StreamState = "pending"

	// StreamStateStarting is when the stream is being initialized.
	StreamStateStarting StreamState = "starting"

	// StreamStateActive is when the stream is actively running.
	StreamStateActive StreamState = "active"

	// StreamStatePaused is when the stream is temporarily paused.
	StreamStatePaused StreamState = "paused"

	// StreamStateStopping is when the stream is being stopped.
	StreamStateStopping StreamState = "stopping"

	// StreamStateStopped is when the stream has been stopped normally.
	StreamStateStopped StreamState = "stopped"

	// StreamStateError is when the stream has encountered an error.
	StreamStateError StreamState = "error"
)

// ValidStreamStates is the list of valid stream states.
var ValidStreamStates = []StreamState{
	StreamStatePending,
	StreamStateStarting,
	StreamStateActive,
	StreamStatePaused,
	StreamStateStopping,
	StreamStateStopped,
	StreamStateError,
}

// IsValid returns true if the stream state is valid.
func (s StreamState) IsValid() bool {
	return slices.Contains(ValidStreamStates, s)
}

// IsTerminal returns true if the stream state is terminal (stopped or error).
func (s StreamState) IsTerminal() bool {
	return s == StreamStateStopped || s == StreamStateError
}

// IsActive returns true if the stream is currently active or paused.
func (s StreamState) IsActive() bool {
	return s == StreamStateActive || s == StreamStatePaused
}

// String returns the string representation of the stream state.
func (s StreamState) String() string {
	return string(s)
}

// Resolution represents video resolution.
type Resolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// IsZero returns true if the resolution is zero (unset).
func (r Resolution) IsZero() bool {
	return r.Width == 0 && r.Height == 0
}

// ICEServer represents a STUN/TURN server configuration for WebRTC.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// StreamOptions contains options for creating a new stream.
type StreamOptions struct {
	// SessionID is the session this stream belongs to.
	SessionID string

	// RunnerID is the runner that will handle this stream.
	RunnerID string

	// TenantID is the tenant this stream belongs to.
	TenantID string

	// Type is the type of stream (desktop, browser, ios, android).
	Type StreamType

	// Resolution is the desired video resolution.
	Resolution Resolution

	// FrameRate is the desired frame rate in FPS.
	FrameRate int

	// BitRate is the desired bit rate in bps.
	BitRate int

	// AudioEnabled indicates whether audio should be streamed.
	AudioEnabled bool

	// InputEnabled indicates whether input (keyboard/mouse) should be enabled.
	InputEnabled bool

	// ICEServers is the list of STUN/TURN servers.
	ICEServers []ICEServer

	// Metadata is arbitrary key-value metadata.
	Metadata map[string]string

	// ExpiresAt is when the stream should expire.
	ExpiresAt *time.Time
}

// Validate validates the stream options.
func (o StreamOptions) Validate() error {
	if o.SessionID == "" {
		return ErrSessionRequired
	}
	if !o.Type.IsValid() {
		return ErrInvalidStreamType
	}
	return nil
}

// Stream represents a streaming session.
type Stream struct {
	// ID is the unique identifier for this stream.
	ID string `json:"id"`

	// SessionID is the session this stream belongs to.
	SessionID string `json:"session_id"`

	// RunnerID is the runner handling this stream.
	RunnerID string `json:"runner_id,omitempty"`

	// TenantID is the tenant this stream belongs to.
	TenantID string `json:"tenant_id,omitempty"`

	// Type is the type of stream.
	Type StreamType `json:"type"`

	// State is the current state of the stream.
	State StreamState `json:"state"`

	// SignalingURL is the WebSocket URL for WebRTC signaling.
	SignalingURL string `json:"signaling_url,omitempty"`

	// ICEServers is the list of STUN/TURN servers.
	ICEServers []ICEServer `json:"ice_servers,omitempty"`

	// Resolution is the current video resolution.
	Resolution Resolution `json:"resolution,omitempty"`

	// FrameRate is the current frame rate in FPS.
	FrameRate int `json:"frame_rate,omitempty"`

	// BitRate is the current bit rate in bps.
	BitRate int `json:"bitrate,omitempty"`

	// VideoCodec is the video codec being used.
	VideoCodec string `json:"video_codec,omitempty"`

	// AudioCodec is the audio codec being used.
	AudioCodec string `json:"audio_codec,omitempty"`

	// AudioEnabled indicates whether audio is enabled.
	AudioEnabled bool `json:"audio_enabled"`

	// InputEnabled indicates whether input is enabled.
	InputEnabled bool `json:"input_enabled"`

	// ProviderName is the name of the provider handling this stream.
	ProviderName string `json:"provider_name"`

	// ProviderStreamID is the provider's internal stream ID.
	ProviderStreamID string `json:"provider_stream_id,omitempty"`

	// Error contains the error message if State is StreamStateError.
	Error string `json:"error,omitempty"`

	// Metadata is arbitrary key-value metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when the stream was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the stream was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// StartedAt is when the stream started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// StoppedAt is when the stream stopped.
	StoppedAt *time.Time `json:"stopped_at,omitempty"`

	// ExpiresAt is when the stream will expire.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// DeviceInfo represents a discoverable device for streaming.
// This is used by providers that support device enumeration
// (e.g., Android devices, iOS simulators).
type DeviceInfo struct {
	// ID is the unique device identifier (e.g., serial number).
	ID string `json:"id"`

	// Name is the human-readable device name.
	Name string `json:"name"`

	// Type is the stream type this device supports.
	Type StreamType `json:"type"`

	// State is the device connection state.
	State string `json:"state"`

	// Metadata is device-specific metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// StreamInfo is returned by providers when a stream is started or queried.
type StreamInfo struct {
	// ID is the provider's stream ID.
	ID string `json:"id"`

	// SignalingURL is the WebSocket URL for WebRTC signaling.
	SignalingURL string `json:"signaling_url,omitempty"`

	// Resolution is the video resolution.
	Resolution Resolution `json:"resolution,omitempty"`

	// FrameRate is the frame rate in FPS.
	FrameRate int `json:"frame_rate,omitempty"`

	// BitRate is the bit rate in bps.
	BitRate int `json:"bitrate,omitempty"`

	// VideoCodec is the video codec.
	VideoCodec string `json:"video_codec,omitempty"`

	// AudioCodec is the audio codec.
	AudioCodec string `json:"audio_codec,omitempty"`

	// Metadata is provider-specific metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}
