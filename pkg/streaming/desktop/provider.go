// Package desktop provides desktop streaming implementations using WebRTC.
// It supports video capture from X11/Wayland displays and input forwarding.
package desktop

import (
	"context"
	"fmt"

	"github.com/chunlea/marionette/pkg/streaming"
)

// InputHandler handles input events for a stream.
// This interface is defined here since main's streaming package doesn't include it.
type InputHandler interface {
	// HandleInput processes an input event.
	HandleInput(ctx context.Context, streamID string, event InputEvent) error

	// SetInputEnabled enables or disables input handling.
	SetInputEnabled(ctx context.Context, streamID string, enabled bool) error
}

// InputEvent represents a keyboard or mouse input event.
type InputEvent struct {
	// Type specifies the event type.
	Type InputEventType

	// Key is the key code for keyboard events.
	Key string

	// X is the horizontal position for mouse events (0.0 to 1.0).
	X float64

	// Y is the vertical position for mouse events (0.0 to 1.0).
	Y float64

	// Button is the mouse button (0=left, 1=middle, 2=right).
	Button int

	// DeltaX is horizontal scroll amount.
	DeltaX float64

	// DeltaY is vertical scroll amount.
	DeltaY float64
}

// InputEventType specifies the type of input event.
type InputEventType string

const (
	InputEventKeyDown    InputEventType = "keydown"
	InputEventKeyUp      InputEventType = "keyup"
	InputEventMouseMove  InputEventType = "mousemove"
	InputEventMouseDown  InputEventType = "mousedown"
	InputEventMouseUp    InputEventType = "mouseup"
	InputEventMouseWheel InputEventType = "mousewheel"
)

// Config contains configuration for desktop streaming providers.
type Config struct {
	// SignalingBaseURL is the base URL for the signaling server.
	// Example: "wss://signal.example.com"
	SignalingBaseURL string

	// Display specifies the X11 display to capture.
	// Example: ":0", ":99"
	Display string

	// Resolution specifies the default capture resolution.
	Resolution streaming.Resolution

	// FrameRate specifies the default frame rate.
	FrameRate int

	// BitRate specifies the default video bitrate in kbps.
	BitRate int

	// VideoCodec specifies the video codec to use.
	// Supported: "h264", "vp8", "vp9", "av1"
	VideoCodec string

	// HardwareAcceleration enables hardware encoding if available.
	HardwareAcceleration bool

	// AudioEnabled enables audio capture.
	AudioEnabled bool

	// AudioDevice specifies the audio device to capture.
	// Leave empty for default pulse/pipewire device.
	AudioDevice string

	// ICEServers specifies STUN/TURN servers for WebRTC.
	ICEServers []streaming.ICEServer

	// EnableInputForwarding enables keyboard/mouse input forwarding.
	EnableInputForwarding bool

	// InputDevice specifies the uinput device path for input injection.
	// Default: auto-detect or create virtual device.
	InputDevice string
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Display:               ":99",
		Resolution:            streaming.Resolution{Width: 1920, Height: 1080},
		FrameRate:             30,
		BitRate:               4000,
		VideoCodec:            "h264",
		HardwareAcceleration:  true,
		AudioEnabled:          false,
		EnableInputForwarding: true,
		ICEServers: []streaming.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
}

// DesktopProvider is an extended StreamProvider interface for desktop streaming.
// It adds desktop-specific capabilities on top of the base StreamProvider.
type DesktopProvider interface {
	streaming.StreamProvider

	// GetDisplayInfo returns information about available displays.
	GetDisplayInfo(ctx context.Context) ([]DisplayInfo, error)

	// SetDisplay changes the display being captured.
	SetDisplay(ctx context.Context, streamID string, display string) error

	// GetCapabilities returns the provider's capabilities.
	GetCapabilities(ctx context.Context) (*Capabilities, error)

	// InputHandler returns the input handler for this provider.
	// Returns nil if input forwarding is not supported.
	InputHandler() InputHandler
}

// DisplayInfo contains information about a display.
type DisplayInfo struct {
	// Name is the display identifier (e.g., ":0", ":99").
	Name string

	// Width is the display width in pixels.
	Width int

	// Height is the display height in pixels.
	Height int

	// RefreshRate is the display refresh rate in Hz.
	RefreshRate int

	// IsPrimary indicates if this is the primary display.
	IsPrimary bool
}

// Capabilities describes the desktop streaming capabilities.
type Capabilities struct {
	// VideoCodecs lists supported video codecs.
	VideoCodecs []string

	// MaxResolution is the maximum supported resolution.
	MaxResolution streaming.Resolution

	// MaxFrameRate is the maximum supported frame rate.
	MaxFrameRate int

	// HardwareAcceleration indicates if hardware encoding is available.
	HardwareAcceleration bool

	// SupportedAccelerators lists available hardware accelerators.
	// Example: ["vaapi", "nvenc", "qsv"]
	SupportedAccelerators []string

	// AudioCapture indicates if audio capture is supported.
	AudioCapture bool

	// InputForwarding indicates if input forwarding is supported.
	InputForwarding bool
}

// ProcessState represents the state of a streaming process.
type ProcessState string

const (
	// ProcessStateStopped indicates the process is not running.
	ProcessStateStopped ProcessState = "stopped"

	// ProcessStateStarting indicates the process is starting.
	ProcessStateStarting ProcessState = "starting"

	// ProcessStateRunning indicates the process is running.
	ProcessStateRunning ProcessState = "running"

	// ProcessStateStopping indicates the process is stopping.
	ProcessStateStopping ProcessState = "stopping"

	// ProcessStateError indicates the process encountered an error.
	ProcessStateError ProcessState = "error"
)

// ProcessInfo contains information about a streaming process.
type ProcessInfo struct {
	// PID is the process ID.
	PID int

	// State is the current process state.
	State ProcessState

	// StartTime is when the process started.
	StartTime int64

	// Memory is the current memory usage in bytes.
	Memory int64

	// CPU is the current CPU usage percentage.
	CPU float64

	// Error contains the error message if State is ProcessStateError.
	Error string
}

// ProviderError represents an error from a desktop provider.
type ProviderError struct {
	// Provider is the provider name.
	Provider string

	// Operation is the operation that failed.
	Operation string

	// Message is the error message.
	Message string

	// Cause is the underlying error, if any.
	Cause error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %s: %v", e.Provider, e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s: %s", e.Provider, e.Operation, e.Message)
}

// Unwrap returns the underlying error.
func (e *ProviderError) Unwrap() error {
	return e.Cause
}
