package browser

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/streaming"
	"go.uber.org/zap"
)

// ProviderName is the name of the browser stream provider.
const ProviderName = "browser-cdp"

// browserStream represents an active browser stream managed by BrowserStreamProvider.
type browserStream struct {
	providerStreamID string
	sessionID        string
	runnerID         string
	tenantID         string

	state     streaming.StreamState
	options   *StreamOptions
	startedAt time.Time
	error     string

	mu sync.RWMutex
}

// BrowserStreamProvider implements streaming.StreamProvider for browser (CDP) streaming.
// It manages browser streams using the Chrome DevTools Protocol.
type BrowserStreamProvider struct {
	mu       sync.RWMutex
	streams  map[string]*browserStream // providerStreamID -> stream
	frameHub *FrameHub
	baseURL  string // Base URL for WebSocket connections (e.g., "ws://localhost:8080")
	logger   *zap.Logger
}

// BrowserStreamProviderConfig contains configuration for the browser stream provider.
type BrowserStreamProviderConfig struct {
	// BaseURL is the base URL for WebSocket connections.
	// The WebSocket URL will be: {BaseURL}/api/v1/streams/{streamID}/ws
	BaseURL string

	// Logger is the logger to use.
	Logger *zap.Logger
}

// NewBrowserStreamProvider creates a new browser stream provider.
func NewBrowserStreamProvider(config BrowserStreamProviderConfig) *BrowserStreamProvider {
	logger := config.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	return &BrowserStreamProvider{
		streams:  make(map[string]*browserStream),
		frameHub: NewFrameHub(logger),
		baseURL:  config.BaseURL,
		logger:   logger.Named("browser-provider"),
	}
}

// Name returns the unique name of this provider.
func (p *BrowserStreamProvider) Name() string {
	return ProviderName
}

// SupportedTypes returns the stream types this provider supports.
func (p *BrowserStreamProvider) SupportedTypes() []streaming.StreamType {
	return []streaming.StreamType{streaming.StreamTypeBrowser}
}

// Start initiates a browser stream with the given options.
// It creates a stream record and returns stream info.
// The actual streaming starts when the agent connects via gRPC.
func (p *BrowserStreamProvider) Start(ctx context.Context, opts streaming.StreamOptions) (*streaming.StreamInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Generate provider stream ID
	providerStreamID := id.New("bstr")

	// Create browser-specific options from streaming options
	browserOpts := &StreamOptions{
		Quality:       80, // Default quality
		MaxFPS:        opts.FrameRate,
		Format:        FormatJPEG,
		MaxWidth:      opts.Resolution.Width,
		MaxHeight:     opts.Resolution.Height,
		EveryNthFrame: 1,
	}

	// Apply defaults
	if browserOpts.MaxFPS == 0 {
		browserOpts.MaxFPS = DefaultMaxFPS
	}

	// Create stream record
	stream := &browserStream{
		providerStreamID: providerStreamID,
		sessionID:        opts.SessionID,
		runnerID:         opts.RunnerID,
		tenantID:         opts.TenantID,
		state:            streaming.StreamStatePending,
		options:          browserOpts,
		startedAt:        time.Now(),
	}

	p.streams[providerStreamID] = stream

	p.logger.Info("browser stream started",
		zap.String("provider_stream_id", providerStreamID),
		zap.String("session_id", opts.SessionID),
		zap.String("runner_id", opts.RunnerID),
	)

	// Build signaling URL (WebSocket endpoint for this stream)
	signalingURL := fmt.Sprintf("%s/api/v1/streams/%s/ws", p.baseURL, providerStreamID)

	return &streaming.StreamInfo{
		ID:           providerStreamID,
		SignalingURL: signalingURL,
		Resolution: streaming.Resolution{
			Width:  browserOpts.MaxWidth,
			Height: browserOpts.MaxHeight,
		},
		FrameRate:  browserOpts.MaxFPS,
		VideoCodec: string(browserOpts.Format), // JPEG/PNG/WebP
		Metadata: map[string]string{
			"quality": fmt.Sprintf("%d", browserOpts.Quality),
		},
	}, nil
}

// Stop stops the stream with the given provider stream ID.
func (p *BrowserStreamProvider) Stop(ctx context.Context, providerStreamID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stream, ok := p.streams[providerStreamID]
	if !ok {
		return streaming.ErrStreamNotFound
	}

	stream.mu.Lock()
	stream.state = streaming.StreamStateStopped
	stream.mu.Unlock()

	// Unregister from FrameHub
	p.frameHub.UnregisterStream(providerStreamID)

	// Remove from streams map
	delete(p.streams, providerStreamID)

	p.logger.Info("browser stream stopped",
		zap.String("provider_stream_id", providerStreamID),
	)

	return nil
}

// GetInfo returns the current info for a stream.
func (p *BrowserStreamProvider) GetInfo(ctx context.Context, providerStreamID string) (*streaming.StreamInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stream, ok := p.streams[providerStreamID]
	if !ok {
		return nil, streaming.ErrStreamNotFound
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	signalingURL := fmt.Sprintf("%s/api/v1/streams/%s/ws", p.baseURL, providerStreamID)

	return &streaming.StreamInfo{
		ID:           providerStreamID,
		SignalingURL: signalingURL,
		Resolution: streaming.Resolution{
			Width:  stream.options.MaxWidth,
			Height: stream.options.MaxHeight,
		},
		FrameRate:  stream.options.MaxFPS,
		VideoCodec: string(stream.options.Format),
		Metadata: map[string]string{
			"quality": fmt.Sprintf("%d", stream.options.Quality),
			"state":   string(stream.state),
		},
	}, nil
}

// FrameHub returns the FrameHub for this provider.
// Used by gRPC handlers to register stream connections.
func (p *BrowserStreamProvider) FrameHub() *FrameHub {
	return p.frameHub
}

// SetStreamState updates the state of a stream.
// Called by gRPC handlers when stream state changes.
func (p *BrowserStreamProvider) SetStreamState(providerStreamID string, state streaming.StreamState, err string) error {
	p.mu.RLock()
	stream, ok := p.streams[providerStreamID]
	p.mu.RUnlock()

	if !ok {
		return streaming.ErrStreamNotFound
	}

	stream.mu.Lock()
	stream.state = state
	stream.error = err
	stream.mu.Unlock()

	p.logger.Debug("stream state updated",
		zap.String("provider_stream_id", providerStreamID),
		zap.String("state", string(state)),
	)

	return nil
}

// GetStreamState returns the current state of a stream.
func (p *BrowserStreamProvider) GetStreamState(providerStreamID string) (streaming.StreamState, error) {
	p.mu.RLock()
	stream, ok := p.streams[providerStreamID]
	p.mu.RUnlock()

	if !ok {
		return "", streaming.ErrStreamNotFound
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	return stream.state, nil
}

// ValidateStream checks if a stream exists and is valid for the given runner.
func (p *BrowserStreamProvider) ValidateStream(providerStreamID, runnerID, sessionID string) error {
	p.mu.RLock()
	stream, ok := p.streams[providerStreamID]
	p.mu.RUnlock()

	if !ok {
		return streaming.ErrStreamNotFound
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()

	if stream.runnerID != runnerID {
		return fmt.Errorf("runner ID mismatch: expected %s, got %s", stream.runnerID, runnerID)
	}

	if stream.sessionID != sessionID {
		return fmt.Errorf("session ID mismatch: expected %s, got %s", stream.sessionID, sessionID)
	}

	return nil
}

// ListStreams returns all active stream IDs.
func (p *BrowserStreamProvider) ListStreams() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]string, 0, len(p.streams))
	for streamID := range p.streams {
		result = append(result, streamID)
	}
	return result
}

// Close closes the provider and all active streams.
func (p *BrowserStreamProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close FrameHub
	p.frameHub.Close()

	// Clear all streams
	p.streams = make(map[string]*browserStream)

	p.logger.Info("browser stream provider closed")

	return nil
}

// Ensure BrowserStreamProvider implements streaming.StreamProvider
var _ streaming.StreamProvider = (*BrowserStreamProvider)(nil)
