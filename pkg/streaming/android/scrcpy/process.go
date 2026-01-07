package scrcpy

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming/android"
)

// Process manages a scrcpy server process and its video/audio streams.
type Process struct {
	deviceSerial string
	options      android.StreamOptions
	config       Config

	// ADB client for device operations
	adb android.ADBClient

	// Process state
	cmd        *exec.Cmd
	videoConn  net.Conn
	audioConn  net.Conn
	localPort  int
	audioPort  int
	forwardSet bool

	// Stream reader
	streamReader *StreamReader

	// Video sink for callbacks
	sink android.VideoSink

	// Logger
	logger *zap.Logger

	// Control
	mu       sync.Mutex
	started  bool
	stopped  bool
	stopOnce sync.Once
	doneCh   chan struct{}
}

// NewProcess creates a new scrcpy process manager.
func NewProcess(
	deviceSerial string,
	options android.StreamOptions,
	config Config,
	sink android.VideoSink,
) *Process {
	return &Process{
		deviceSerial: deviceSerial,
		options:      options.WithDefaults(),
		config:       config.WithDefaults(),
		adb:          config.ADBClient,
		sink:         sink,
		logger:       config.Logger.Named("process"),
		doneCh:       make(chan struct{}),
	}
}

// Start starts the scrcpy server and begins streaming.
func (p *Process) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return fmt.Errorf("process already started")
	}
	p.started = true
	p.mu.Unlock()

	// Allocate local port
	port, err := allocatePort(p.config.BasePort)
	if err != nil {
		return fmt.Errorf("failed to allocate port: %w", err)
	}
	p.localPort = port

	// If audio is enabled, allocate another port
	if p.options.AudioEnabled {
		audioPort, err := allocatePort(p.localPort + 1)
		if err != nil {
			return fmt.Errorf("failed to allocate audio port: %w", err)
		}
		p.audioPort = audioPort
	}

	p.logger.Info("starting scrcpy process",
		zap.String("device", p.deviceSerial),
		zap.Int("port", p.localPort),
		zap.Int("audio_port", p.audioPort),
	)

	// Setup ADB port forwarding
	if err := p.setupForwarding(ctx); err != nil {
		return fmt.Errorf("failed to setup port forwarding: %w", err)
	}

	// Start scrcpy server on device
	if err := p.startServer(ctx); err != nil {
		p.cleanupForwarding(ctx)
		return fmt.Errorf("failed to start scrcpy server: %w", err)
	}

	// Connect to video stream
	if err := p.connectVideo(ctx); err != nil {
		_ = p.Stop()
		return fmt.Errorf("failed to connect to video stream: %w", err)
	}

	// Start stream reading in background
	go p.readLoop()

	p.logger.Info("scrcpy process started successfully",
		zap.String("device", p.deviceSerial),
		zap.Int("port", p.localPort),
	)

	return nil
}

// Stop stops the scrcpy process and cleans up resources.
func (p *Process) Stop() error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopped = true
		p.mu.Unlock()

		close(p.doneCh)

		// Close stream reader
		if p.streamReader != nil {
			_ = p.streamReader.Close()
		}

		// Close connections
		if p.videoConn != nil {
			_ = p.videoConn.Close()
		}
		if p.audioConn != nil {
			_ = p.audioConn.Close()
		}

		// Kill process
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
			// Give it a moment to exit gracefully
			time.Sleep(100 * time.Millisecond)
			_ = p.cmd.Process.Kill()
		}

		// Cleanup port forwarding
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.cleanupForwarding(ctx)

		p.logger.Info("scrcpy process stopped",
			zap.String("device", p.deviceSerial),
		)
	})

	return nil
}

// LocalPort returns the local port for the video stream.
func (p *Process) LocalPort() int {
	return p.localPort
}

// VideoConfig returns the video configuration after stream starts.
func (p *Process) VideoConfig() *android.VideoConfig {
	if p.streamReader != nil {
		return p.streamReader.VideoConfig()
	}
	return nil
}

// AudioConfig returns the audio configuration if audio is enabled.
func (p *Process) AudioConfig() *android.AudioConfig {
	if p.streamReader != nil {
		return p.streamReader.AudioConfig()
	}
	return nil
}

// Stats returns current streaming statistics.
func (p *Process) Stats() *android.StreamStats {
	if p.streamReader != nil {
		return p.streamReader.Stats()
	}
	return nil
}

// Done returns a channel that's closed when the process stops.
func (p *Process) Done() <-chan struct{} {
	return p.doneCh
}

// setupForwarding sets up ADB port forwarding for the scrcpy server.
func (p *Process) setupForwarding(ctx context.Context) error {
	// Forward video port
	if err := p.adb.Forward(ctx, p.deviceSerial, p.localPort, p.localPort); err != nil {
		return fmt.Errorf("failed to forward video port: %w", err)
	}

	// Forward audio port if enabled
	if p.options.AudioEnabled && p.audioPort > 0 {
		if err := p.adb.Forward(ctx, p.deviceSerial, p.audioPort, p.audioPort); err != nil {
			// Cleanup video forward
			_ = p.adb.RemoveForward(ctx, p.deviceSerial, p.localPort)
			return fmt.Errorf("failed to forward audio port: %w", err)
		}
	}

	p.forwardSet = true
	return nil
}

// cleanupForwarding removes ADB port forwarding.
func (p *Process) cleanupForwarding(ctx context.Context) {
	if !p.forwardSet {
		return
	}

	if err := p.adb.RemoveForward(ctx, p.deviceSerial, p.localPort); err != nil {
		p.logger.Warn("failed to remove video port forward",
			zap.Int("port", p.localPort),
			zap.Error(err),
		)
	}

	if p.audioPort > 0 {
		if err := p.adb.RemoveForward(ctx, p.deviceSerial, p.audioPort); err != nil {
			p.logger.Warn("failed to remove audio port forward",
				zap.Int("port", p.audioPort),
				zap.Error(err),
			)
		}
	}
}

// startServer starts the scrcpy server on the device.
func (p *Process) startServer(ctx context.Context) error {
	// Build scrcpy server command
	// The scrcpy-server is pushed to /data/local/tmp/ on the device
	// and executed via app_process

	serverPath := "/data/local/tmp/scrcpy-server"

	// Push server if we have a local copy
	if p.config.ScrcpyServerPath != "" {
		if err := p.pushServer(ctx); err != nil {
			return fmt.Errorf("failed to push scrcpy server: %w", err)
		}
	}

	// Build server arguments
	args := p.buildServerArgs()

	// Use adb shell to start the server
	// Format: adb -s SERIAL shell CLASSPATH=/data/local/tmp/scrcpy-server app_process / com.genymobile.scrcpy.Server <version> <args...>
	shellCmd := fmt.Sprintf(
		"CLASSPATH=%s app_process / com.genymobile.scrcpy.Server 2.4 %s",
		serverPath,
		strings.Join(args, " "),
	)

	cmd := exec.CommandContext(ctx, "adb", "-s", p.deviceSerial, "shell", shellCmd)
	cmd.Stdout = nil // Ignore stdout
	cmd.Stderr = nil // Ignore stderr

	// Start in background
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start scrcpy server: %w", err)
	}

	p.cmd = cmd

	// Wait for server to be ready
	return p.waitForServer(ctx)
}

// pushServer pushes the scrcpy-server binary to the device.
func (p *Process) pushServer(ctx context.Context) error {
	serverPath := p.config.ScrcpyServerPath

	// Check if file exists
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		return fmt.Errorf("scrcpy-server not found at %s", serverPath)
	}

	// Push to device
	destPath := "/data/local/tmp/scrcpy-server"
	cmd := exec.CommandContext(ctx, "adb", "-s", p.deviceSerial, "push", serverPath, destPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb push failed: %s: %w", string(output), err)
	}

	return nil
}

// buildServerArgs builds the scrcpy server command line arguments.
func (p *Process) buildServerArgs() []string {
	var args []string

	// Tunnel forward mode (we handle port forwarding ourselves)
	args = append(args, "tunnel_forward=true")

	// Video options
	if p.options.MaxWidth > 0 || p.options.MaxHeight > 0 {
		maxSize := p.options.MaxWidth
		if p.options.MaxHeight > maxSize {
			maxSize = p.options.MaxHeight
		}
		args = append(args, fmt.Sprintf("max_size=%d", maxSize))
	}

	if p.options.Bitrate > 0 {
		args = append(args, fmt.Sprintf("video_bit_rate=%d", p.options.Bitrate))
	}

	if p.options.MaxFPS > 0 {
		args = append(args, fmt.Sprintf("max_fps=%d", p.options.MaxFPS))
	}

	if p.options.VideoCodec != "" {
		args = append(args, fmt.Sprintf("video_codec=%s", p.options.VideoCodec))
	}

	// Rotation
	if p.options.Rotation != 0 {
		args = append(args, fmt.Sprintf("rotation=%d", p.options.Rotation/90))
	}

	// Audio options
	if p.options.AudioEnabled {
		args = append(args, "audio=true")
		if p.options.AudioCodec != "" {
			args = append(args, fmt.Sprintf("audio_codec=%s", p.options.AudioCodec))
		}
	} else {
		args = append(args, "audio=false")
	}

	// Control mode
	if p.options.NoControl {
		args = append(args, "control=false")
	}

	// Other options
	if p.options.StayAwake {
		args = append(args, "stay_awake=true")
	}

	if p.options.ShowTouches {
		args = append(args, "show_touches=true")
	}

	if p.options.TurnScreenOff {
		args = append(args, "power_off_on_close=true")
	}

	// Scid (stream connection ID) for tunnel mode
	args = append(args, fmt.Sprintf("scid=%08x", p.localPort))

	// Local socket name
	args = append(args, fmt.Sprintf("local_socket_name=scrcpy_%d", p.localPort))

	return args
}

// waitForServer waits for the scrcpy server to be ready.
func (p *Process) waitForServer(ctx context.Context) error {
	deadline := time.Now().Add(p.config.ServerStartTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.doneCh:
			return fmt.Errorf("process stopped")
		default:
		}

		// Try to connect to the server
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", p.localPort), time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for scrcpy server to start")
}

// connectVideo establishes the video stream connection.
func (p *Process) connectVideo(ctx context.Context) error {
	deadline := time.Now().Add(p.config.ConnectionTimeout)

	var conn net.Conn
	var err error

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.doneCh:
			return fmt.Errorf("process stopped")
		default:
		}

		conn, err = net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", p.localPort), time.Second)
		if err == nil {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if conn == nil {
		return fmt.Errorf("failed to connect to video stream: %w", err)
	}

	p.videoConn = conn

	// Create stream reader
	if p.options.AudioEnabled {
		p.streamReader = NewStreamReaderV2(conn, p.sink, true).StreamReader
	} else {
		p.streamReader = NewStreamReader(conn, p.sink)
	}

	return nil
}

// readLoop runs the stream reading loop.
func (p *Process) readLoop() {
	defer func() { _ = p.Stop() }()

	if err := p.streamReader.Start(); err != nil {
		p.logger.Error("stream reader error",
			zap.String("device", p.deviceSerial),
			zap.Error(err),
		)
	}
}

// allocatePort finds an available port starting from basePort.
func allocatePort(basePort int) (int, error) {
	for port := basePort; port < basePort+100; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = listener.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", basePort, basePort+100)
}

// parseVersion parses a version string like "2.4" or "2.4.1".
func parseVersion(s string) (ScrcpyVersion, error) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return ScrcpyVersion{}, fmt.Errorf("invalid version format: %s", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return ScrcpyVersion{}, err
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return ScrcpyVersion{}, err
	}

	patch := 0
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}

	return ScrcpyVersion{Major: major, Minor: minor, Patch: patch}, nil
}
