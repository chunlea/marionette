package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

// SelkiesConfig contains configuration for the Selkies provider.
type SelkiesConfig struct {
	// ExecutablePath is the path to the selkies-gstreamer executable.
	// Default: "selkies-gstreamer" (searches PATH)
	ExecutablePath string

	// SignalingURL is the WebSocket URL for WebRTC signaling.
	SignalingURL string

	// Display is the X11 display to capture.
	Display string

	// Resolution is the stream resolution.
	Resolution streaming.Resolution

	// FrameRate is the target frame rate.
	FrameRate int

	// BitRate is the target bitrate in kbps.
	BitRate int

	// VideoCodec specifies the video codec.
	// Supported: "h264", "vp8", "vp9", "av1"
	VideoCodec string

	// HardwareAcceleration enables hardware encoding.
	HardwareAcceleration bool

	// AudioEnabled enables audio capture.
	AudioEnabled bool

	// AudioDevice is the PulseAudio/PipeWire device to capture.
	AudioDevice string

	// EnableInput enables input forwarding.
	EnableInput bool

	// ICEServers are the STUN/TURN servers.
	ICEServers []streaming.ICEServer

	// StartupTimeout is how long to wait for the process to start.
	StartupTimeout time.Duration

	// ShutdownTimeout is how long to wait for graceful shutdown.
	ShutdownTimeout time.Duration

	// LogOutput is where to write process logs.
	// Default: discard
	LogOutput io.Writer
}

// DefaultSelkiesConfig returns a configuration with sensible defaults.
func DefaultSelkiesConfig() SelkiesConfig {
	return SelkiesConfig{
		ExecutablePath:       "selkies-gstreamer",
		Display:              ":99",
		Resolution:           streaming.Resolution{Width: 1920, Height: 1080},
		FrameRate:            30,
		BitRate:              4000,
		VideoCodec:           "h264",
		HardwareAcceleration: true,
		AudioEnabled:         false,
		EnableInput:          true,
		StartupTimeout:       30 * time.Second,
		ShutdownTimeout:      10 * time.Second,
		ICEServers: []streaming.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
}

// SelkiesProvider implements StreamProvider using Selkies GStreamer.
type SelkiesProvider struct {
	config SelkiesConfig
	logger *zap.Logger

	mu      sync.RWMutex
	streams map[string]*selkiesStream
}

// selkiesStream represents a running Selkies stream.
type selkiesStream struct {
	id           string
	sessionID    string
	runnerID     string
	config       SelkiesConfig
	cmd          *exec.Cmd
	state        ProcessState
	signalingURL string
	startedAt    time.Time
	stoppedAt    *time.Time
	error        string

	stopC    chan struct{}
	stoppedC chan struct{}
}

// NewSelkiesProvider creates a new Selkies provider.
func NewSelkiesProvider(config SelkiesConfig, logger *zap.Logger) *SelkiesProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &SelkiesProvider{
		config:  config,
		logger:  logger.Named("selkies"),
		streams: make(map[string]*selkiesStream),
	}
}

// Name returns the provider name.
func (p *SelkiesProvider) Name() string {
	return "selkies"
}

// SupportedTypes returns the supported stream types.
func (p *SelkiesProvider) SupportedTypes() []streaming.StreamType {
	return []streaming.StreamType{streaming.StreamTypeDesktop}
}

// Start starts a new desktop stream.
func (p *SelkiesProvider) Start(ctx context.Context, opts streaming.StreamOptions) (*streaming.StreamInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if stream already exists for this session
	for _, s := range p.streams {
		if s.sessionID == opts.SessionID && s.state == ProcessStateRunning {
			return nil, fmt.Errorf("stream already exists for session %s", opts.SessionID)
		}
	}

	// Generate stream ID
	streamID := fmt.Sprintf("selkies_%d", time.Now().UnixNano())

	// Merge options with defaults
	config := p.mergeConfig(opts)

	// Build signaling URL
	signalingURL := config.SignalingURL
	if signalingURL == "" {
		signalingURL = fmt.Sprintf("ws://localhost:8080/ws?stream=%s", streamID)
	}

	// Create stream record
	stream := &selkiesStream{
		id:           streamID,
		sessionID:    opts.SessionID,
		runnerID:     opts.RunnerID,
		config:       config,
		state:        ProcessStateStarting,
		signalingURL: signalingURL,
		stopC:        make(chan struct{}),
		stoppedC:     make(chan struct{}),
	}

	p.streams[streamID] = stream

	// Start the process
	if err := p.startProcess(ctx, stream); err != nil {
		stream.state = ProcessStateError
		stream.error = err.Error()
		return nil, &ProviderError{
			Provider:  "selkies",
			Operation: "start",
			Message:   "failed to start process",
			Cause:     err,
		}
	}

	stream.state = ProcessStateRunning
	stream.startedAt = time.Now()

	p.logger.Info("started stream",
		zap.String("stream_id", streamID),
		zap.String("session_id", opts.SessionID),
		zap.String("display", config.Display),
	)

	// Return StreamInfo matching main's interface
	return &streaming.StreamInfo{
		ID:           streamID,
		SignalingURL: signalingURL,
		Resolution:   config.Resolution,
		FrameRate:    config.FrameRate,
		BitRate:      config.BitRate,
		VideoCodec:   config.VideoCodec,
		Metadata: map[string]string{
			"provider":      "selkies",
			"display":       config.Display,
			"audio_enabled": fmt.Sprintf("%t", config.AudioEnabled),
			"input_enabled": fmt.Sprintf("%t", config.EnableInput),
		},
	}, nil
}

// Stop stops a running stream.
func (p *SelkiesProvider) Stop(ctx context.Context, providerStreamID string) error {
	p.mu.Lock()
	stream, ok := p.streams[providerStreamID]
	if !ok {
		p.mu.Unlock()
		return &ProviderError{
			Provider:  "selkies",
			Operation: "stop",
			Message:   "stream not found",
		}
	}
	p.mu.Unlock()

	if stream.state != ProcessStateRunning {
		return nil // Already stopped
	}

	// Signal stop
	close(stream.stopC)

	// Wait for process to exit with timeout
	select {
	case <-stream.stoppedC:
		// Process exited
	case <-time.After(p.config.ShutdownTimeout):
		p.logger.Warn("stream shutdown timeout, force killing",
			zap.String("stream_id", providerStreamID),
		)
		if stream.cmd != nil && stream.cmd.Process != nil {
			_ = stream.cmd.Process.Kill()
		}
	}

	p.mu.Lock()
	now := time.Now()
	stream.stoppedAt = &now
	stream.state = ProcessStateStopped
	p.mu.Unlock()

	p.logger.Info("stopped stream", zap.String("stream_id", providerStreamID))

	return nil
}

// GetInfo returns the current info for a stream.
func (p *SelkiesProvider) GetInfo(ctx context.Context, providerStreamID string) (*streaming.StreamInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stream, ok := p.streams[providerStreamID]
	if !ok {
		return nil, &ProviderError{
			Provider:  "selkies",
			Operation: "get_info",
			Message:   "stream not found",
		}
	}

	metadata := map[string]string{
		"provider":      "selkies",
		"display":       stream.config.Display,
		"audio_enabled": fmt.Sprintf("%t", stream.config.AudioEnabled),
		"input_enabled": fmt.Sprintf("%t", stream.config.EnableInput),
		"state":         string(stream.state),
	}
	if stream.error != "" {
		metadata["error"] = stream.error
	}

	return &streaming.StreamInfo{
		ID:           stream.id,
		SignalingURL: stream.signalingURL,
		Resolution:   stream.config.Resolution,
		FrameRate:    stream.config.FrameRate,
		BitRate:      stream.config.BitRate,
		VideoCodec:   stream.config.VideoCodec,
		Metadata:     metadata,
	}, nil
}

// GetDisplayInfo returns information about available displays.
func (p *SelkiesProvider) GetDisplayInfo(ctx context.Context) ([]DisplayInfo, error) {
	// Try to detect X11 displays
	displays := []DisplayInfo{}

	// Check DISPLAY environment variable
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}

	// Try to get display info via xdpyinfo
	cmd := exec.CommandContext(ctx, "xdpyinfo", "-display", display)
	output, _ := cmd.Output()
	if len(output) == 0 {
		// Return default display info when xdpyinfo fails or returns empty
		return []DisplayInfo{
			{
				Name:        display,
				Width:       1920,
				Height:      1080,
				RefreshRate: 60,
				IsPrimary:   true,
			},
		}, nil
	}

	// Parse xdpyinfo output (simplified)
	info := DisplayInfo{
		Name:        display,
		Width:       1920,
		Height:      1080,
		RefreshRate: 60,
		IsPrimary:   true,
	}

	// TODO: Parse output for actual dimensions
	_ = output

	displays = append(displays, info)
	return displays, nil
}

// SetDisplay changes the display being captured.
func (p *SelkiesProvider) SetDisplay(ctx context.Context, streamID string, display string) error {
	// Would need to restart the stream with new display
	return &ProviderError{
		Provider:  "selkies",
		Operation: "set_display",
		Message:   "runtime display change not supported",
	}
}

// GetCapabilities returns the provider's capabilities.
func (p *SelkiesProvider) GetCapabilities(ctx context.Context) (*Capabilities, error) {
	caps := &Capabilities{
		VideoCodecs:     []string{"h264", "vp8", "vp9"},
		MaxResolution:   streaming.Resolution{Width: 4096, Height: 2160},
		MaxFrameRate:    60,
		AudioCapture:    true,
		InputForwarding: true,
	}

	// Detect hardware acceleration
	caps.HardwareAcceleration, caps.SupportedAccelerators = p.detectHardwareAccel(ctx)

	return caps, nil
}

// InputHandler returns the input handler for this provider.
func (p *SelkiesProvider) InputHandler() InputHandler {
	// Selkies handles input through its WebRTC data channel
	// We don't need a separate handler
	return nil
}

// startProcess starts the Selkies process.
func (p *SelkiesProvider) startProcess(ctx context.Context, stream *selkiesStream) error {
	config := stream.config

	// Build command line arguments
	args := p.buildArgs(stream)

	// Build environment
	env := p.buildEnv(stream)

	// Create command
	cmd := exec.CommandContext(ctx, config.ExecutablePath, args...)
	cmd.Env = append(os.Environ(), env...)

	// Set up output handling
	if config.LogOutput != nil {
		cmd.Stdout = config.LogOutput
		cmd.Stderr = config.LogOutput
	}

	// Set process group for clean shutdown
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	stream.cmd = cmd

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting process: %w", err)
	}

	// Start goroutine to monitor process
	go p.monitorProcess(stream)

	// Wait for process to be ready (simplified check)
	select {
	case <-time.After(config.StartupTimeout):
		// Check if process is still running
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return fmt.Errorf("process exited during startup")
		}
		return nil
	case <-stream.stopC:
		return fmt.Errorf("stop requested during startup")
	case <-stream.stoppedC:
		return fmt.Errorf("process exited unexpectedly")
	}
}

// monitorProcess monitors the process and handles cleanup.
func (p *SelkiesProvider) monitorProcess(stream *selkiesStream) {
	defer close(stream.stoppedC)

	cmd := stream.cmd
	if cmd == nil {
		return
	}

	// Wait for stop signal or process exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-stream.stopC:
		// Stop requested - send SIGTERM
		stream.state = ProcessStateStopping
		if cmd.Process != nil {
			// Send SIGTERM to process group
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		// Wait for process to exit
		select {
		case err := <-done:
			if err != nil {
				p.logger.Debug("process exited with error", zap.Error(err))
			}
		case <-time.After(p.config.ShutdownTimeout):
			// Force kill
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
		}
	case err := <-done:
		// Process exited on its own
		p.mu.Lock()
		if stream.state == ProcessStateRunning {
			stream.state = ProcessStateError
			if err != nil {
				stream.error = err.Error()
			} else {
				stream.error = "process exited unexpectedly"
			}
			p.logger.Error("stream process exited unexpectedly",
				zap.String("stream_id", stream.id),
				zap.Error(err),
			)
		}
		p.mu.Unlock()
	}
}

// buildArgs builds command line arguments for selkies-gstreamer.
func (p *SelkiesProvider) buildArgs(stream *selkiesStream) []string {
	config := stream.config
	args := []string{}

	// Display
	if config.Display != "" {
		args = append(args, "--display", config.Display)
	}

	// Resolution
	if config.Resolution.Width > 0 && config.Resolution.Height > 0 {
		args = append(args,
			"--video-width", fmt.Sprintf("%d", config.Resolution.Width),
			"--video-height", fmt.Sprintf("%d", config.Resolution.Height),
		)
	}

	// Frame rate
	if config.FrameRate > 0 {
		args = append(args, "--framerate", fmt.Sprintf("%d", config.FrameRate))
	}

	// Bitrate
	if config.BitRate > 0 {
		args = append(args, "--video-bitrate", fmt.Sprintf("%d", config.BitRate))
	}

	// Video codec
	if config.VideoCodec != "" {
		args = append(args, "--encoder", config.VideoCodec)
	}

	// Hardware acceleration
	if config.HardwareAcceleration {
		args = append(args, "--enable-hw-accel")
	}

	// Audio
	if config.AudioEnabled {
		args = append(args, "--enable-audio")
		if config.AudioDevice != "" {
			args = append(args, "--audio-device", config.AudioDevice)
		}
	}

	// Signaling URL
	if config.SignalingURL != "" {
		args = append(args, "--signaling-server", config.SignalingURL)
	}

	return args
}

// buildEnv builds environment variables for the process.
func (p *SelkiesProvider) buildEnv(stream *selkiesStream) []string {
	config := stream.config
	env := []string{}

	// Display
	if config.Display != "" {
		env = append(env, "DISPLAY="+config.Display)
	}

	// ICE servers as JSON
	if len(config.ICEServers) > 0 {
		iceJSON, _ := json.Marshal(config.ICEServers)
		env = append(env, "SELKIES_TURN_SERVERS="+string(iceJSON))
	}

	return env
}

// detectHardwareAccel detects available hardware acceleration.
func (p *SelkiesProvider) detectHardwareAccel(ctx context.Context) (bool, []string) {
	accelerators := []string{}

	// Check for VAAPI
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
		accelerators = append(accelerators, "vaapi")
	}

	// Check for NVENC (NVIDIA)
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		accelerators = append(accelerators, "nvenc")
	}

	// Check for Intel Quick Sync
	if output, err := exec.CommandContext(ctx, "grep", "-i", "intel", "/proc/cpuinfo").Output(); err == nil && len(output) > 0 {
		if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
			accelerators = append(accelerators, "qsv")
		}
	}

	return len(accelerators) > 0, accelerators
}

// mergeConfig merges stream options with provider config.
func (p *SelkiesProvider) mergeConfig(opts streaming.StreamOptions) SelkiesConfig {
	config := p.config

	if opts.Resolution.Width > 0 && opts.Resolution.Height > 0 {
		config.Resolution = opts.Resolution
	}
	if opts.FrameRate > 0 {
		config.FrameRate = opts.FrameRate
	}
	if opts.BitRate > 0 {
		config.BitRate = opts.BitRate
	}
	if len(opts.ICEServers) > 0 {
		config.ICEServers = opts.ICEServers
	}
	config.AudioEnabled = opts.AudioEnabled
	config.EnableInput = opts.InputEnabled

	return config
}

// Cleanup removes all stopped streams from memory.
func (p *SelkiesProvider) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, stream := range p.streams {
		if stream.state == ProcessStateStopped || stream.state == ProcessStateError {
			delete(p.streams, id)
		}
	}
}

// StopAll stops all running streams.
func (p *SelkiesProvider) StopAll(ctx context.Context) {
	p.mu.RLock()
	ids := make([]string, 0, len(p.streams))
	for id, stream := range p.streams {
		if stream.state == ProcessStateRunning {
			ids = append(ids, id)
		}
	}
	p.mu.RUnlock()

	for _, id := range ids {
		if err := p.Stop(ctx, id); err != nil {
			p.logger.Error("failed to stop stream",
				zap.String("stream_id", id),
				zap.Error(err),
			)
		}
	}
}

// GetConfigPath returns the path to the Selkies config file.
func GetConfigPath() string {
	// Check common locations
	paths := []string{
		"/etc/selkies/config.yaml",
		filepath.Join(os.Getenv("HOME"), ".config/selkies/config.yaml"),
		"./selkies-config.yaml",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
