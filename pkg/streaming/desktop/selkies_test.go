package desktop

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

func TestNewSelkiesProvider(t *testing.T) {
	config := DefaultSelkiesConfig()
	logger := zap.NewNop()

	provider := NewSelkiesProvider(config, logger)

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.config.ExecutablePath != "selkies-gstreamer" {
		t.Errorf("expected executable path %q, got %q", "selkies-gstreamer", provider.config.ExecutablePath)
	}
}

func TestNewSelkiesProviderNilLogger(t *testing.T) {
	config := DefaultSelkiesConfig()

	provider := NewSelkiesProvider(config, nil)

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	// Should use nop logger when nil is passed
}

func TestSelkiesProviderName(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	name := provider.Name()

	if name != "selkies" {
		t.Errorf("expected name %q, got %q", "selkies", name)
	}
}

func TestSelkiesProviderSupportedTypes(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	types := provider.SupportedTypes()

	if len(types) != 1 {
		t.Fatalf("expected 1 supported type, got %d", len(types))
	}
	if types[0] != streaming.StreamTypeDesktop {
		t.Errorf("expected type %q, got %q", streaming.StreamTypeDesktop, types[0])
	}
}

func TestDefaultSelkiesConfig(t *testing.T) {
	config := DefaultSelkiesConfig()

	if config.ExecutablePath != "selkies-gstreamer" {
		t.Errorf("expected executable path %q, got %q", "selkies-gstreamer", config.ExecutablePath)
	}
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
	if !config.EnableInput {
		t.Error("expected input to be enabled by default")
	}
	if config.StartupTimeout != 30*time.Second {
		t.Errorf("expected startup timeout 30s, got %v", config.StartupTimeout)
	}
	if config.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected shutdown timeout 10s, got %v", config.ShutdownTimeout)
	}
	if len(config.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(config.ICEServers))
	}
}

func TestSelkiesProviderGetInfoNotFound(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	_, err := provider.GetInfo(ctx, "nonexistent")

	if err == nil {
		t.Fatal("expected error for nonexistent stream")
	}
	provErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if provErr.Operation != "get_info" {
		t.Errorf("expected operation %q, got %q", "get_info", provErr.Operation)
	}
	if provErr.Message != "stream not found" {
		t.Errorf("expected message %q, got %q", "stream not found", provErr.Message)
	}
}

func TestSelkiesProviderStopNotFound(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	err := provider.Stop(ctx, "nonexistent")

	if err == nil {
		t.Fatal("expected error for nonexistent stream")
	}
	provErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if provErr.Operation != "stop" {
		t.Errorf("expected operation %q, got %q", "stop", provErr.Operation)
	}
}

func TestSelkiesProviderSetDisplayNotSupported(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	err := provider.SetDisplay(ctx, "any-stream", ":1")

	if err == nil {
		t.Fatal("expected error for set display")
	}
	provErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if provErr.Operation != "set_display" {
		t.Errorf("expected operation %q, got %q", "set_display", provErr.Operation)
	}
}

func TestSelkiesProviderGetCapabilities(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	caps, err := provider.GetCapabilities(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caps == nil {
		t.Fatal("expected non-nil capabilities")
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
	if !caps.AudioCapture {
		t.Error("expected audio capture support")
	}
	if !caps.InputForwarding {
		t.Error("expected input forwarding support")
	}
}

func TestSelkiesProviderInputHandler(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	handler := provider.InputHandler()

	// Selkies handles input through WebRTC data channel, so returns nil
	if handler != nil {
		t.Error("expected nil input handler")
	}
}

func TestSelkiesProviderGetDisplayInfo(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	displays, err := provider.GetDisplayInfo(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return at least a default display
	if len(displays) == 0 {
		t.Error("expected at least one display")
	}
	if displays[0].Width <= 0 || displays[0].Height <= 0 {
		t.Error("expected valid display dimensions")
	}
}

func TestProviderError(t *testing.T) {
	t.Run("without cause", func(t *testing.T) {
		err := &ProviderError{
			Provider:  "test",
			Operation: "start",
			Message:   "something failed",
		}

		errStr := err.Error()
		expected := "test: start: something failed"
		if errStr != expected {
			t.Errorf("expected %q, got %q", expected, errStr)
		}
		if err.Unwrap() != nil {
			t.Error("expected nil unwrapped error")
		}
	})

	t.Run("with cause", func(t *testing.T) {
		cause := &ProviderError{Provider: "inner", Operation: "op", Message: "inner error"}
		err := &ProviderError{
			Provider:  "test",
			Operation: "start",
			Message:   "something failed",
			Cause:     cause,
		}

		errStr := err.Error()
		if errStr != "test: start: something failed: inner: op: inner error" {
			t.Errorf("unexpected error string: %s", errStr)
		}
		if err.Unwrap() != cause {
			t.Error("expected cause to be unwrapped")
		}
	})
}

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
		t.Error("expected hardware acceleration enabled")
	}
	if config.AudioEnabled {
		t.Error("expected audio disabled by default")
	}
	if !config.EnableInputForwarding {
		t.Error("expected input forwarding enabled")
	}
	if len(config.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(config.ICEServers))
	}
}

func TestBuildArgs(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	stream := &selkiesStream{
		config: SelkiesConfig{
			Display:              ":0",
			Resolution:           streaming.Resolution{Width: 1280, Height: 720},
			FrameRate:            60,
			BitRate:              8000,
			VideoCodec:           "vp9",
			HardwareAcceleration: true,
			AudioEnabled:         true,
			AudioDevice:          "pulse",
			SignalingURL:         "ws://example.com/ws",
		},
	}

	args := provider.buildArgs(stream)

	// Check that expected args are present
	expectedArgs := map[string]bool{
		"--display":          false,
		"--video-width":      false,
		"--video-height":     false,
		"--framerate":        false,
		"--video-bitrate":    false,
		"--encoder":          false,
		"--enable-hw-accel":  false,
		"--enable-audio":     false,
		"--audio-device":     false,
		"--signaling-server": false,
	}

	for _, arg := range args {
		if _, ok := expectedArgs[arg]; ok {
			expectedArgs[arg] = true
		}
	}

	for arg, found := range expectedArgs {
		if !found {
			t.Errorf("expected arg %q not found", arg)
		}
	}
}

func TestBuildEnv(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	stream := &selkiesStream{
		config: SelkiesConfig{
			Display: ":0",
			ICEServers: []streaming.ICEServer{
				{URLs: []string{"stun:stun.example.com:3478"}},
			},
		},
	}

	env := provider.buildEnv(stream)

	// Check for DISPLAY
	foundDisplay := false
	foundICE := false
	for _, e := range env {
		if e == "DISPLAY=:0" {
			foundDisplay = true
		}
		if strings.HasPrefix(e, "SELKIES_TURN_SERVERS=") {
			foundICE = true
		}
	}

	if !foundDisplay {
		t.Error("expected DISPLAY env var")
	}
	if !foundICE {
		t.Error("expected SELKIES_TURN_SERVERS env var")
	}
}

func TestMergeConfig(t *testing.T) {
	providerConfig := DefaultSelkiesConfig()
	providerConfig.Resolution = streaming.Resolution{Width: 1920, Height: 1080}
	providerConfig.FrameRate = 30

	provider := NewSelkiesProvider(providerConfig, zap.NewNop())

	opts := streaming.StreamOptions{
		Resolution:   streaming.Resolution{Width: 1280, Height: 720},
		FrameRate:    60,
		BitRate:      8000,
		AudioEnabled: true,
		InputEnabled: false,
		ICEServers: []streaming.ICEServer{
			{URLs: []string{"stun:custom.example.com"}},
		},
	}

	merged := provider.mergeConfig(opts)

	if merged.Resolution.Width != 1280 || merged.Resolution.Height != 720 {
		t.Errorf("expected resolution 1280x720, got %dx%d",
			merged.Resolution.Width, merged.Resolution.Height)
	}
	if merged.FrameRate != 60 {
		t.Errorf("expected frame rate 60, got %d", merged.FrameRate)
	}
	if merged.BitRate != 8000 {
		t.Errorf("expected bitrate 8000, got %d", merged.BitRate)
	}
	if !merged.AudioEnabled {
		t.Error("expected audio enabled")
	}
	if merged.EnableInput {
		t.Error("expected input disabled")
	}
	if len(merged.ICEServers) != 1 || merged.ICEServers[0].URLs[0] != "stun:custom.example.com" {
		t.Error("expected custom ICE servers")
	}
}

func TestMergeConfigDefaults(t *testing.T) {
	providerConfig := DefaultSelkiesConfig()
	provider := NewSelkiesProvider(providerConfig, zap.NewNop())

	// Empty options should use provider defaults
	opts := streaming.StreamOptions{}

	merged := provider.mergeConfig(opts)

	if merged.Resolution.Width != 1920 || merged.Resolution.Height != 1080 {
		t.Errorf("expected default resolution 1920x1080, got %dx%d",
			merged.Resolution.Width, merged.Resolution.Height)
	}
	if merged.FrameRate != 30 {
		t.Errorf("expected default frame rate 30, got %d", merged.FrameRate)
	}
}

func TestCleanup(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Add some stopped streams
	provider.streams["stopped1"] = &selkiesStream{state: ProcessStateStopped}
	provider.streams["error1"] = &selkiesStream{state: ProcessStateError}
	provider.streams["running1"] = &selkiesStream{state: ProcessStateRunning}

	provider.Cleanup()

	if len(provider.streams) != 1 {
		t.Errorf("expected 1 stream after cleanup, got %d", len(provider.streams))
	}
	if _, ok := provider.streams["running1"]; !ok {
		t.Error("expected running stream to be preserved")
	}
}

func TestGetConfigPath(t *testing.T) {
	// This test just verifies the function doesn't panic
	// Actual path depends on system configuration
	_ = GetConfigPath()
}

func TestSelkiesProviderImplementsStreamProvider(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Verify it implements streaming.StreamProvider
	var _ streaming.StreamProvider = provider
}

func TestSelkiesProviderImplementsDesktopProvider(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Verify it implements DesktopProvider
	var _ DesktopProvider = provider
}

func TestProcessState(t *testing.T) {
	tests := []struct {
		state    ProcessState
		expected string
	}{
		{ProcessStateStopped, "stopped"},
		{ProcessStateStarting, "starting"},
		{ProcessStateRunning, "running"},
		{ProcessStateStopping, "stopping"},
		{ProcessStateError, "error"},
	}

	for _, tt := range tests {
		if string(tt.state) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, string(tt.state))
		}
	}
}

func TestInputEventType(t *testing.T) {
	tests := []struct {
		eventType InputEventType
		expected  string
	}{
		{InputEventKeyDown, "keydown"},
		{InputEventKeyUp, "keyup"},
		{InputEventMouseMove, "mousemove"},
		{InputEventMouseDown, "mousedown"},
		{InputEventMouseUp, "mouseup"},
		{InputEventMouseWheel, "mousewheel"},
	}

	for _, tt := range tests {
		if string(tt.eventType) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, string(tt.eventType))
		}
	}
}
