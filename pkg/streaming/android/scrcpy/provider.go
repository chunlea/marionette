// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package scrcpy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/streaming/android"
)

// Provider implements the android.Provider interface using scrcpy.
// It manages Android device streaming through scrcpy's screen mirroring
// capabilities with support for video and audio streaming.
type Provider struct {
	config Config
	adb    android.ADBClient
	logger *zap.Logger

	// Active streams
	streams   map[string]*streamEntry
	streamsMu sync.RWMutex

	// Shutdown
	closed  bool
	closeMu sync.RWMutex
}

// streamEntry holds an active stream and its process.
type streamEntry struct {
	info    *android.StreamInfo
	process *Process
	sink    android.VideoSink
}

// New creates a new scrcpy provider.
func New(config Config) (*Provider, error) {
	config = config.WithDefaults()

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create or use provided ADB client
	var adbClient android.ADBClient
	if config.ADBClient != nil {
		adbClient = config.ADBClient
	} else {
		adbCfg := android.ADBClientConfig{
			ADBPath: config.ADBPath,
			Logger:  config.Logger,
		}
		var err error
		adbClient, err = android.NewADBClient(adbCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create ADB client: %w", err)
		}
	}

	// Update config with ADB client
	config.ADBClient = adbClient

	return &Provider{
		config:  config,
		adb:     adbClient,
		logger:  config.Logger.Named("scrcpy"),
		streams: make(map[string]*streamEntry),
	}, nil
}

// ListDevices returns all connected Android devices.
func (p *Provider) ListDevices(ctx context.Context) ([]android.Device, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, &android.StreamError{Op: "list_devices", Err: fmt.Errorf("provider closed")}
	}
	p.closeMu.RUnlock()

	devices, err := p.adb.ListDevices(ctx)
	if err != nil {
		return nil, &android.ADBError{Command: "devices", Err: err}
	}

	return devices, nil
}

// GetDevice returns a specific device by serial number.
func (p *Provider) GetDevice(ctx context.Context, serial string) (*android.Device, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, &android.StreamError{Op: "get_device", Err: fmt.Errorf("provider closed")}
	}
	p.closeMu.RUnlock()

	info, err := p.adb.GetDeviceInfo(ctx, serial)
	if err != nil {
		return nil, &android.DeviceNotFoundError{Serial: serial}
	}

	return info, nil
}

// StartStream starts screen streaming for a device.
func (p *Provider) StartStream(ctx context.Context, opts android.StreamOptions) (*android.StreamInfo, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, &android.StreamError{Op: "start_stream", Err: fmt.Errorf("provider closed")}
	}
	p.closeMu.RUnlock()

	// Validate options
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Check device exists and is ready
	device, err := p.GetDevice(ctx, opts.DeviceSerial)
	if err != nil {
		return nil, err
	}

	if !device.State.IsConnected() {
		return nil, &android.DeviceStateError{
			Serial:   opts.DeviceSerial,
			State:    device.State,
			Expected: android.DeviceStateDevice,
		}
	}

	// Generate stream ID (using unified stream prefix)
	streamID := id.Stream()

	// Create stream info
	now := time.Now()
	info := &android.StreamInfo{
		ID:        streamID,
		Device:    device,
		State:     android.StreamStateStarting,
		Options:   &opts,
		StartedAt: &now,
		Stats: &android.StreamStats{
			UpdatedAt: now,
		},
	}

	// Create a video sink that we can use later
	// For now, we'll use a buffered sink that can be connected to later
	sink := newBufferedSink()

	// Create and start the scrcpy process
	process := NewProcess(opts.DeviceSerial, opts, p.config, sink)

	if err := process.Start(ctx); err != nil {
		return nil, &android.ScrcpyError{
			Serial: opts.DeviceSerial,
			Err:    err,
		}
	}

	// Update info with actual values
	info.LocalPort = process.LocalPort()
	info.State = android.StreamStateRunning

	// Wait a bit for video config
	time.Sleep(500 * time.Millisecond)
	if vc := process.VideoConfig(); vc != nil {
		info.Width = vc.Width
		info.Height = vc.Height
	}

	// Store stream entry
	entry := &streamEntry{
		info:    info,
		process: process,
		sink:    sink,
	}

	p.streamsMu.Lock()
	p.streams[streamID] = entry
	p.streamsMu.Unlock()

	// Monitor stream lifecycle
	go p.monitorStream(streamID, process)

	p.logger.Info("stream started",
		zap.String("stream_id", streamID),
		zap.String("device", opts.DeviceSerial),
		zap.Int("port", info.LocalPort),
		zap.Int("width", info.Width),
		zap.Int("height", info.Height),
	)

	return info, nil
}

// StopStream stops an active stream.
func (p *Provider) StopStream(ctx context.Context, streamID string) error {
	p.streamsMu.Lock()
	entry, exists := p.streams[streamID]
	if !exists {
		p.streamsMu.Unlock()
		return &android.StreamError{
			Op:       "stop",
			StreamID: streamID,
			Err:      fmt.Errorf("stream not found"),
		}
	}
	delete(p.streams, streamID)
	p.streamsMu.Unlock()

	// Stop the process
	if err := entry.process.Stop(); err != nil {
		p.logger.Warn("error stopping stream process",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
	}

	// Close the sink
	if bs, ok := entry.sink.(*bufferedSink); ok {
		bs.Close()
	}

	entry.info.State = android.StreamStateStopped

	p.logger.Info("stream stopped",
		zap.String("stream_id", streamID),
	)

	return nil
}

// GetStream returns information about an active stream.
func (p *Provider) GetStream(ctx context.Context, streamID string) (*android.StreamInfo, error) {
	p.streamsMu.RLock()
	entry, exists := p.streams[streamID]
	p.streamsMu.RUnlock()

	if !exists {
		return nil, &android.StreamError{
			Op:       "get",
			StreamID: streamID,
			Err:      fmt.Errorf("stream not found"),
		}
	}

	// Update stats from process
	if stats := entry.process.Stats(); stats != nil {
		entry.info.Stats = stats
	}

	// Copy to avoid mutation
	infoCopy := *entry.info
	return &infoCopy, nil
}

// ListStreams returns all active streams.
func (p *Provider) ListStreams(ctx context.Context) ([]*android.StreamInfo, error) {
	p.streamsMu.RLock()
	defer p.streamsMu.RUnlock()

	streams := make([]*android.StreamInfo, 0, len(p.streams))
	for _, entry := range p.streams {
		// Update stats
		if stats := entry.process.Stats(); stats != nil {
			entry.info.Stats = stats
		}
		// Copy to avoid mutation
		infoCopy := *entry.info
		streams = append(streams, &infoCopy)
	}

	return streams, nil
}

// SendInput sends an input event to a device.
func (p *Provider) SendInput(ctx context.Context, serial string, event android.InputEvent) error {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return &android.StreamError{Op: "send_input", Err: fmt.Errorf("provider closed")}
	}
	p.closeMu.RUnlock()

	// Validate input
	if err := event.Validate(); err != nil {
		return err
	}

	// Send via ADB
	switch event.Type {
	case android.InputTypeTap:
		return p.adb.InputTap(ctx, serial, event.X, event.Y)

	case android.InputTypeSwipe:
		duration := event.Duration
		if duration == 0 {
			duration = 300 // Default 300ms
		}
		return p.adb.InputSwipe(ctx, serial, event.X, event.Y, event.EndX, event.EndY, duration)

	case android.InputTypeLongPress:
		duration := event.Duration
		if duration == 0 {
			duration = 1000 // Default 1s
		}
		// Long press is a swipe to the same point
		return p.adb.InputSwipe(ctx, serial, event.X, event.Y, event.X, event.Y, duration)

	case android.InputTypeText:
		return p.adb.InputText(ctx, serial, event.Text)

	case android.InputTypeKey:
		keyCode := event.KeyCode
		if keyCode == 0 && event.KeyName != "" {
			if code, ok := android.GetKeyCode(event.KeyName); ok {
				keyCode = code
			}
		}
		return p.adb.InputKeyEvent(ctx, serial, keyCode)

	case android.InputTypeScroll:
		// Scroll is implemented as swipe in the scroll direction
		endY := event.Y - event.ScrollY*10 // Scale scroll amount
		return p.adb.InputSwipe(ctx, serial, event.X, event.Y, event.X, endY, 100)

	case android.InputTypePinch:
		// Pinch requires multi-touch which isn't supported via adb input
		return &android.InvalidInputError{
			Type:    event.Type,
			Message: "pinch gesture not supported via ADB input",
		}

	default:
		return &android.InvalidInputError{
			Type:    event.Type,
			Message: "unknown input type",
		}
	}
}

// Close releases all resources and stops all streams.
func (p *Provider) Close() error {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return nil
	}
	p.closed = true
	p.closeMu.Unlock()

	p.logger.Info("closing provider")

	// Stop all streams
	p.streamsMu.Lock()
	streams := make([]*streamEntry, 0, len(p.streams))
	for _, entry := range p.streams {
		streams = append(streams, entry)
	}
	p.streams = make(map[string]*streamEntry)
	p.streamsMu.Unlock()

	for _, entry := range streams {
		if err := entry.process.Stop(); err != nil {
			p.logger.Warn("error stopping stream on close",
				zap.String("stream_id", entry.info.ID),
				zap.Error(err),
			)
		}
	}

	p.logger.Info("provider closed")
	return nil
}

// GetVideoSink returns the video sink for a stream, allowing external consumers
// to receive video data.
func (p *Provider) GetVideoSink(streamID string) (android.VideoSink, error) {
	p.streamsMu.RLock()
	entry, exists := p.streams[streamID]
	p.streamsMu.RUnlock()

	if !exists {
		return nil, &android.StreamError{
			Op:       "get_sink",
			StreamID: streamID,
			Err:      fmt.Errorf("stream not found"),
		}
	}

	return entry.sink, nil
}

// SetVideoSink sets a new video sink for a stream, replacing the buffered sink.
func (p *Provider) SetVideoSink(streamID string, sink android.VideoSink) error {
	p.streamsMu.Lock()
	entry, exists := p.streams[streamID]
	if !exists {
		p.streamsMu.Unlock()
		return &android.StreamError{
			Op:       "set_sink",
			StreamID: streamID,
			Err:      fmt.Errorf("stream not found"),
		}
	}

	// Connect new sink to buffered sink
	if bs, ok := entry.sink.(*bufferedSink); ok {
		bs.Connect(sink)
	}
	p.streamsMu.Unlock()

	return nil
}

// monitorStream monitors a stream and cleans up when it stops.
func (p *Provider) monitorStream(streamID string, process *Process) {
	<-process.Done()

	p.streamsMu.Lock()
	entry, exists := p.streams[streamID]
	if exists {
		entry.info.State = android.StreamStateStopped
		delete(p.streams, streamID)
	}
	p.streamsMu.Unlock()

	if exists {
		p.logger.Info("stream ended",
			zap.String("stream_id", streamID),
		)
	}
}

// bufferedSink is a video sink that buffers data and forwards to a connected sink.
// This allows starting streams before consumers are connected.
type bufferedSink struct {
	mu     sync.RWMutex
	target android.VideoSink

	// Config caching
	videoWidth, videoHeight int
	videoCodec              string
	videoConfig             []byte
	audioSampleRate         int
	audioChannels           int
	audioCodec              string
	audioConfig             []byte

	closed bool
}

// newBufferedSink creates a new buffered sink.
func newBufferedSink() *bufferedSink {
	return &bufferedSink{}
}

// Connect sets the target sink to forward data to.
func (s *bufferedSink) Connect(target android.VideoSink) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.target = target

	// Send cached config if available
	if target != nil {
		if s.videoWidth > 0 && s.videoHeight > 0 {
			_ = target.OnVideoConfig(s.videoWidth, s.videoHeight, s.videoCodec, s.videoConfig)
		}
		if s.audioSampleRate > 0 {
			_ = target.OnAudioConfig(s.audioSampleRate, s.audioChannels, s.audioCodec, s.audioConfig)
		}
	}
}

// Close marks the sink as closed.
func (s *bufferedSink) Close() {
	s.mu.Lock()
	s.closed = true
	target := s.target
	s.mu.Unlock()

	if target != nil {
		target.OnClose()
	}
}

// OnVideoData forwards video data to the connected sink.
func (s *bufferedSink) OnVideoData(data []byte) error {
	s.mu.RLock()
	target := s.target
	s.mu.RUnlock()

	if target != nil {
		return target.OnVideoData(data)
	}
	return nil
}

// OnVideoConfig caches and forwards video config.
func (s *bufferedSink) OnVideoConfig(width, height int, codec string, config []byte) error {
	s.mu.Lock()
	s.videoWidth = width
	s.videoHeight = height
	s.videoCodec = codec
	s.videoConfig = config
	target := s.target
	s.mu.Unlock()

	if target != nil {
		return target.OnVideoConfig(width, height, codec, config)
	}
	return nil
}

// OnAudioData forwards audio data to the connected sink.
func (s *bufferedSink) OnAudioData(data []byte) error {
	s.mu.RLock()
	target := s.target
	s.mu.RUnlock()

	if target != nil {
		return target.OnAudioData(data)
	}
	return nil
}

// OnAudioConfig caches and forwards audio config.
func (s *bufferedSink) OnAudioConfig(sampleRate, channels int, codec string, config []byte) error {
	s.mu.Lock()
	s.audioSampleRate = sampleRate
	s.audioChannels = channels
	s.audioCodec = codec
	s.audioConfig = config
	target := s.target
	s.mu.Unlock()

	if target != nil {
		return target.OnAudioConfig(sampleRate, channels, codec, config)
	}
	return nil
}

// OnError forwards errors to the connected sink.
func (s *bufferedSink) OnError(err error) {
	s.mu.RLock()
	target := s.target
	s.mu.RUnlock()

	if target != nil {
		target.OnError(err)
	}
}

// OnClose forwards close notification to the connected sink.
func (s *bufferedSink) OnClose() {
	s.mu.RLock()
	target := s.target
	s.mu.RUnlock()

	if target != nil {
		target.OnClose()
	}
}

// Ensure bufferedSink implements VideoSink.
var _ android.VideoSink = (*bufferedSink)(nil)

// Ensure Provider implements Provider interface.
var _ android.Provider = (*Provider)(nil)
