package desktop

import (
	"testing"

	"github.com/chunlea/marionette/pkg/streaming"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Display != ":99" {
		t.Errorf("expected display %q, got %q", ":99", config.Display)
	}
	if config.Resolution.Width != 1920 || config.Resolution.Height != 1080 {
		t.Errorf("expected resolution 1920x1080, got %dx%d",
			config.Resolution.Width, config.Resolution.Height)
	}
	if config.FrameRate != 30 {
		t.Errorf("expected frame rate 30, got %d", config.FrameRate)
	}
	if config.BitRate != 4000 {
		t.Errorf("expected bitrate 4000, got %d", config.BitRate)
	}
	if config.VideoCodec != "h264" {
		t.Errorf("expected video codec %q, got %q", "h264", config.VideoCodec)
	}
	if !config.HardwareAcceleration {
		t.Error("expected hardware acceleration to be enabled")
	}
	if config.AudioEnabled {
		t.Error("expected audio to be disabled by default")
	}
	if !config.EnableInputForwarding {
		t.Error("expected input forwarding to be enabled")
	}
	if len(config.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(config.ICEServers))
	}
}

func TestProcessStates(t *testing.T) {
	tests := []struct {
		name     string
		state    ProcessState
		expected string
	}{
		{"stopped", ProcessStateStopped, "stopped"},
		{"starting", ProcessStateStarting, "starting"},
		{"running", ProcessStateRunning, "running"},
		{"stopping", ProcessStateStopping, "stopping"},
		{"error", ProcessStateError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.state)
			}
		})
	}
}

func TestDisplayInfo(t *testing.T) {
	info := DisplayInfo{
		Name:        ":0",
		Width:       1920,
		Height:      1080,
		RefreshRate: 60,
		IsPrimary:   true,
	}

	if info.Name != ":0" {
		t.Errorf("expected name %q, got %q", ":0", info.Name)
	}
	if info.Width != 1920 || info.Height != 1080 {
		t.Errorf("expected dimensions 1920x1080, got %dx%d", info.Width, info.Height)
	}
	if info.RefreshRate != 60 {
		t.Errorf("expected refresh rate 60, got %d", info.RefreshRate)
	}
	if !info.IsPrimary {
		t.Error("expected IsPrimary to be true")
	}
}

func TestCapabilities(t *testing.T) {
	caps := Capabilities{
		VideoCodecs:           []string{"h264", "vp8", "vp9"},
		MaxResolution:         streaming.Resolution{Width: 4096, Height: 2160},
		MaxFrameRate:          60,
		HardwareAcceleration:  true,
		SupportedAccelerators: []string{"vaapi", "nvenc"},
		AudioCapture:          true,
		InputForwarding:       true,
	}

	if len(caps.VideoCodecs) != 3 {
		t.Errorf("expected 3 video codecs, got %d", len(caps.VideoCodecs))
	}
	if caps.MaxResolution.Width != 4096 || caps.MaxResolution.Height != 2160 {
		t.Errorf("expected max resolution 4096x2160, got %dx%d",
			caps.MaxResolution.Width, caps.MaxResolution.Height)
	}
	if caps.MaxFrameRate != 60 {
		t.Errorf("expected max frame rate 60, got %d", caps.MaxFrameRate)
	}
	if !caps.HardwareAcceleration {
		t.Error("expected hardware acceleration to be available")
	}
	if len(caps.SupportedAccelerators) != 2 {
		t.Errorf("expected 2 accelerators, got %d", len(caps.SupportedAccelerators))
	}
	if !caps.AudioCapture {
		t.Error("expected audio capture to be supported")
	}
	if !caps.InputForwarding {
		t.Error("expected input forwarding to be supported")
	}
}

func TestProcessInfo(t *testing.T) {
	info := ProcessInfo{
		PID:       12345,
		State:     ProcessStateRunning,
		StartTime: 1609459200,
		Memory:    104857600, // 100 MB
		CPU:       25.5,
	}

	if info.PID != 12345 {
		t.Errorf("expected PID 12345, got %d", info.PID)
	}
	if info.State != ProcessStateRunning {
		t.Errorf("expected state %q, got %q", ProcessStateRunning, info.State)
	}
	if info.Memory != 104857600 {
		t.Errorf("expected memory 104857600, got %d", info.Memory)
	}
	if info.CPU != 25.5 {
		t.Errorf("expected CPU 25.5, got %f", info.CPU)
	}
}

func TestProcessInfoWithError(t *testing.T) {
	info := ProcessInfo{
		PID:   12345,
		State: ProcessStateError,
		Error: "failed to start encoder",
	}

	if info.State != ProcessStateError {
		t.Errorf("expected state %q, got %q", ProcessStateError, info.State)
	}
	if info.Error != "failed to start encoder" {
		t.Errorf("expected error %q, got %q", "failed to start encoder", info.Error)
	}
}

func TestProviderErrorError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ProviderError
		expected string
	}{
		{
			name: "without cause",
			err: &ProviderError{
				Provider:  "selkies",
				Operation: "start",
				Message:   "failed to start encoder",
			},
			expected: "selkies: start: failed to start encoder",
		},
		{
			name: "with cause",
			err: &ProviderError{
				Provider:  "selkies",
				Operation: "start",
				Message:   "failed to start encoder",
				Cause:     streaming.ErrStreamNotFound,
			},
			expected: "selkies: start: failed to start encoder: stream not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestProviderErrorUnwrap(t *testing.T) {
	cause := streaming.ErrStreamNotFound
	err := &ProviderError{
		Provider:  "selkies",
		Operation: "start",
		Message:   "test error",
		Cause:     cause,
	}

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Errorf("expected unwrapped error %v, got %v", cause, unwrapped)
	}
}

func TestProviderErrorUnwrapNil(t *testing.T) {
	err := &ProviderError{
		Provider:  "selkies",
		Operation: "start",
		Message:   "test error",
	}

	unwrapped := err.Unwrap()
	if unwrapped != nil {
		t.Errorf("expected nil, got %v", unwrapped)
	}
}

func TestConfig(t *testing.T) {
	config := Config{
		SignalingBaseURL:      "wss://signal.example.com",
		Display:               ":0",
		Resolution:            streaming.Resolution{Width: 2560, Height: 1440},
		FrameRate:             60,
		BitRate:               8000,
		VideoCodec:            "vp9",
		HardwareAcceleration:  true,
		AudioEnabled:          true,
		AudioDevice:           "pulse",
		EnableInputForwarding: true,
		InputDevice:           "/dev/uinput",
		ICEServers: []streaming.ICEServer{
			{
				URLs:       []string{"turn:turn.example.com:3478"},
				Username:   "user",
				Credential: "pass",
			},
		},
	}

	if config.SignalingBaseURL != "wss://signal.example.com" {
		t.Errorf("expected signaling URL %q, got %q", "wss://signal.example.com", config.SignalingBaseURL)
	}
	if config.Display != ":0" {
		t.Errorf("expected display %q, got %q", ":0", config.Display)
	}
	if config.Resolution.Width != 2560 || config.Resolution.Height != 1440 {
		t.Errorf("expected resolution 2560x1440, got %dx%d",
			config.Resolution.Width, config.Resolution.Height)
	}
	if config.FrameRate != 60 {
		t.Errorf("expected frame rate 60, got %d", config.FrameRate)
	}
	if config.BitRate != 8000 {
		t.Errorf("expected bitrate 8000, got %d", config.BitRate)
	}
	if config.VideoCodec != "vp9" {
		t.Errorf("expected video codec %q, got %q", "vp9", config.VideoCodec)
	}
	if !config.HardwareAcceleration {
		t.Error("expected hardware acceleration to be enabled")
	}
	if !config.AudioEnabled {
		t.Error("expected audio to be enabled")
	}
	if config.AudioDevice != "pulse" {
		t.Errorf("expected audio device %q, got %q", "pulse", config.AudioDevice)
	}
	if !config.EnableInputForwarding {
		t.Error("expected input forwarding to be enabled")
	}
	if config.InputDevice != "/dev/uinput" {
		t.Errorf("expected input device %q, got %q", "/dev/uinput", config.InputDevice)
	}
	if len(config.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(config.ICEServers))
	}
	if config.ICEServers[0].Username != "user" {
		t.Errorf("expected username %q, got %q", "user", config.ICEServers[0].Username)
	}
}
