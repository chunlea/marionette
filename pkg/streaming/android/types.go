// Package android provides Android device streaming and input forwarding capabilities.
// It uses scrcpy for screen mirroring and ADB for device management and input.
package android

import (
	"encoding/json"
	"time"
)

// DeviceState represents the connection state of an Android device.
type DeviceState string

const (
	// DeviceStateDevice indicates the device is connected and ready.
	DeviceStateDevice DeviceState = "device"
	// DeviceStateOffline indicates the device is offline.
	DeviceStateOffline DeviceState = "offline"
	// DeviceStateUnauthorized indicates USB debugging is not authorized.
	DeviceStateUnauthorized DeviceState = "unauthorized"
	// DeviceStateBootloader indicates the device is in bootloader mode.
	DeviceStateBootloader DeviceState = "bootloader"
	// DeviceStateRecovery indicates the device is in recovery mode.
	DeviceStateRecovery DeviceState = "recovery"
	// DeviceStateSideload indicates the device is in sideload mode.
	DeviceStateSideload DeviceState = "sideload"
	// DeviceStateNoDevice indicates no device is connected.
	DeviceStateNoDevice DeviceState = "no device"
)

// IsConnected returns true if the device is in a connected state.
func (s DeviceState) IsConnected() bool {
	return s == DeviceStateDevice
}

// String returns the string representation of the device state.
func (s DeviceState) String() string {
	return string(s)
}

// Device represents an Android device connected via ADB.
type Device struct {
	// Serial is the unique device identifier (e.g., "emulator-5554" or USB serial).
	Serial string `json:"serial"`
	// Model is the device model name (e.g., "Pixel_4_API_30").
	Model string `json:"model,omitempty"`
	// Product is the product name (e.g., "sdk_gphone_x86_64").
	Product string `json:"product,omitempty"`
	// Device is the device name (e.g., "generic_x86_64").
	Device string `json:"device,omitempty"`
	// TransportID is the ADB transport identifier.
	TransportID string `json:"transport_id,omitempty"`
	// State is the connection state of the device.
	State DeviceState `json:"state"`
	// IsEmulator indicates if this is an emulator.
	IsEmulator bool `json:"is_emulator"`
	// AndroidVersion is the Android SDK version (e.g., "30").
	AndroidVersion string `json:"android_version,omitempty"`
	// ScreenSize is the screen resolution (e.g., "1080x1920").
	ScreenSize string `json:"screen_size,omitempty"`
	// ScreenDensity is the screen density in DPI.
	ScreenDensity int `json:"screen_density,omitempty"`
}

// StreamState represents the state of an Android stream.
type StreamState string

const (
	// StreamStateStarting indicates the stream is starting up.
	StreamStateStarting StreamState = "starting"
	// StreamStateRunning indicates the stream is active.
	StreamStateRunning StreamState = "running"
	// StreamStatePaused indicates the stream is paused.
	StreamStatePaused StreamState = "paused"
	// StreamStateStopped indicates the stream has stopped.
	StreamStateStopped StreamState = "stopped"
	// StreamStateError indicates an error occurred.
	StreamStateError StreamState = "error"
)

// String returns the string representation of the stream state.
func (s StreamState) String() string {
	return string(s)
}

// IsActive returns true if the stream is in an active state.
func (s StreamState) IsActive() bool {
	return s == StreamStateStarting || s == StreamStateRunning
}

// StreamOptions configures Android screen streaming.
type StreamOptions struct {
	// DeviceSerial is the target device serial number (required).
	DeviceSerial string `json:"device_serial"`

	// MaxWidth is the maximum width of the video stream (0 = device width).
	// The stream will maintain aspect ratio.
	MaxWidth int `json:"max_width,omitempty"`

	// MaxHeight is the maximum height of the video stream (0 = device height).
	// The stream will maintain aspect ratio.
	MaxHeight int `json:"max_height,omitempty"`

	// Bitrate is the video bitrate in bits per second (default: 8Mbps).
	Bitrate int `json:"bitrate,omitempty"`

	// MaxFPS is the maximum frames per second (default: 60, 0 = unlimited).
	MaxFPS int `json:"max_fps,omitempty"`

	// Rotation specifies the rotation angle (0, 90, 180, 270).
	Rotation int `json:"rotation,omitempty"`

	// LockOrientation locks the orientation (0 = unlocked).
	LockOrientation int `json:"lock_orientation,omitempty"`

	// Crop specifies a crop region in the format "width:height:x:y".
	Crop string `json:"crop,omitempty"`

	// NoControl disables device control (touch, keyboard).
	NoControl bool `json:"no_control,omitempty"`

	// StayAwake keeps the device awake while streaming.
	StayAwake bool `json:"stay_awake,omitempty"`

	// ShowTouches displays touch points on the device screen.
	ShowTouches bool `json:"show_touches,omitempty"`

	// TurnScreenOff turns off the device screen during streaming.
	TurnScreenOff bool `json:"turn_screen_off,omitempty"`

	// VideoCodec specifies the video codec (h264, h265, av1).
	// Default is h264 for maximum compatibility.
	VideoCodec string `json:"video_codec,omitempty"`

	// AudioEnabled enables audio streaming (scrcpy 2.0+).
	AudioEnabled bool `json:"audio_enabled,omitempty"`

	// AudioCodec specifies the audio codec (opus, aac, flac, raw).
	AudioCodec string `json:"audio_codec,omitempty"`
}

// Validate validates the stream options.
func (o *StreamOptions) Validate() error {
	if o.DeviceSerial == "" {
		return &InvalidOptionsError{Field: "device_serial", Message: "device serial is required"}
	}
	if o.MaxWidth < 0 {
		return &InvalidOptionsError{Field: "max_width", Message: "max width must be non-negative"}
	}
	if o.MaxHeight < 0 {
		return &InvalidOptionsError{Field: "max_height", Message: "max height must be non-negative"}
	}
	if o.Bitrate < 0 {
		return &InvalidOptionsError{Field: "bitrate", Message: "bitrate must be non-negative"}
	}
	if o.MaxFPS < 0 {
		return &InvalidOptionsError{Field: "max_fps", Message: "max FPS must be non-negative"}
	}
	validRotations := map[int]bool{0: true, 90: true, 180: true, 270: true}
	if !validRotations[o.Rotation] {
		return &InvalidOptionsError{Field: "rotation", Message: "rotation must be 0, 90, 180, or 270"}
	}
	validCodecs := map[string]bool{"": true, "h264": true, "h265": true, "av1": true}
	if !validCodecs[o.VideoCodec] {
		return &InvalidOptionsError{Field: "video_codec", Message: "video codec must be h264, h265, or av1"}
	}
	validAudioCodecs := map[string]bool{"": true, "opus": true, "aac": true, "flac": true, "raw": true}
	if !validAudioCodecs[o.AudioCodec] {
		return &InvalidOptionsError{Field: "audio_codec", Message: "audio codec must be opus, aac, flac, or raw"}
	}
	return nil
}

// WithDefaults returns a copy of StreamOptions with default values applied.
func (o StreamOptions) WithDefaults() StreamOptions {
	if o.Bitrate == 0 {
		o.Bitrate = 8_000_000 // 8 Mbps
	}
	if o.MaxFPS == 0 {
		o.MaxFPS = 60
	}
	if o.VideoCodec == "" {
		o.VideoCodec = "h264"
	}
	if o.AudioEnabled && o.AudioCodec == "" {
		o.AudioCodec = "opus"
	}
	return o
}

// StreamInfo contains information about an active stream.
type StreamInfo struct {
	// ID is the unique stream identifier.
	ID string `json:"id"`

	// SessionID is the Marionette session this stream belongs to.
	SessionID string `json:"session_id"`

	// Device is the streaming device.
	Device *Device `json:"device"`

	// State is the current stream state.
	State StreamState `json:"state"`

	// Options contains the stream configuration.
	Options *StreamOptions `json:"options"`

	// LocalPort is the local port for the video stream.
	LocalPort int `json:"local_port,omitempty"`

	// PublicURL is the public URL for accessing the stream.
	PublicURL string `json:"public_url,omitempty"`

	// Width is the actual stream width.
	Width int `json:"width,omitempty"`

	// Height is the actual stream height.
	Height int `json:"height,omitempty"`

	// StartedAt is when the stream started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// Error contains error details if State is StreamStateError.
	Error string `json:"error,omitempty"`

	// Stats contains streaming statistics.
	Stats *StreamStats `json:"stats,omitempty"`
}

// StreamStats contains streaming statistics.
type StreamStats struct {
	// FramesSent is the total number of frames sent.
	FramesSent int64 `json:"frames_sent"`

	// BytesSent is the total bytes sent.
	BytesSent int64 `json:"bytes_sent"`

	// DroppedFrames is the number of dropped frames.
	DroppedFrames int64 `json:"dropped_frames"`

	// AverageFPS is the average frames per second.
	AverageFPS float64 `json:"average_fps"`

	// CurrentBitrate is the current bitrate in bits per second.
	CurrentBitrate int64 `json:"current_bitrate"`

	// Latency is the current latency in milliseconds.
	Latency int64 `json:"latency_ms"`

	// UpdatedAt is when stats were last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// InputType represents the type of input event.
type InputType string

const (
	// InputTypeTap represents a tap/touch event.
	InputTypeTap InputType = "tap"
	// InputTypeSwipe represents a swipe gesture.
	InputTypeSwipe InputType = "swipe"
	// InputTypeLongPress represents a long press gesture.
	InputTypeLongPress InputType = "long_press"
	// InputTypeText represents text input.
	InputTypeText InputType = "text"
	// InputTypeKey represents a key event.
	InputTypeKey InputType = "key"
	// InputTypeScroll represents a scroll event.
	InputTypeScroll InputType = "scroll"
	// InputTypePinch represents a pinch gesture.
	InputTypePinch InputType = "pinch"
)

// String returns the string representation of the input type.
func (t InputType) String() string {
	return string(t)
}

// InputEvent represents an input event to be sent to the device.
type InputEvent struct {
	// Type is the input event type.
	Type InputType `json:"type"`

	// X is the X coordinate (for tap, swipe, long_press, scroll).
	X int `json:"x,omitempty"`

	// Y is the Y coordinate (for tap, swipe, long_press, scroll).
	Y int `json:"y,omitempty"`

	// EndX is the ending X coordinate (for swipe).
	EndX int `json:"end_x,omitempty"`

	// EndY is the ending Y coordinate (for swipe).
	EndY int `json:"end_y,omitempty"`

	// Duration is the duration in milliseconds (for swipe, long_press).
	Duration int `json:"duration,omitempty"`

	// Text is the text to input (for text).
	Text string `json:"text,omitempty"`

	// KeyCode is the Android keycode (for key).
	// See: https://developer.android.com/reference/android/view/KeyEvent
	KeyCode int `json:"key_code,omitempty"`

	// KeyName is the key name (alternative to KeyCode).
	// Examples: "BACK", "HOME", "VOLUME_UP", "ENTER"
	KeyName string `json:"key_name,omitempty"`

	// ScrollX is the horizontal scroll amount (for scroll).
	ScrollX int `json:"scroll_x,omitempty"`

	// ScrollY is the vertical scroll amount (for scroll).
	ScrollY int `json:"scroll_y,omitempty"`

	// Scale is the pinch scale factor (for pinch, >1 = zoom in, <1 = zoom out).
	Scale float64 `json:"scale,omitempty"`
}

// Validate validates the input event.
func (e *InputEvent) Validate() error {
	switch e.Type {
	case InputTypeTap, InputTypeLongPress:
		if e.X < 0 || e.Y < 0 {
			return &InvalidInputError{Type: e.Type, Message: "coordinates must be non-negative"}
		}
	case InputTypeSwipe:
		if e.X < 0 || e.Y < 0 || e.EndX < 0 || e.EndY < 0 {
			return &InvalidInputError{Type: e.Type, Message: "coordinates must be non-negative"}
		}
		if e.Duration < 0 {
			return &InvalidInputError{Type: e.Type, Message: "duration must be non-negative"}
		}
	case InputTypeText:
		if e.Text == "" {
			return &InvalidInputError{Type: e.Type, Message: "text is required"}
		}
	case InputTypeKey:
		if e.KeyCode == 0 && e.KeyName == "" {
			return &InvalidInputError{Type: e.Type, Message: "key code or key name is required"}
		}
	case InputTypeScroll:
		// Scroll amounts can be negative (scroll up/left)
	case InputTypePinch:
		if e.Scale <= 0 {
			return &InvalidInputError{Type: e.Type, Message: "scale must be positive"}
		}
	default:
		return &InvalidInputError{Type: e.Type, Message: "unknown input type"}
	}
	return nil
}

// MarshalJSON implements json.Marshaler for InputEvent.
func (e InputEvent) MarshalJSON() ([]byte, error) {
	type Alias InputEvent
	return json.Marshal(&struct {
		Alias
	}{
		Alias: (Alias)(e),
	})
}

// StreamRecord represents a stored stream record in the database.
type StreamRecord struct {
	// ID is the unique stream identifier (astr_ prefix).
	ID string `json:"id"`

	// SessionID is the Marionette session ID.
	SessionID string `json:"session_id"`

	// RunnerID is the runner providing the stream.
	RunnerID string `json:"runner_id"`

	// TunnelID is the associated tunnel ID.
	TunnelID string `json:"tunnel_id,omitempty"`

	// DeviceSerial is the Android device serial.
	DeviceSerial string `json:"device_serial"`

	// State is the stream state.
	State StreamState `json:"state"`

	// Options is the stream configuration (JSON).
	Options json.RawMessage `json:"options"`

	// LocalPort is the local port on the runner.
	LocalPort int `json:"local_port"`

	// Width is the stream width.
	Width int `json:"width,omitempty"`

	// Height is the stream height.
	Height int `json:"height,omitempty"`

	// Error is the error message if state is error.
	Error string `json:"error,omitempty"`

	// TenantID for multi-tenant support.
	TenantID *string `json:"tenant_id,omitempty"`

	// CreatedAt is when the stream record was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the stream record was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// ClosedAt is when the stream was closed.
	ClosedAt *time.Time `json:"closed_at,omitempty"`
}

// StreamRecordUpdates contains fields that can be updated on a stream record.
type StreamRecordUpdates struct {
	State     *StreamState
	LocalPort *int
	Width     *int
	Height    *int
	Error     *string
	ClosedAt  *time.Time
}
