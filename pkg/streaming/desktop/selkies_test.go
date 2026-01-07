package desktop

import (
	"context"
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

func TestSelkiesProviderGetStatusNotFound(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	_, err := provider.GetStatus(ctx, "nonexistent")

	if err == nil {
		t.Fatal("expected error for nonexistent stream")
	}
	provErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if provErr.Operation != "get_status" {
		t.Errorf("expected operation %q, got %q", "get_status", provErr.Operation)
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

func TestSelkiesProviderUpdateOptionsNotSupported(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	err := provider.UpdateOptions(ctx, "any-stream", streaming.StreamOptions{})

	if err == nil {
		t.Fatal("expected error for update options")
	}
	provErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if provErr.Operation != "update_options" {
		t.Errorf("expected operation %q, got %q", "update_options", provErr.Operation)
	}
	if provErr.Message != "runtime option updates not supported" {
		t.Errorf("expected message about not supported, got %q", provErr.Message)
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
		t.Error("expected audio capture to be supported")
	}
	if !caps.InputForwarding {
		t.Error("expected input forwarding to be supported")
	}
}

func TestSelkiesProviderInputHandler(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	handler := provider.InputHandler()

	// Selkies handles input through WebRTC data channel, so we return nil
	if handler != nil {
		t.Error("expected nil input handler for Selkies")
	}
}

func TestSelkiesProviderCleanup(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Add a mock stopped stream to test cleanup
	provider.mu.Lock()
	provider.streams["test-stream"] = &selkiesStream{
		id:    "test-stream",
		state: ProcessStateStopped,
	}
	provider.streams["running-stream"] = &selkiesStream{
		id:    "running-stream",
		state: ProcessStateRunning,
	}
	provider.mu.Unlock()

	provider.Cleanup()

	provider.mu.RLock()
	defer provider.mu.RUnlock()

	// Stopped stream should be removed
	if _, ok := provider.streams["test-stream"]; ok {
		t.Error("expected stopped stream to be cleaned up")
	}
	// Running stream should remain
	if _, ok := provider.streams["running-stream"]; !ok {
		t.Error("expected running stream to remain")
	}
}

func TestBuildArgs(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	stream := &selkiesStream{
		id: "test-stream",
		config: SelkiesConfig{
			Display:              ":0",
			Resolution:           streaming.Resolution{Width: 1920, Height: 1080},
			FrameRate:            30,
			BitRate:              4000,
			VideoCodec:           "h264",
			HardwareAcceleration: true,
			AudioEnabled:         true,
			AudioDevice:          "pulse",
			SignalingURL:         "ws://localhost:8080/ws",
		},
	}

	args := provider.buildArgs(stream)

	// Check that expected args are present
	expectedArgs := map[string]string{
		"--display":         ":0",
		"--video-width":     "1920",
		"--video-height":    "1080",
		"--framerate":       "30",
		"--video-bitrate":   "4000",
		"--encoder":         "h264",
		"--signaling-server": "ws://localhost:8080/ws",
	}

	for i := 0; i < len(args)-1; i++ {
		if expected, ok := expectedArgs[args[i]]; ok {
			if args[i+1] != expected {
				t.Errorf("expected %s %s, got %s %s", args[i], expected, args[i], args[i+1])
			}
		}
	}

	// Check for --enable-hw-accel flag
	hasHWAccel := false
	for _, arg := range args {
		if arg == "--enable-hw-accel" {
			hasHWAccel = true
			break
		}
	}
	if !hasHWAccel {
		t.Error("expected --enable-hw-accel flag")
	}

	// Check for --enable-audio flag
	hasAudio := false
	for _, arg := range args {
		if arg == "--enable-audio" {
			hasAudio = true
			break
		}
	}
	if !hasAudio {
		t.Error("expected --enable-audio flag")
	}
}

func TestBuildEnv(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	stream := &selkiesStream{
		id: "test-stream",
		config: SelkiesConfig{
			Display: ":99",
			ICEServers: []streaming.ICEServer{
				{URLs: []string{"stun:stun.l.google.com:19302"}},
			},
		},
	}

	env := provider.buildEnv(stream)

	// Check for DISPLAY
	hasDisplay := false
	for _, e := range env {
		if e == "DISPLAY=:99" {
			hasDisplay = true
			break
		}
	}
	if !hasDisplay {
		t.Error("expected DISPLAY=:99 in environment")
	}

	// Check for SELKIES_TURN_SERVERS
	hasTurnServers := false
	for _, e := range env {
		if len(e) > 21 && e[:21] == "SELKIES_TURN_SERVERS=" {
			hasTurnServers = true
			break
		}
	}
	if !hasTurnServers {
		t.Error("expected SELKIES_TURN_SERVERS in environment")
	}
}

func TestMergeConfig(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	opts := streaming.StreamOptions{
		Resolution: streaming.Resolution{Width: 2560, Height: 1440},
		FrameRate:  60,
		BitRate:    8000,
		Display:    ":1",
		ICEServers: []streaming.ICEServer{
			{URLs: []string{"turn:turn.example.com:3478"}},
		},
		EnableAudio: true,
		EnableInput: false,
	}

	config := provider.mergeConfig(opts)

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
	if config.Display != ":1" {
		t.Errorf("expected display %q, got %q", ":1", config.Display)
	}
	if len(config.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(config.ICEServers))
	}
	if !config.AudioEnabled {
		t.Error("expected audio to be enabled")
	}
	if config.EnableInput {
		t.Error("expected input to be disabled")
	}
}

func TestMergeConfigDefaults(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Empty options should use provider defaults
	opts := streaming.StreamOptions{}

	config := provider.mergeConfig(opts)

	// Should keep default values when options are zero
	if config.Display != ":99" {
		t.Errorf("expected default display %q, got %q", ":99", config.Display)
	}
	// Zero resolution should keep default
	if config.Resolution.Width != 1920 || config.Resolution.Height != 1080 {
		t.Errorf("expected default resolution 1920x1080, got %dx%d",
			config.Resolution.Width, config.Resolution.Height)
	}
}

func TestGetConfigPath(t *testing.T) {
	// This function looks for config files in standard locations
	// Just ensure it doesn't panic
	path := GetConfigPath()
	// Path may be empty if no config files exist
	_ = path
}

func TestGetDisplayInfo(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	displays, err := provider.GetDisplayInfo(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return at least one display (even if default)
	if len(displays) == 0 {
		t.Error("expected at least one display")
	}
	// First display should have valid dimensions
	if displays[0].Width <= 0 || displays[0].Height <= 0 {
		t.Error("expected positive display dimensions")
	}
}
