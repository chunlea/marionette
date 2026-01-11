package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

// mockStreamStore implements streaming.StreamStore for testing.
type mockStreamStore struct {
	mu      sync.RWMutex
	streams map[string]*streaming.Stream
}

func newMockStreamStore() *mockStreamStore {
	return &mockStreamStore{
		streams: make(map[string]*streaming.Stream),
	}
}

func (s *mockStreamStore) CreateStream(ctx context.Context, params streaming.CreateStreamParams) (*streaming.Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.streams[params.ID]; exists {
		return nil, streaming.ErrStreamExists
	}

	now := time.Now()
	stream := &streaming.Stream{
		ID:           params.ID,
		SessionID:    params.SessionID,
		RunnerID:     params.RunnerID,
		TenantID:     params.TenantID,
		Type:         params.Type,
		State:        streaming.StreamStatePending,
		Resolution:   params.Resolution,
		FrameRate:    params.FrameRate,
		BitRate:      params.BitRate,
		AudioEnabled: params.AudioEnabled,
		InputEnabled: params.InputEnabled,
		ICEServers:   params.ICEServers,
		ProviderName: params.ProviderName,
		Metadata:     params.Metadata,
		ExpiresAt:    params.ExpiresAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.streams[params.ID] = stream
	return stream, nil
}

func (s *mockStreamStore) GetStream(ctx context.Context, id string) (*streaming.Stream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stream, ok := s.streams[id]
	if !ok {
		return nil, streaming.ErrStreamNotFound
	}
	return stream, nil
}

func (s *mockStreamStore) GetStreamBySession(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, stream := range s.streams {
		if stream.SessionID == sessionID && stream.Type == streamType && !stream.State.IsTerminal() {
			return stream, nil
		}
	}
	return nil, streaming.ErrStreamNotFound
}

func (s *mockStreamStore) UpdateStream(ctx context.Context, id string, params streaming.UpdateStreamParams) (*streaming.Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stream, ok := s.streams[id]
	if !ok {
		return nil, streaming.ErrStreamNotFound
	}

	if params.State != nil {
		stream.State = *params.State
	}
	if params.SignalingURL != nil {
		stream.SignalingURL = *params.SignalingURL
	}
	if params.ProviderStreamID != nil {
		stream.ProviderStreamID = *params.ProviderStreamID
	}
	if params.Resolution != nil {
		stream.Resolution = *params.Resolution
	}
	if params.FrameRate != nil {
		stream.FrameRate = *params.FrameRate
	}
	if params.BitRate != nil {
		stream.BitRate = *params.BitRate
	}
	if params.VideoCodec != nil {
		stream.VideoCodec = *params.VideoCodec
	}
	if params.AudioCodec != nil {
		stream.AudioCodec = *params.AudioCodec
	}
	if params.Error != nil {
		stream.Error = *params.Error
	}
	if params.StartedAt != nil {
		stream.StartedAt = params.StartedAt
	}
	if params.StoppedAt != nil {
		stream.StoppedAt = params.StoppedAt
	}
	if params.Metadata != nil {
		stream.Metadata = params.Metadata
	}
	stream.UpdatedAt = time.Now()

	return stream, nil
}

func (s *mockStreamStore) DeleteStream(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.streams[id]; !ok {
		return streaming.ErrStreamNotFound
	}
	delete(s.streams, id)
	return nil
}

func (s *mockStreamStore) ListStreams(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*streaming.Stream
	for _, stream := range s.streams {
		if params.SessionID != "" && stream.SessionID != params.SessionID {
			continue
		}
		if params.ActiveOnly && stream.State.IsTerminal() {
			continue
		}
		result = append(result, stream)
	}
	return result, len(result), nil
}

func (s *mockStreamStore) CleanupExpiredStreams(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	now := time.Now()
	for _, stream := range s.streams {
		if stream.ExpiresAt != nil && stream.ExpiresAt.Before(now) && !stream.State.IsTerminal() {
			stream.State = streaming.StreamStateStopped
			stream.StoppedAt = &now
			count++
		}
	}
	return count, nil
}

// mockProvider implements streaming.StreamProvider for testing.
type mockProvider struct {
	name           string
	supportedTypes []streaming.StreamType
	startErr       error
	stopErr        error
	streams        map[string]*streaming.StreamInfo
	mu             sync.Mutex
}

func newMockProvider(name string, types []streaming.StreamType) *mockProvider {
	return &mockProvider{
		name:           name,
		supportedTypes: types,
		streams:        make(map[string]*streaming.StreamInfo),
	}
}

func (p *mockProvider) Name() string {
	return p.name
}

func (p *mockProvider) SupportedTypes() []streaming.StreamType {
	return p.supportedTypes
}

func (p *mockProvider) Start(ctx context.Context, opts streaming.StreamOptions) (*streaming.StreamInfo, error) {
	if p.startErr != nil {
		return nil, p.startErr
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	info := &streaming.StreamInfo{
		ID:           "provider-stream-" + opts.SessionID,
		SignalingURL: "wss://example.com/signaling/" + opts.SessionID,
		Resolution:   opts.Resolution,
		FrameRate:    opts.FrameRate,
		BitRate:      opts.BitRate,
		VideoCodec:   "VP8",
		AudioCodec:   "opus",
	}
	p.streams[info.ID] = info
	return info, nil
}

func (p *mockProvider) Stop(ctx context.Context, providerStreamID string) error {
	if p.stopErr != nil {
		return p.stopErr
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.streams, providerStreamID)
	return nil
}

func (p *mockProvider) GetInfo(ctx context.Context, providerStreamID string) (*streaming.StreamInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	info, ok := p.streams[providerStreamID]
	if !ok {
		return nil, streaming.ErrStreamNotFound
	}
	return info, nil
}

func createTestManager(t *testing.T) (*Manager, *mockStreamStore, *mockProvider) {
	t.Helper()

	cfg := DefaultConfig()
	cfg.CleanupInterval = 0 // Disable cleanup for tests

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	provider := newMockProvider("test-provider", []streaming.StreamType{
		streaming.StreamTypeDesktop,
		streaming.StreamTypeBrowser,
	})
	err = m.RegisterProvider(provider)
	require.NoError(t, err)

	return m, store, provider
}

func TestNew(t *testing.T) {
	cfg := DefaultConfig()
	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)
	assert.NotNil(t, m)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	assert.NotNil(t, m.GetSFU())
	assert.NotNil(t, m.GetSignalingHandler())
	assert.Equal(t, cfg.DefaultTimeout, m.Config().DefaultTimeout)
}

func TestNew_NilLogger(t *testing.T) {
	cfg := DefaultConfig()
	store := newMockStreamStore()

	m, err := New(cfg, store, nil)
	require.NoError(t, err)
	assert.NotNil(t, m)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})
}

func TestManager_StartStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 100 * time.Millisecond

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	ctx := context.Background()

	err = m.Start(ctx)
	require.NoError(t, err)

	// Let cleanup run at least once
	time.Sleep(150 * time.Millisecond)

	err = m.Stop(ctx)
	require.NoError(t, err)

	// Stop should be idempotent
	err = m.Stop(ctx)
	require.NoError(t, err)
}

func TestManager_RegisterProvider(t *testing.T) {
	m, _, _ := createTestManager(t)

	// Provider already registered
	provider := newMockProvider("test-provider", []streaming.StreamType{streaming.StreamTypeDesktop})
	err := m.RegisterProvider(provider)
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrProviderExists, err)

	// Register new provider
	provider2 := newMockProvider("test-provider-2", []streaming.StreamType{streaming.StreamTypeAndroid})
	err = m.RegisterProvider(provider2)
	assert.NoError(t, err)

	// Get provider
	p, ok := m.GetProvider("test-provider-2")
	assert.True(t, ok)
	assert.Equal(t, "test-provider-2", p.Name())

	// Get non-existent provider
	_, ok = m.GetProvider("non-existent")
	assert.False(t, ok)
}

func TestManager_UnregisterProvider(t *testing.T) {
	m, _, _ := createTestManager(t)

	// Unregister existing provider
	ok := m.UnregisterProvider("test-provider")
	assert.True(t, ok)

	// Unregister non-existent provider
	ok = m.UnregisterProvider("non-existent")
	assert.False(t, ok)
}

func TestManager_GetProviderForType(t *testing.T) {
	m, _, _ := createTestManager(t)

	// Get provider for supported type
	p, err := m.GetProviderForType(streaming.StreamTypeDesktop)
	require.NoError(t, err)
	assert.NotNil(t, p)

	// Get provider for unsupported type
	_, err = m.GetProviderForType(streaming.StreamTypeAndroid)
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrNoProviderForType, err)
}

func TestManager_ListProviders(t *testing.T) {
	m, _, _ := createTestManager(t)

	providers := m.ListProviders()
	assert.Len(t, providers, 1)
}

func TestManager_StartStream(t *testing.T) {
	m, store, _ := createTestManager(t)

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	}

	stream, err := m.StartStream(ctx, opts)
	require.NoError(t, err)
	assert.NotNil(t, stream)
	assert.NotEmpty(t, stream.ID)
	assert.Equal(t, "sess_test123", stream.SessionID)
	assert.Equal(t, streaming.StreamStateActive, stream.State)
	assert.NotEmpty(t, stream.SignalingURL)
	assert.NotEmpty(t, stream.ProviderStreamID)
	assert.Equal(t, "test-provider", stream.ProviderName)

	// Verify in store
	stored, err := store.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.Equal(t, stream.ID, stored.ID)
}

func TestManager_StartStream_WithDefaults(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
		// No resolution, frame rate, etc. - should use defaults
	}

	stream, err := m.StartStream(ctx, opts)
	require.NoError(t, err)

	// Check defaults were applied
	assert.Equal(t, 1920, stream.Resolution.Width)
	assert.Equal(t, 1080, stream.Resolution.Height)
	assert.Equal(t, 30, stream.FrameRate)
	assert.Equal(t, 4_000_000, stream.BitRate)
}

func TestManager_StartStream_ValidationError(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Missing session ID
	_, err := m.StartStream(ctx, streaming.StreamOptions{
		Type: streaming.StreamTypeDesktop,
	})
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrSessionRequired, err)

	// Invalid stream type
	_, err = m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      "invalid",
	})
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrInvalidStreamType, err)
}

func TestManager_StartStream_NoProvider(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeAndroid, // No provider for this type
	}

	_, err := m.StartStream(ctx, opts)
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrNoProviderForType, err)
}

func TestManager_StartStream_ProviderError(t *testing.T) {
	m, store, provider := createTestManager(t)

	provider.startErr = errors.New("provider start failed")

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	}

	_, err := m.StartStream(ctx, opts)
	assert.Error(t, err)

	// Check that stream is in error state
	streams, _, _ := store.ListStreams(ctx, streaming.ListStreamsParams{})
	require.Len(t, streams, 1)
	assert.Equal(t, streaming.StreamStateError, streams[0].State)
	assert.Contains(t, streams[0].Error, "provider start failed")
}

func TestManager_StartStream_Closed(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()
	m.Stop(ctx)

	_, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrStreamClosed, err)
}

func TestManager_StopStream(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start a stream
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Stop the stream
	err = m.StopStream(ctx, stream.ID)
	require.NoError(t, err)

	// Verify stream is stopped
	stopped, err := m.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.Equal(t, streaming.StreamStateStopped, stopped.State)
	assert.NotNil(t, stopped.StoppedAt)
}

func TestManager_StopStream_AlreadyStopped(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start a stream
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Stop the stream
	err = m.StopStream(ctx, stream.ID)
	require.NoError(t, err)

	// Stop again - should be idempotent
	err = m.StopStream(ctx, stream.ID)
	require.NoError(t, err)
}

func TestManager_StopStream_NotFound(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()
	err := m.StopStream(ctx, "non-existent")
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrStreamNotFound, err)
}

func TestManager_StopStream_Closed(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start a stream
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Close manager
	m.Stop(ctx)

	// Try to stop stream
	err = m.StopStream(ctx, stream.ID)
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrStreamClosed, err)
}

func TestManager_GetStream(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start a stream
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Get the stream
	got, err := m.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.Equal(t, stream.ID, got.ID)
}

func TestManager_GetStreamBySession(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start a stream
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Get by session and type
	got, err := m.GetStreamBySession(ctx, "sess_test123", streaming.StreamTypeDesktop)
	require.NoError(t, err)
	assert.Equal(t, stream.ID, got.ID)

	// Get non-existent
	_, err = m.GetStreamBySession(ctx, "sess_test123", streaming.StreamTypeBrowser)
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrStreamNotFound, err)
}

func TestManager_ListStreams(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start multiple streams
	_, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test1",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	_, err = m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test2",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// List all streams
	streams, count, err := m.ListStreams(ctx, streaming.ListStreamsParams{})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Len(t, streams, 2)

	// List by session
	streams, count, err = m.ListStreams(ctx, streaming.ListStreamsParams{
		SessionID: "sess_test1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Len(t, streams, 1)
}

func TestManager_ListSessionStreams(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()

	// Start multiple streams for same session
	_, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// List session streams
	streams, err := m.ListSessionStreams(ctx, "sess_test123")
	require.NoError(t, err)
	assert.Len(t, streams, 1)
}

func TestManager_Stats(t *testing.T) {
	m, _, _ := createTestManager(t)

	stats := m.Stats()
	assert.Equal(t, 1, stats.ProviderCount)
	assert.False(t, stats.IsClosed)
}

func TestManager_Stats_Closed(t *testing.T) {
	m, _, _ := createTestManager(t)

	ctx := context.Background()
	m.Stop(ctx)

	stats := m.Stats()
	assert.True(t, stats.IsClosed)
}

func TestManager_Config(t *testing.T) {
	m, _, _ := createTestManager(t)

	cfg := m.Config()
	assert.Equal(t, 30*time.Second, cfg.DefaultTimeout)
}

func TestManager_DefaultProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 0
	cfg.DefaultProvider = "test-provider"

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	// Register provider
	provider := newMockProvider("test-provider", []streaming.StreamType{
		streaming.StreamTypeDesktop,
	})
	err = m.RegisterProvider(provider)
	require.NoError(t, err)

	// Start stream - should use default provider
	ctx := context.Background()
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-provider", stream.ProviderName)
}

func TestManager_DefaultProvider_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 0
	cfg.DefaultProvider = "non-existent"

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	ctx := context.Background()
	_, err = m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrProviderNotFound, err)
}

func TestManager_CleanupExpiredStreams(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 50 * time.Millisecond
	cfg.StreamExpiry = 100 * time.Millisecond

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	// Register provider
	provider := newMockProvider("test-provider", []streaming.StreamType{
		streaming.StreamTypeDesktop,
	})
	err = m.RegisterProvider(provider)
	require.NoError(t, err)

	ctx := context.Background()

	// Start manager
	err = m.Start(ctx)
	require.NoError(t, err)

	// Start a stream with expiry
	_, err = m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Wait for expiry and cleanup
	time.Sleep(200 * time.Millisecond)

	// Check stream is cleaned up
	streams, _, err := store.ListStreams(ctx, streaming.ListStreamsParams{
		ActiveOnly: true,
	})
	require.NoError(t, err)
	assert.Len(t, streams, 0)
}

func TestConfig_WithSFUConfig(t *testing.T) {
	cfg := DefaultConfig()

	sfuCfg := cfg.SFU
	sfuCfg.PLIInterval = 10

	cfg = cfg.WithSFUConfig(sfuCfg)
	assert.Equal(t, uint16(10), cfg.SFU.PLIInterval)
}

func TestManager_Start_AlreadyClosed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 100 * time.Millisecond

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Close the manager first
	err = m.Stop(ctx)
	require.NoError(t, err)

	// Now try to start - should fail
	err = m.Start(ctx)
	assert.Error(t, err)
	assert.Equal(t, streaming.ErrStreamClosed, err)
}

func TestManager_StopStream_ProviderError(t *testing.T) {
	m, _, provider := createTestManager(t)

	ctx := context.Background()

	// Start a stream
	stream, err := m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	require.NoError(t, err)

	// Set provider to return error on stop
	provider.stopErr = errors.New("provider stop failed")

	// Stop should still succeed (provider error is logged but not returned)
	err = m.StopStream(ctx, stream.ID)
	require.NoError(t, err)

	// Verify stream is stopped
	stopped, err := m.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.Equal(t, streaming.StreamStateStopped, stopped.State)
}

func TestManager_StartStream_UpdateError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 0

	// Create a store that fails on update after creating stream
	store := &failingUpdateStore{
		mockStreamStore: newMockStreamStore(),
		failAfterCreate: true,
	}
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	// Register provider
	provider := newMockProvider("test-provider", []streaming.StreamType{
		streaming.StreamTypeDesktop,
	})
	err = m.RegisterProvider(provider)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = m.StartStream(ctx, streaming.StreamOptions{
		SessionID: "sess_test123",
		Type:      streaming.StreamTypeDesktop,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

// failingUpdateStore is a mock store that fails on update.
type failingUpdateStore struct {
	*mockStreamStore
	failAfterCreate bool
	createCount     int
}

func (s *failingUpdateStore) CreateStream(ctx context.Context, params streaming.CreateStreamParams) (*streaming.Stream, error) {
	s.createCount++
	return s.mockStreamStore.CreateStream(ctx, params)
}

func (s *failingUpdateStore) UpdateStream(ctx context.Context, id string, params streaming.UpdateStreamParams) (*streaming.Stream, error) {
	if s.failAfterCreate && s.createCount > 0 {
		return nil, errors.New("update failed")
	}
	return s.mockStreamStore.UpdateStream(ctx, id, params)
}

func TestManager_StopStream_NoProviderInfo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 0

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	ctx := context.Background()

	// Create a stream directly in store without provider info
	params := streaming.CreateStreamParams{
		ID:           "strm_test123",
		SessionID:    "sess_test123",
		Type:         streaming.StreamTypeDesktop,
		ProviderName: "", // No provider
	}
	stream, err := store.CreateStream(ctx, params)
	require.NoError(t, err)

	// Update to active state
	activeState := streaming.StreamStateActive
	_, err = store.UpdateStream(ctx, stream.ID, streaming.UpdateStreamParams{
		State: &activeState,
	})
	require.NoError(t, err)

	// Stop should succeed even without provider info
	err = m.StopStream(ctx, stream.ID)
	require.NoError(t, err)
}

// errorCleanupStore is a mock store that returns error on cleanup.
type errorCleanupStore struct {
	*mockStreamStore
}

func (s *errorCleanupStore) CleanupExpiredStreams(ctx context.Context) (int, error) {
	return 0, errors.New("cleanup failed")
}

func TestManager_CleanupError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 50 * time.Millisecond

	store := &errorCleanupStore{
		mockStreamStore: newMockStreamStore(),
	}
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Start manager - cleanup should log error but not crash
	err = m.Start(ctx)
	require.NoError(t, err)

	// Wait for at least one cleanup run
	time.Sleep(100 * time.Millisecond)

	// Manager should still be running
	assert.False(t, m.isClosed())

	m.Stop(ctx)
}

func TestManager_StopWithTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 50 * time.Millisecond

	store := newMockStreamStore()
	logger := zap.NewNop()

	m, err := New(cfg, store, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Start manager
	err = m.Start(ctx)
	require.NoError(t, err)

	// Stop with immediate timeout
	cancelCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
	defer cancel()

	// Wait a bit to ensure the context times out
	time.Sleep(10 * time.Millisecond)

	err = m.Stop(cancelCtx)
	require.NoError(t, err) // Stop should return nil even if context times out
}
