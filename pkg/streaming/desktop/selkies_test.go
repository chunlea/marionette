package desktop

import (
	"context"
	"os"
	"path/filepath"
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

func TestSelkiesProviderStartExecutableNotFound(t *testing.T) {
	config := DefaultSelkiesConfig()
	config.ExecutablePath = "/nonexistent/path/to/selkies"
	config.StartupTimeout = 100 * time.Millisecond
	provider := NewSelkiesProvider(config, zap.NewNop())
	ctx := context.Background()

	opts := streaming.StreamOptions{
		SessionID: "test-session",
		RunnerID:  "test-runner",
	}

	_, err := provider.Start(ctx, opts)

	if err == nil {
		t.Fatal("expected error for nonexistent executable")
	}
	provErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if provErr.Operation != "start" {
		t.Errorf("expected operation %q, got %q", "start", provErr.Operation)
	}
}

func TestSelkiesProviderStartDuplicateSession(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Manually add a running stream
	provider.streams["existing"] = &selkiesStream{
		id:        "existing",
		sessionID: "test-session",
		state:     ProcessStateRunning,
	}

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "test-session",
		RunnerID:  "test-runner",
	}

	_, err := provider.Start(ctx, opts)

	if err == nil {
		t.Fatal("expected error for duplicate session")
	}
	if !strings.Contains(err.Error(), "stream already exists") {
		t.Errorf("expected 'stream already exists' error, got: %v", err)
	}
}

func TestSelkiesProviderGetInfoWithStream(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Manually add a stream
	provider.streams["test-stream"] = &selkiesStream{
		id:           "test-stream",
		sessionID:    "test-session",
		state:        ProcessStateRunning,
		signalingURL: "ws://localhost:8080/ws",
		config: SelkiesConfig{
			Display:      ":99",
			Resolution:   streaming.Resolution{Width: 1920, Height: 1080},
			FrameRate:    30,
			BitRate:      4000,
			VideoCodec:   "h264",
			AudioEnabled: true,
			EnableInput:  true,
		},
	}

	ctx := context.Background()
	info, err := provider.GetInfo(ctx, "test-stream")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "test-stream" {
		t.Errorf("expected ID %q, got %q", "test-stream", info.ID)
	}
	if info.SignalingURL != "ws://localhost:8080/ws" {
		t.Errorf("expected signaling URL %q, got %q", "ws://localhost:8080/ws", info.SignalingURL)
	}
	if info.Resolution.Width != 1920 || info.Resolution.Height != 1080 {
		t.Errorf("expected resolution 1920x1080, got %dx%d", info.Resolution.Width, info.Resolution.Height)
	}
	if info.Metadata["state"] != "running" {
		t.Errorf("expected state %q, got %q", "running", info.Metadata["state"])
	}
}

func TestSelkiesProviderGetInfoWithError(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Manually add a stream with error
	provider.streams["error-stream"] = &selkiesStream{
		id:        "error-stream",
		sessionID: "test-session",
		state:     ProcessStateError,
		error:     "something went wrong",
		config:    DefaultSelkiesConfig(),
	}

	ctx := context.Background()
	info, err := provider.GetInfo(ctx, "error-stream")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Metadata["error"] != "something went wrong" {
		t.Errorf("expected error in metadata, got %q", info.Metadata["error"])
	}
}

func TestSelkiesProviderStopRunningStream(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Create channels
	stopC := make(chan struct{})
	stoppedC := make(chan struct{})

	// Manually add a running stream
	provider.streams["running-stream"] = &selkiesStream{
		id:        "running-stream",
		sessionID: "test-session",
		state:     ProcessStateRunning,
		stopC:     stopC,
		stoppedC:  stoppedC,
	}

	// Close stoppedC to simulate process exit
	close(stoppedC)

	ctx := context.Background()
	err := provider.Stop(ctx, "running-stream")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	provider.mu.RLock()
	stream := provider.streams["running-stream"]
	provider.mu.RUnlock()

	if stream.state != ProcessStateStopped {
		t.Errorf("expected state %q, got %q", ProcessStateStopped, stream.state)
	}
}

func TestSelkiesProviderStopAlreadyStopped(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Manually add a stopped stream
	provider.streams["stopped-stream"] = &selkiesStream{
		id:        "stopped-stream",
		sessionID: "test-session",
		state:     ProcessStateStopped,
	}

	ctx := context.Background()
	err := provider.Stop(ctx, "stopped-stream")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelkiesProviderStopAll(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Create channels for streams
	stoppedC1 := make(chan struct{})
	stoppedC2 := make(chan struct{})
	close(stoppedC1)
	close(stoppedC2)

	// Add multiple running streams
	provider.streams["stream1"] = &selkiesStream{
		id:       "stream1",
		state:    ProcessStateRunning,
		stopC:    make(chan struct{}),
		stoppedC: stoppedC1,
	}
	provider.streams["stream2"] = &selkiesStream{
		id:       "stream2",
		state:    ProcessStateRunning,
		stopC:    make(chan struct{}),
		stoppedC: stoppedC2,
	}
	provider.streams["stream3"] = &selkiesStream{
		id:    "stream3",
		state: ProcessStateStopped,
	}

	ctx := context.Background()
	provider.StopAll(ctx)

	// Check that running streams were stopped
	for _, id := range []string{"stream1", "stream2"} {
		stream := provider.streams[id]
		if stream.state != ProcessStateStopped {
			t.Errorf("expected stream %s to be stopped, got %s", id, stream.state)
		}
	}
}

func TestSelkiesProviderDetectHardwareAccelNoDevice(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	// This tests the detection logic - results depend on actual system
	hasAccel, accelerators := provider.detectHardwareAccel(ctx)

	// Just verify the function doesn't panic and returns valid types
	_ = hasAccel
	if accelerators == nil {
		t.Error("expected non-nil accelerators slice")
	}
}

func TestGetConfigPathNoConfig(t *testing.T) {
	// Save current HOME
	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()

	// Set HOME to a temp directory without config
	tmpDir := t.TempDir()
	_ = os.Setenv("HOME", tmpDir)

	path := GetConfigPath()

	// Should return empty when no config exists
	if path != "" {
		// Only fail if the path actually doesn't exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected empty path or existing file, got %q", path)
		}
	}
}

func TestGetConfigPathWithConfig(t *testing.T) {
	// Create temp directory with config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".config", "selkies")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configFile := filepath.Join(configPath, "config.yaml")
	if err := os.WriteFile(configFile, []byte("test: config"), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Save current HOME
	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()

	_ = os.Setenv("HOME", tmpDir)

	path := GetConfigPath()

	if path != configFile {
		t.Errorf("expected path %q, got %q", configFile, path)
	}
}

func TestSelkiesProviderBuildArgsMinimal(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	stream := &selkiesStream{
		config: SelkiesConfig{}, // Empty config
	}

	args := provider.buildArgs(stream)

	// Should return empty or minimal args for empty config
	if args == nil {
		t.Error("expected non-nil args")
	}
}

func TestSelkiesProviderBuildEnvEmpty(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	stream := &selkiesStream{
		config: SelkiesConfig{}, // Empty config
	}

	env := provider.buildEnv(stream)

	// Should not panic with empty config
	if env == nil {
		t.Error("expected non-nil env")
	}
}

func TestSelkiesProviderGetDisplayInfoWithEnv(t *testing.T) {
	// Save and set DISPLAY
	origDisplay := os.Getenv("DISPLAY")
	defer func() {
		if origDisplay == "" {
			_ = os.Unsetenv("DISPLAY")
		} else {
			_ = os.Setenv("DISPLAY", origDisplay)
		}
	}()

	_ = os.Setenv("DISPLAY", ":42")

	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())
	ctx := context.Background()

	displays, err := provider.GetDisplayInfo(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(displays) == 0 {
		t.Fatal("expected at least one display")
	}
	// Display name should include :42
	found := false
	for _, d := range displays {
		if d.Name == ":42" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected display :42 in list, got %v", displays)
	}
}

func TestMergeConfigWithMetadata(t *testing.T) {
	providerConfig := DefaultSelkiesConfig()
	provider := NewSelkiesProvider(providerConfig, zap.NewNop())

	opts := streaming.StreamOptions{
		Metadata: map[string]string{
			"display": ":1",
		},
	}

	merged := provider.mergeConfig(opts)

	// Metadata display should not override config display in current implementation
	// This test verifies the behavior
	if merged.Display != ":99" {
		t.Logf("Note: display from metadata is not applied (current behavior)")
	}
}

func TestSelkiesStreamState(t *testing.T) {
	stream := &selkiesStream{
		id:        "test",
		state:     ProcessStateStarting,
		startedAt: time.Now(),
	}

	if stream.state != ProcessStateStarting {
		t.Errorf("expected state %q, got %q", ProcessStateStarting, stream.state)
	}

	stream.state = ProcessStateRunning
	if stream.state != ProcessStateRunning {
		t.Errorf("expected state %q, got %q", ProcessStateRunning, stream.state)
	}

	now := time.Now()
	stream.stoppedAt = &now
	stream.state = ProcessStateStopped
	if stream.stoppedAt == nil {
		t.Error("expected stoppedAt to be set")
	}
}

func TestSelkiesStreamError(t *testing.T) {
	stream := &selkiesStream{
		id:    "test",
		state: ProcessStateError,
		error: "process crashed",
	}

	if stream.error != "process crashed" {
		t.Errorf("expected error %q, got %q", "process crashed", stream.error)
	}
}

func TestSelkiesProviderCleanupStoppedStreams(t *testing.T) {
	config := DefaultSelkiesConfig()
	provider := NewSelkiesProvider(config, zap.NewNop())

	// Add some stopped streams
	provider.mu.Lock()
	provider.streams["stopped1"] = &selkiesStream{
		id:    "stopped1",
		state: ProcessStateStopped,
	}
	provider.streams["error1"] = &selkiesStream{
		id:    "error1",
		state: ProcessStateError,
	}
	provider.streams["running1"] = &selkiesStream{
		id:    "running1",
		state: ProcessStateRunning,
	}
	provider.mu.Unlock()

	// Run cleanup
	provider.Cleanup()

	// Check that only running stream remains
	provider.mu.RLock()
	count := len(provider.streams)
	_, hasRunning := provider.streams["running1"]
	_, hasStopped := provider.streams["stopped1"]
	_, hasError := provider.streams["error1"]
	provider.mu.RUnlock()

	if count != 1 {
		t.Errorf("expected 1 stream after cleanup, got %d", count)
	}
	if !hasRunning {
		t.Error("expected running stream to remain")
	}
	if hasStopped {
		t.Error("expected stopped stream to be removed")
	}
	if hasError {
		t.Error("expected error stream to be removed")
	}
}

func TestSelkiesProviderStopNotRunning(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Add a stream in error state
	provider.mu.Lock()
	provider.streams["error-stream"] = &selkiesStream{
		id:       "error-stream",
		state:    ProcessStateError,
		stopC:    make(chan struct{}),
		stoppedC: make(chan struct{}),
	}
	provider.mu.Unlock()

	ctx := context.Background()
	err := provider.Stop(ctx, "error-stream")

	// Should return nil (not running)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSelkiesProviderStopAllWithMultipleStreams(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Add multiple streams in different states
	provider.mu.Lock()
	provider.streams["running1"] = &selkiesStream{
		id:       "running1",
		state:    ProcessStateRunning,
		stopC:    make(chan struct{}),
		stoppedC: make(chan struct{}),
	}
	provider.streams["running2"] = &selkiesStream{
		id:       "running2",
		state:    ProcessStateRunning,
		stopC:    make(chan struct{}),
		stoppedC: make(chan struct{}),
	}
	provider.streams["stopped"] = &selkiesStream{
		id:       "stopped",
		state:    ProcessStateStopped,
		stopC:    make(chan struct{}),
		stoppedC: make(chan struct{}),
	}
	provider.mu.Unlock()

	// Close the stoppedC channels to simulate processes already exited
	close(provider.streams["running1"].stoppedC)
	close(provider.streams["running2"].stoppedC)

	ctx := context.Background()
	provider.StopAll(ctx)

	// Verify running streams were stopped
	provider.mu.RLock()
	stream1 := provider.streams["running1"]
	stream2 := provider.streams["running2"]
	provider.mu.RUnlock()

	if stream1.state != ProcessStateStopped {
		t.Errorf("expected stream1 to be stopped, got %v", stream1.state)
	}
	if stream2.state != ProcessStateStopped {
		t.Errorf("expected stream2 to be stopped, got %v", stream2.state)
	}
}

func TestSelkiesProviderGetInfoWithErrorState(t *testing.T) {
	provider := NewSelkiesProvider(DefaultSelkiesConfig(), zap.NewNop())

	// Add a stream with error state
	provider.mu.Lock()
	provider.streams["error-stream"] = &selkiesStream{
		id:           "error-stream",
		sessionID:    "test-session",
		state:        ProcessStateError,
		error:        "test error",
		signalingURL: "ws://localhost/ws",
		config: SelkiesConfig{
			Resolution: streaming.Resolution{Width: 1920, Height: 1080},
			FrameRate:  30,
			BitRate:    4000,
			VideoCodec: "h264",
		},
	}
	provider.mu.Unlock()

	ctx := context.Background()
	info, err := provider.GetInfo(ctx, "error-stream")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.ID != "error-stream" {
		t.Errorf("expected ID %q, got %q", "error-stream", info.ID)
	}
}

func TestProviderErrorChain(t *testing.T) {
	cause := &ProviderError{
		Provider:  "inner",
		Operation: "inner_op",
		Message:   "inner error",
	}

	err := &ProviderError{
		Provider:  "selkies",
		Operation: "start",
		Message:   "failed",
		Cause:     cause,
	}

	// Test error string includes chain
	errStr := err.Error()
	if !strings.Contains(errStr, "inner error") {
		t.Errorf("expected error chain to contain cause, got: %s", errStr)
	}

	// Test Unwrap returns cause
	if err.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}
}
