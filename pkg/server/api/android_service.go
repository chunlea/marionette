package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// AndroidStreamService defines operations for managing Android screen streaming.
// This interface is implemented by the core android stream manager and
// can be mocked for testing HTTP handlers.
type AndroidStreamService interface {
	// StartStream starts a new Android screen stream for a session.
	StartStream(ctx context.Context, opts StartAndroidStreamOptions) (*store.AndroidStream, error)

	// GetStream retrieves an Android stream by ID.
	GetStream(ctx context.Context, id string) (*store.AndroidStream, error)

	// ListStreams returns Android streams matching the filter options.
	ListStreams(ctx context.Context, opts ListAndroidStreamsOptions) (*store.ListResult[store.AndroidStream], error)

	// StopStream stops an active Android stream.
	StopStream(ctx context.Context, id string) error

	// ListDevices lists available Android devices for a session.
	ListDevices(ctx context.Context, sessionID string) ([]AndroidDevice, error)

	// SendInput sends an input event to an Android device.
	SendInput(ctx context.Context, streamID string, input AndroidInputEvent) error
}

// StartAndroidStreamOptions contains options for starting an Android stream.
type StartAndroidStreamOptions struct {
	SessionID    string `json:"session_id"`
	DeviceSerial string `json:"device_serial"`

	// Video options
	MaxWidth  int `json:"max_width,omitempty"`
	MaxHeight int `json:"max_height,omitempty"`
	MaxFPS    int `json:"max_fps,omitempty"`
	Bitrate   int `json:"bitrate,omitempty"`

	// Audio options
	AudioEnabled bool `json:"audio_enabled,omitempty"`
}

// ListAndroidStreamsOptions contains options for listing Android streams.
type ListAndroidStreamsOptions struct {
	Limit         int      `json:"limit,omitempty"`
	Cursor        string   `json:"cursor,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	DeviceSerial  string   `json:"device_serial,omitempty"`
	State         []string `json:"state,omitempty"`
	IncludeClosed bool     `json:"include_closed,omitempty"`
}

// AndroidDevice represents an Android device available for streaming.
type AndroidDevice struct {
	Serial      string `json:"serial"`
	State       string `json:"state"` // device, offline, unauthorized
	Product     string `json:"product,omitempty"`
	Model       string `json:"model,omitempty"`
	Device      string `json:"device,omitempty"`
	TransportID string `json:"transport_id,omitempty"`
}

// AndroidInputEvent represents an input event to send to an Android device.
type AndroidInputEvent struct {
	Type string `json:"type"` // touch, key, text

	// Touch event fields
	Action string  `json:"action,omitempty"` // down, up, move
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`

	// Key event fields
	KeyCode   int    `json:"key_code,omitempty"`
	KeyAction string `json:"key_action,omitempty"` // down, up

	// Text event fields
	Text string `json:"text,omitempty"`
}
