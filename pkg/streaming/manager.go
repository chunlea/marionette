package streaming

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Common errors for stream management.
var (
	// ErrStreamNotFound indicates the requested stream does not exist.
	ErrStreamNotFound = errors.New("stream not found")

	// ErrStreamAlreadyExists indicates a stream already exists for the session/type.
	ErrStreamAlreadyExists = errors.New("stream already exists")

	// ErrProviderNotFound indicates the requested provider does not exist.
	ErrProviderNotFound = errors.New("provider not found")

	// ErrUnsupportedStreamType indicates the stream type is not supported.
	ErrUnsupportedStreamType = errors.New("unsupported stream type")

	// ErrStreamNotActive indicates the stream is not in an active state.
	ErrStreamNotActive = errors.New("stream not active")

	// ErrInvalidStreamState indicates an invalid state transition was attempted.
	ErrInvalidStreamState = errors.New("invalid stream state")
)

// ManagerConfig contains configuration for the stream manager.
type ManagerConfig struct {
	// DefaultProvider is the default provider name to use.
	DefaultProvider string

	// DefaultTimeout is the default timeout for stream operations.
	DefaultTimeout time.Duration

	// DefaultICEServers are the default STUN/TURN servers.
	DefaultICEServers []ICEServer

	// DefaultResolution is the default stream resolution.
	DefaultResolution Resolution

	// DefaultFrameRate is the default frame rate.
	DefaultFrameRate int

	// DefaultBitRate is the default bitrate in kbps.
	DefaultBitRate int

	// CleanupInterval is how often to clean up expired streams.
	CleanupInterval time.Duration

	// StreamExpiry is how long inactive streams are kept.
	StreamExpiry time.Duration
}

// DefaultManagerConfig returns a configuration with sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		DefaultProvider: "selkies",
		DefaultTimeout:  30 * time.Second,
		DefaultICEServers: []ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun1.l.google.com:19302"}},
		},
		DefaultResolution: Resolution{Width: 1920, Height: 1080},
		DefaultFrameRate:  30,
		DefaultBitRate:    4000,
		CleanupInterval:   5 * time.Minute,
		StreamExpiry:      24 * time.Hour,
	}
}

// Manager manages stream providers and stream lifecycle.
type Manager struct {
	config    ManagerConfig
	store     StreamStore
	providers map[string]StreamProvider
	logger    *zap.Logger

	mu       sync.RWMutex
	stopC    chan struct{}
	stoppedC chan struct{}
}

// NewManager creates a new stream manager.
func NewManager(config ManagerConfig, store StreamStore, logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		config:    config,
		store:     store,
		providers: make(map[string]StreamProvider),
		logger:    logger.Named("streaming"),
		stopC:     make(chan struct{}),
		stoppedC:  make(chan struct{}),
	}
}

// RegisterProvider registers a stream provider.
func (m *Manager) RegisterProvider(provider StreamProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := provider.Name()
	if _, exists := m.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}

	m.providers[name] = provider
	m.logger.Info("registered stream provider",
		zap.String("provider", name),
		zap.Any("supported_types", provider.SupportedTypes()),
	)

	return nil
}

// GetProvider returns a provider by name.
func (m *Manager) GetProvider(name string) (StreamProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return provider, nil
}

// GetProviderForType returns a provider that supports the given stream type.
func (m *Manager) GetProviderForType(streamType StreamType) (StreamProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First try the default provider
	if provider, ok := m.providers[m.config.DefaultProvider]; ok {
		for _, t := range provider.SupportedTypes() {
			if t == streamType {
				return provider, nil
			}
		}
	}

	// Search all providers
	for _, provider := range m.providers {
		for _, t := range provider.SupportedTypes() {
			if t == streamType {
				return provider, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrUnsupportedStreamType, streamType)
}

// StartStream starts a new stream for a session.
func (m *Manager) StartStream(ctx context.Context, opts StreamOptions) (*StreamInfo, error) {
	// Apply defaults
	opts = m.applyDefaults(opts)

	// Get provider for the stream type
	provider, err := m.GetProviderForType(opts.Type)
	if err != nil {
		return nil, err
	}

	// Check if stream already exists
	existing, err := m.store.GetStreamBySession(ctx, opts.SessionID, opts.Type)
	if err != nil {
		return nil, fmt.Errorf("checking existing stream: %w", err)
	}
	if existing != nil && existing.State != StreamStateStopped && existing.State != StreamStateError {
		return nil, fmt.Errorf("%w: session %s already has active %s stream",
			ErrStreamAlreadyExists, opts.SessionID, opts.Type)
	}

	// Create stream record
	expiresAt := time.Now().Add(m.config.StreamExpiry)
	stream, err := m.store.CreateStream(ctx, CreateStreamParams{
		SessionID:    opts.SessionID,
		RunnerID:     opts.RunnerID,
		Type:         opts.Type,
		ProviderName: provider.Name(),
		ICEServers:   opts.ICEServers,
		Resolution:   opts.Resolution,
		FrameRate:    opts.FrameRate,
		BitRate:      opts.BitRate,
		AudioEnabled: opts.EnableAudio,
		InputEnabled: opts.EnableInput,
		ExpiresAt:    &expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("creating stream record: %w", err)
	}

	// Update state to starting
	stream, err = m.store.UpdateStream(ctx, stream.ID, UpdateStreamParams{
		State: ptr(StreamStateStarting),
	})
	if err != nil {
		return nil, fmt.Errorf("updating stream state: %w", err)
	}

	// Start the stream via provider
	info, err := provider.Start(ctx, opts)
	if err != nil {
		// Update stream with error state
		errStr := err.Error()
		_, updateErr := m.store.UpdateStream(ctx, stream.ID, UpdateStreamParams{
			State: ptr(StreamStateError),
			Error: &errStr,
		})
		if updateErr != nil {
			m.logger.Error("failed to update stream error state",
				zap.String("stream_id", stream.ID),
				zap.Error(updateErr),
			)
		}
		return nil, fmt.Errorf("starting stream: %w", err)
	}

	// Update stream with provider info
	now := time.Now()
	stream, err = m.store.UpdateStream(ctx, stream.ID, UpdateStreamParams{
		State:            ptr(StreamStateActive),
		SignalingURL:     &info.SignalingURL,
		ProviderStreamID: ptr(info.ID),
		Resolution:       &info.Resolution,
		FrameRate:        &info.FrameRate,
		BitRate:          &info.BitRate,
		StartedAt:        &now,
		Metadata:         info.Metadata,
	})
	if err != nil {
		// Try to stop the provider stream
		if stopErr := provider.Stop(ctx, info.ID); stopErr != nil {
			m.logger.Error("failed to stop provider stream after update failure",
				zap.String("stream_id", stream.ID),
				zap.Error(stopErr),
			)
		}
		return nil, fmt.Errorf("updating stream info: %w", err)
	}

	m.logger.Info("started stream",
		zap.String("stream_id", stream.ID),
		zap.String("session_id", opts.SessionID),
		zap.String("type", string(opts.Type)),
		zap.String("provider", provider.Name()),
	)

	return &StreamInfo{
		ID:           stream.ID,
		SessionID:    stream.SessionID,
		RunnerID:     stream.RunnerID,
		Type:         stream.Type,
		State:        stream.State,
		SignalingURL: stream.SignalingURL,
		ICEServers:   stream.ICEServers,
		Resolution:   stream.Resolution,
		FrameRate:    stream.FrameRate,
		BitRate:      stream.BitRate,
		AudioEnabled: stream.AudioEnabled,
		InputEnabled: stream.InputEnabled,
		StartedAt:    now,
		Metadata:     stream.Metadata,
	}, nil
}

// StopStream stops an active stream.
func (m *Manager) StopStream(ctx context.Context, streamID string) error {
	// Get stream record
	stream, err := m.store.GetStream(ctx, streamID)
	if err != nil {
		return fmt.Errorf("getting stream: %w", err)
	}
	if stream == nil {
		return ErrStreamNotFound
	}

	// Check if already stopped
	if stream.State == StreamStateStopped {
		return nil
	}

	// Update state to stopping
	_, err = m.store.UpdateStream(ctx, streamID, UpdateStreamParams{
		State: ptr(StreamStateStopping),
	})
	if err != nil {
		return fmt.Errorf("updating stream state: %w", err)
	}

	// Get provider and stop the stream
	provider, err := m.GetProvider(stream.ProviderName)
	if err != nil {
		m.logger.Warn("provider not found for stream, marking as stopped",
			zap.String("stream_id", streamID),
			zap.String("provider", stream.ProviderName),
		)
	} else if stream.ProviderStreamID != "" {
		if err := provider.Stop(ctx, stream.ProviderStreamID); err != nil {
			m.logger.Error("failed to stop provider stream",
				zap.String("stream_id", streamID),
				zap.String("provider_stream_id", stream.ProviderStreamID),
				zap.Error(err),
			)
			// Continue to mark as stopped anyway
		}
	}

	// Update stream as stopped
	now := time.Now()
	_, err = m.store.UpdateStream(ctx, streamID, UpdateStreamParams{
		State:     ptr(StreamStateStopped),
		StoppedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("updating stream stopped state: %w", err)
	}

	m.logger.Info("stopped stream",
		zap.String("stream_id", streamID),
		zap.String("session_id", stream.SessionID),
	)

	return nil
}

// GetStream returns stream information by ID.
func (m *Manager) GetStream(ctx context.Context, streamID string) (*StreamInfo, error) {
	stream, err := m.store.GetStream(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("getting stream: %w", err)
	}
	if stream == nil {
		return nil, ErrStreamNotFound
	}

	return m.streamToInfo(stream), nil
}

// GetSessionStream returns the active stream for a session.
func (m *Manager) GetSessionStream(ctx context.Context, sessionID string, streamType StreamType) (*StreamInfo, error) {
	stream, err := m.store.GetStreamBySession(ctx, sessionID, streamType)
	if err != nil {
		return nil, fmt.Errorf("getting session stream: %w", err)
	}
	if stream == nil {
		return nil, ErrStreamNotFound
	}

	return m.streamToInfo(stream), nil
}

// ListSessionStreams lists all streams for a session.
func (m *Manager) ListSessionStreams(ctx context.Context, sessionID string) ([]*StreamInfo, error) {
	streams, err := m.store.ListStreams(ctx, ListStreamsParams{
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("listing streams: %w", err)
	}

	result := make([]*StreamInfo, len(streams))
	for i, stream := range streams {
		result[i] = m.streamToInfo(stream)
	}
	return result, nil
}

// Start starts the manager's background tasks.
func (m *Manager) Start(ctx context.Context) {
	go m.cleanupLoop(ctx)
	m.logger.Info("stream manager started")
}

// Stop stops the manager and all background tasks.
func (m *Manager) Stop() {
	close(m.stopC)
	<-m.stoppedC
	m.logger.Info("stream manager stopped")
}

// cleanupLoop periodically cleans up expired streams.
func (m *Manager) cleanupLoop(ctx context.Context) {
	defer close(m.stoppedC)

	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopC:
			return
		case <-ticker.C:
			count, err := m.store.CleanupExpiredStreams(ctx)
			if err != nil {
				m.logger.Error("failed to cleanup expired streams", zap.Error(err))
			} else if count > 0 {
				m.logger.Info("cleaned up expired streams", zap.Int("count", count))
			}
		}
	}
}

// applyDefaults applies default values to stream options.
func (m *Manager) applyDefaults(opts StreamOptions) StreamOptions {
	if opts.Timeout == 0 {
		opts.Timeout = m.config.DefaultTimeout
	}
	if len(opts.ICEServers) == 0 {
		opts.ICEServers = m.config.DefaultICEServers
	}
	if opts.Resolution.Width == 0 || opts.Resolution.Height == 0 {
		opts.Resolution = m.config.DefaultResolution
	}
	if opts.FrameRate == 0 {
		opts.FrameRate = m.config.DefaultFrameRate
	}
	if opts.BitRate == 0 {
		opts.BitRate = m.config.DefaultBitRate
	}
	return opts
}

// streamToInfo converts a Stream record to StreamInfo.
func (m *Manager) streamToInfo(stream *Stream) *StreamInfo {
	info := &StreamInfo{
		ID:           stream.ID,
		SessionID:    stream.SessionID,
		RunnerID:     stream.RunnerID,
		Type:         stream.Type,
		State:        stream.State,
		SignalingURL: stream.SignalingURL,
		ICEServers:   stream.ICEServers,
		Resolution:   stream.Resolution,
		FrameRate:    stream.FrameRate,
		BitRate:      stream.BitRate,
		AudioEnabled: stream.AudioEnabled,
		InputEnabled: stream.InputEnabled,
		Error:        stream.Error,
		Metadata:     stream.Metadata,
	}
	if stream.StartedAt != nil {
		info.StartedAt = *stream.StartedAt
	}
	return info
}

// ptr returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}
