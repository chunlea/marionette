package streaming

import (
	"context"
	"fmt"
	"sync"
)

// SFUProvider is a server-side provider that uses the SFU for WebRTC streaming.
// It supports all stream types and creates SFU rooms for each stream.
// The actual video/audio publishing is handled by agents.
type SFUProvider struct {
	signalingBaseURL string
	mu               sync.RWMutex
	streams          map[string]*sfuStreamState
}

type sfuStreamState struct {
	id          string
	sessionID   string
	runnerID    string
	streamType  StreamType
	state       StreamState
	signalingID string
}

// SFUProviderConfig configures the SFU provider.
type SFUProviderConfig struct {
	// SignalingBaseURL is the base URL for WebSocket signaling.
	// Example: "ws://localhost:8081/admin/api/v1/signaling"
	SignalingBaseURL string
}

// NewSFUProvider creates a new SFU-based stream provider.
func NewSFUProvider(config SFUProviderConfig) *SFUProvider {
	return &SFUProvider{
		signalingBaseURL: config.SignalingBaseURL,
		streams:          make(map[string]*sfuStreamState),
	}
}

// Name returns the provider name.
func (p *SFUProvider) Name() string {
	return "sfu"
}

// SupportedTypes returns the stream types this provider supports.
func (p *SFUProvider) SupportedTypes() []StreamType {
	return []StreamType{
		StreamTypeDesktop,
		StreamTypeBrowser,
		StreamTypeIOS,
		StreamTypeAndroid,
	}
}

// Start initiates a stream.
func (p *SFUProvider) Start(ctx context.Context, opts StreamOptions) (*StreamInfo, error) {
	if opts.SessionID == "" {
		return nil, ErrSessionRequired
	}

	// Generate a unique stream ID for this provider
	providerStreamID := fmt.Sprintf("sfu_%s_%d", opts.SessionID, len(p.streams))

	// Build signaling URL
	signalingURL := fmt.Sprintf("%s?stream_id=%s", p.signalingBaseURL, providerStreamID)

	// Create stream state
	state := &sfuStreamState{
		id:          providerStreamID,
		sessionID:   opts.SessionID,
		runnerID:    opts.RunnerID,
		streamType:  opts.Type,
		state:       StreamStateStarting,
		signalingID: providerStreamID,
	}

	p.mu.Lock()
	p.streams[providerStreamID] = state
	p.mu.Unlock()

	// Return stream info
	info := &StreamInfo{
		ID:           providerStreamID,
		SignalingURL: signalingURL,
		Resolution:   opts.Resolution,
		FrameRate:    opts.FrameRate,
		BitRate:      opts.BitRate,
	}

	return info, nil
}

// Stop stops a stream.
func (p *SFUProvider) Stop(ctx context.Context, providerStreamID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	state, exists := p.streams[providerStreamID]
	if !exists {
		return ErrStreamNotFound
	}

	state.state = StreamStateStopped
	delete(p.streams, providerStreamID)

	return nil
}

// GetInfo returns info about a stream.
func (p *SFUProvider) GetInfo(ctx context.Context, providerStreamID string) (*StreamInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	_, exists := p.streams[providerStreamID]
	if !exists {
		return nil, ErrStreamNotFound
	}

	signalingURL := fmt.Sprintf("%s?stream_id=%s", p.signalingBaseURL, providerStreamID)

	return &StreamInfo{
		ID:           providerStreamID,
		SignalingURL: signalingURL,
	}, nil
}
