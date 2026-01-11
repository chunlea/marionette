package manager

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/sfu"
)

// Manager manages the lifecycle of all streams.
// It coordinates between providers, the SFU, and the store.
type Manager struct {
	config   Config
	store    streaming.StreamStore
	registry *streaming.ProviderRegistry
	sfu      *sfu.SFU
	handler  *sfu.SignalingHandler
	logger   *zap.Logger

	mu     sync.RWMutex
	closed bool
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new streaming Manager.
func New(config Config, store streaming.StreamStore, logger *zap.Logger) (*Manager, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	sfuInstance, err := sfu.New(config.SFU, logger)
	if err != nil {
		return nil, err
	}

	handler := sfu.NewSignalingHandler(sfuInstance, logger)

	return &Manager{
		config:   config,
		store:    store,
		registry: streaming.NewProviderRegistry(),
		sfu:      sfuInstance,
		handler:  handler,
		logger:   logger.Named("streaming.manager"),
		stopCh:   make(chan struct{}),
	}, nil
}

// Start starts the manager and its background tasks.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return streaming.ErrStreamClosed
	}
	m.mu.Unlock()

	// Start cleanup goroutine
	if m.config.CleanupInterval > 0 {
		m.wg.Add(1)
		go m.cleanupLoop()
	}

	m.logger.Info("streaming manager started",
		zap.Duration("cleanup_interval", m.config.CleanupInterval),
	)

	return nil
}

// Stop stops the manager and all its background tasks.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.stopCh)
	m.mu.Unlock()

	// Wait for background tasks with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		m.logger.Warn("manager stop timed out waiting for background tasks")
	}

	// Close SFU
	if err := m.sfu.Close(ctx); err != nil {
		m.logger.Error("failed to close SFU", zap.Error(err))
	}

	m.logger.Info("streaming manager stopped")
	return nil
}

// RegisterProvider registers a stream provider.
func (m *Manager) RegisterProvider(provider streaming.StreamProvider) error {
	return m.registry.Register(provider)
}

// UnregisterProvider unregisters a stream provider.
func (m *Manager) UnregisterProvider(name string) bool {
	return m.registry.Unregister(name)
}

// GetProvider returns a provider by name.
func (m *Manager) GetProvider(name string) (streaming.StreamProvider, bool) {
	return m.registry.Get(name)
}

// GetProviderForType returns a provider that supports the given stream type.
func (m *Manager) GetProviderForType(streamType streaming.StreamType) (streaming.StreamProvider, error) {
	return m.registry.GetForType(streamType)
}

// ListProviders returns all registered providers.
func (m *Manager) ListProviders() []streaming.StreamProvider {
	return m.registry.List()
}

// StartStream starts a new stream with the given options.
func (m *Manager) StartStream(ctx context.Context, opts streaming.StreamOptions) (*streaming.Stream, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, streaming.ErrStreamClosed
	}
	m.mu.RUnlock()

	// Validate options
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Apply defaults
	opts = m.applyDefaults(opts)

	// Get provider
	var provider streaming.StreamProvider
	var err error
	if m.config.DefaultProvider != "" {
		var ok bool
		provider, ok = m.registry.Get(m.config.DefaultProvider)
		if !ok {
			return nil, streaming.ErrProviderNotFound
		}
	} else {
		provider, err = m.registry.GetForType(opts.Type)
		if err != nil {
			return nil, err
		}
	}

	// Generate stream ID
	streamID := id.Stream()

	// Create stream record in store
	createParams := streaming.CreateStreamParams{
		ID:           streamID,
		SessionID:    opts.SessionID,
		RunnerID:     opts.RunnerID,
		TenantID:     opts.TenantID,
		Type:         opts.Type,
		Resolution:   opts.Resolution,
		FrameRate:    opts.FrameRate,
		BitRate:      opts.BitRate,
		AudioEnabled: opts.AudioEnabled,
		InputEnabled: opts.InputEnabled,
		ICEServers:   opts.ICEServers,
		ProviderName: provider.Name(),
		Metadata:     opts.Metadata,
		ExpiresAt:    opts.ExpiresAt,
	}

	stream, err := m.store.CreateStream(ctx, createParams)
	if err != nil {
		return nil, err
	}

	// Update state to starting
	startingState := streaming.StreamStateStarting
	stream, err = m.store.UpdateStream(ctx, streamID, streaming.UpdateStreamParams{
		State: &startingState,
	})
	if err != nil {
		m.logger.Error("failed to update stream state to starting",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
		return nil, err
	}

	// Start with provider
	info, err := provider.Start(ctx, opts)
	if err != nil {
		// Update state to error
		errorState := streaming.StreamStateError
		errMsg := err.Error()
		_, updateErr := m.store.UpdateStream(ctx, streamID, streaming.UpdateStreamParams{
			State: &errorState,
			Error: &errMsg,
		})
		if updateErr != nil {
			m.logger.Error("failed to update stream state to error",
				zap.String("stream_id", streamID),
				zap.Error(updateErr),
			)
		}
		return nil, err
	}

	// Update stream with provider info
	now := time.Now()
	activeState := streaming.StreamStateActive
	stream, err = m.store.UpdateStream(ctx, streamID, streaming.UpdateStreamParams{
		State:            &activeState,
		SignalingURL:     &info.SignalingURL,
		ProviderStreamID: &info.ID,
		Resolution:       &info.Resolution,
		FrameRate:        &info.FrameRate,
		BitRate:          &info.BitRate,
		VideoCodec:       &info.VideoCodec,
		AudioCodec:       &info.AudioCodec,
		StartedAt:        &now,
		Metadata:         info.Metadata,
	})
	if err != nil {
		m.logger.Error("failed to update stream with provider info",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
		// Try to stop the provider stream
		if stopErr := provider.Stop(ctx, info.ID); stopErr != nil {
			m.logger.Error("failed to stop provider stream after update error",
				zap.String("stream_id", streamID),
				zap.String("provider_stream_id", info.ID),
				zap.Error(stopErr),
			)
		}
		return nil, err
	}

	m.logger.Info("stream started",
		zap.String("stream_id", streamID),
		zap.String("session_id", opts.SessionID),
		zap.String("type", string(opts.Type)),
		zap.String("provider", provider.Name()),
	)

	return stream, nil
}

// StopStream stops a stream.
func (m *Manager) StopStream(ctx context.Context, streamID string) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return streaming.ErrStreamClosed
	}
	m.mu.RUnlock()

	// Get stream
	stream, err := m.store.GetStream(ctx, streamID)
	if err != nil {
		return err
	}

	// Check if already stopped
	if stream.State.IsTerminal() {
		return nil
	}

	// Update state to stopping
	stoppingState := streaming.StreamStateStopping
	stream, err = m.store.UpdateStream(ctx, streamID, streaming.UpdateStreamParams{
		State: &stoppingState,
	})
	if err != nil {
		return err
	}

	// Get provider and stop
	if stream.ProviderStreamID != "" && stream.ProviderName != "" {
		provider, ok := m.registry.Get(stream.ProviderName)
		if ok {
			if err := provider.Stop(ctx, stream.ProviderStreamID); err != nil {
				m.logger.Warn("failed to stop provider stream",
					zap.String("stream_id", streamID),
					zap.String("provider_stream_id", stream.ProviderStreamID),
					zap.Error(err),
				)
			}
		}
	}

	// Close SFU room
	if err := m.sfu.RemoveRoom(ctx, streamID); err != nil && err != streaming.ErrRoomNotFound {
		m.logger.Warn("failed to remove SFU room",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
	}

	// Update state to stopped
	now := time.Now()
	stoppedState := streaming.StreamStateStopped
	_, err = m.store.UpdateStream(ctx, streamID, streaming.UpdateStreamParams{
		State:     &stoppedState,
		StoppedAt: &now,
	})
	if err != nil {
		return err
	}

	m.logger.Info("stream stopped",
		zap.String("stream_id", streamID),
	)

	return nil
}

// GetStream returns a stream by ID.
func (m *Manager) GetStream(ctx context.Context, streamID string) (*streaming.Stream, error) {
	return m.store.GetStream(ctx, streamID)
}

// GetStreamBySession returns the active stream for a session and type.
func (m *Manager) GetStreamBySession(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error) {
	return m.store.GetStreamBySession(ctx, sessionID, streamType)
}

// ListStreams lists streams matching the given parameters.
func (m *Manager) ListStreams(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
	return m.store.ListStreams(ctx, params)
}

// ListSessionStreams lists all streams for a session.
func (m *Manager) ListSessionStreams(ctx context.Context, sessionID string) ([]*streaming.Stream, error) {
	streams, _, err := m.store.ListStreams(ctx, streaming.ListStreamsParams{
		SessionID:  sessionID,
		ActiveOnly: true,
	})
	return streams, err
}

// GetSFU returns the SFU instance.
func (m *Manager) GetSFU() *sfu.SFU {
	return m.sfu
}

// GetSignalingHandler returns the signaling handler.
func (m *Manager) GetSignalingHandler() *sfu.SignalingHandler {
	return m.handler
}

// Config returns the manager configuration.
func (m *Manager) Config() Config {
	return m.config
}

// applyDefaults applies default values to stream options.
func (m *Manager) applyDefaults(opts streaming.StreamOptions) streaming.StreamOptions {
	if opts.Resolution.IsZero() {
		opts.Resolution = m.config.DefaultResolution
	}
	if opts.FrameRate == 0 {
		opts.FrameRate = m.config.DefaultFrameRate
	}
	if opts.BitRate == 0 {
		opts.BitRate = m.config.DefaultBitRate
	}
	if len(opts.ICEServers) == 0 {
		opts.ICEServers = m.config.DefaultICEServers
	}
	if opts.ExpiresAt == nil && m.config.StreamExpiry > 0 {
		expiry := time.Now().Add(m.config.StreamExpiry)
		opts.ExpiresAt = &expiry
	}
	return opts
}

// cleanupLoop runs the cleanup job periodically.
func (m *Manager) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.runCleanup()
		}
	}
}

// runCleanup cleans up expired streams.
func (m *Manager) runCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), m.config.DefaultTimeout)
	defer cancel()

	count, err := m.store.CleanupExpiredStreams(ctx)
	if err != nil {
		m.logger.Error("failed to cleanup expired streams", zap.Error(err))
		return
	}

	if count > 0 {
		m.logger.Info("cleaned up expired streams", zap.Int("count", count))
	}
}

// Stats returns manager statistics.
func (m *Manager) Stats() Stats {
	return Stats{
		ProviderCount: len(m.registry.List()),
		SFUStats:      m.sfu.GetStats(),
		IsClosed:      m.isClosed(),
	}
}

// isClosed returns whether the manager is closed.
func (m *Manager) isClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// Stats contains manager statistics.
type Stats struct {
	ProviderCount int
	SFUStats      sfu.Stats
	IsClosed      bool
}
