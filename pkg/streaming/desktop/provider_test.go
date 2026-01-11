package desktop

import (
	"testing"

	"github.com/chunlea/marionette/pkg/streaming"
)

func TestConfig(t *testing.T) {
	config := Config{
		SignalingBaseURL:      "wss://signal.example.com",
		Display:               ":0",
		Resolution:            streaming.Resolution{Width: 1920, Height: 1080},
		FrameRate:             60,
		BitRate:               8000,
		VideoCodec:            "h264",
		HardwareAcceleration:  true,
		AudioEnabled:          true,
		AudioDevice:           "pulse",
		EnableInputForwarding: true,
		InputDevice:           "/dev/uinput",
		ICEServers: []streaming.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	if config.SignalingBaseURL != "wss://signal.example.com" {
		t.Errorf("expected signaling base URL %q, got %q",
			"wss://signal.example.com", config.SignalingBaseURL)
	}
	if config.Display != ":0" {
		t.Errorf("expected display %q, got %q", ":0", config.Display)
	}
	if config.Resolution.Width != 1920 || config.Resolution.Height != 1080 {
		t.Errorf("expected resolution 1920x1080, got %dx%d",
			config.Resolution.Width, config.Resolution.Height)
	}
	if config.VideoCodec != "h264" {
		t.Errorf("expected video codec %q, got %q", "h264", config.VideoCodec)
	}
	if !config.HardwareAcceleration {
		t.Error("expected hardware acceleration enabled")
	}
	if !config.AudioEnabled {
		t.Error("expected audio enabled")
	}
	if config.AudioDevice != "pulse" {
		t.Errorf("expected audio device %q, got %q", "pulse", config.AudioDevice)
	}
	if !config.EnableInputForwarding {
		t.Error("expected input forwarding enabled")
	}
	if config.InputDevice != "/dev/uinput" {
		t.Errorf("expected input device %q, got %q", "/dev/uinput", config.InputDevice)
	}
	if len(config.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(config.ICEServers))
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
		t.Error("expected primary display")
	}
}

func TestCapabilities(t *testing.T) {
	caps := Capabilities{
		VideoCodecs:           []string{"h264", "vp8", "vp9"},
		MaxResolution:         streaming.Resolution{Width: 4096, Height: 2160},
		MaxFrameRate:          120,
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
	if caps.MaxFrameRate != 120 {
		t.Errorf("expected max frame rate 120, got %d", caps.MaxFrameRate)
	}
	if !caps.HardwareAcceleration {
		t.Error("expected hardware acceleration")
	}
	if len(caps.SupportedAccelerators) != 2 {
		t.Errorf("expected 2 accelerators, got %d", len(caps.SupportedAccelerators))
	}
	if !caps.AudioCapture {
		t.Error("expected audio capture")
	}
	if !caps.InputForwarding {
		t.Error("expected input forwarding")
	}
}

func TestProcessInfo(t *testing.T) {
	info := ProcessInfo{
		PID:       12345,
		State:     ProcessStateRunning,
		StartTime: 1640000000,
		Memory:    1024 * 1024 * 100, // 100MB
		CPU:       25.5,
		Error:     "",
	}

	if info.PID != 12345 {
		t.Errorf("expected PID 12345, got %d", info.PID)
	}
	if info.State != ProcessStateRunning {
		t.Errorf("expected state %q, got %q", ProcessStateRunning, info.State)
	}
	if info.StartTime != 1640000000 {
		t.Errorf("expected start time 1640000000, got %d", info.StartTime)
	}
	if info.Memory != 1024*1024*100 {
		t.Errorf("expected memory 104857600, got %d", info.Memory)
	}
	if info.CPU != 25.5 {
		t.Errorf("expected CPU 25.5, got %f", info.CPU)
	}
	if info.Error != "" {
		t.Errorf("expected empty error, got %q", info.Error)
	}
}

func TestInputEvent(t *testing.T) {
	// Test keyboard event
	keyEvent := InputEvent{
		Type: InputEventKeyDown,
		Key:  "KeyA",
	}
	if keyEvent.Type != InputEventKeyDown {
		t.Errorf("expected type %q, got %q", InputEventKeyDown, keyEvent.Type)
	}
	if keyEvent.Key != "KeyA" {
		t.Errorf("expected key %q, got %q", "KeyA", keyEvent.Key)
	}

	// Test mouse event
	mouseEvent := InputEvent{
		Type:   InputEventMouseMove,
		X:      0.5,
		Y:      0.5,
		Button: 0,
	}
	if mouseEvent.Type != InputEventMouseMove {
		t.Errorf("expected type %q, got %q", InputEventMouseMove, mouseEvent.Type)
	}
	if mouseEvent.X != 0.5 || mouseEvent.Y != 0.5 {
		t.Errorf("expected position (0.5, 0.5), got (%f, %f)", mouseEvent.X, mouseEvent.Y)
	}

	// Test wheel event
	wheelEvent := InputEvent{
		Type:   InputEventMouseWheel,
		DeltaX: 0,
		DeltaY: -100,
	}
	if wheelEvent.Type != InputEventMouseWheel {
		t.Errorf("expected type %q, got %q", InputEventMouseWheel, wheelEvent.Type)
	}
	if wheelEvent.DeltaY != -100 {
		t.Errorf("expected deltaY -100, got %f", wheelEvent.DeltaY)
	}
}
