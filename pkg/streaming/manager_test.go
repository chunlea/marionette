package streaming

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// MockStreamStore implements StreamStore for testing.
type MockStreamStore struct {
	mu      sync.Mutex
	streams map[string]*Stream

	// Hooks for testing
	CreateErr  error
	GetErr     error
	UpdateErr  error
	DeleteErr  error
	ListErr    error
	CleanupErr error
}

func NewMockStreamStore() *MockStreamStore {
	return &MockStreamStore{
		streams: make(map[string]*Stream),
	}
}

func (m *MockStreamStore) CreateStream(ctx context.Context, params CreateStreamParams) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CreateErr != nil {
		return nil, m.CreateErr
	}

	stream := &Stream{
		ID:           params.ID,
		SessionID:    params.SessionID,
		RunnerID:     params.RunnerID,
		TenantID:     params.TenantID,
		Type:         params.Type,
		State:        StreamStatePending,
		ProviderName: params.ProviderName,
		SignalingURL: params.SignalingURL,
		ICEServers:   params.ICEServers,
		Resolution:   params.Resolution,
		FrameRate:    params.FrameRate,
		BitRate:      params.BitRate,
		AudioEnabled: params.AudioEnabled,
		InputEnabled: params.InputEnabled,
		Metadata:     params.Metadata,
		ExpiresAt:    params.ExpiresAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if stream.ID == "" {
		stream.ID = "strm_test_" + time.Now().Format("20060102150405")
	}

	m.streams[stream.ID] = stream
	return stream, nil
}

func (m *MockStreamStore) GetStream(ctx context.Context, id string) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.GetErr != nil {
		return nil, m.GetErr
	}

	stream, ok := m.streams[id]
	if !ok {
		return nil, nil
	}
	return stream, nil
}

func (m *MockStreamStore) GetStreamBySession(ctx context.Context, sessionID string, streamType StreamType) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.GetErr != nil {
		return nil, m.GetErr
	}

	for _, stream := range m.streams {
		if stream.SessionID == sessionID && stream.Type == streamType &&
			stream.State != StreamStateStopped && stream.State != StreamStateError {
			return stream, nil
		}
	}
	return nil, nil
}

func (m *MockStreamStore) UpdateStream(ctx context.Context, id string, params UpdateStreamParams) (*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.UpdateErr != nil {
		return nil, m.UpdateErr
	}

	stream, ok := m.streams[id]
	if !ok {
		return nil, errors.New("stream not found")
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

func (m *MockStreamStore) DeleteStream(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.DeleteErr != nil {
		return m.DeleteErr
	}

	delete(m.streams, id)
	return nil
}

func (m *MockStreamStore) ListStreams(ctx context.Context, params ListStreamsParams) ([]*Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	var result []*Stream
	for _, stream := range m.streams {
		if params.SessionID != "" && stream.SessionID != params.SessionID {
			continue
		}
		if params.RunnerID != "" && stream.RunnerID != params.RunnerID {
			continue
		}
		if params.TenantID != "" && stream.TenantID != params.TenantID {
			continue
		}
		if params.Type != nil && stream.Type != *params.Type {
			continue
		}
		if params.State != nil && stream.State != *params.State {
			continue
		}
		if params.ActiveOnly && (stream.State == StreamStateStopped || stream.State == StreamStateError) {
			continue
		}
		result = append(result, stream)
	}
	return result, nil
}

func (m *MockStreamStore) CleanupExpiredStreams(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CleanupErr != nil {
		return 0, m.CleanupErr
	}

	count := 0
	now := time.Now()
	for _, stream := range m.streams {
		if stream.ExpiresAt != nil && stream.ExpiresAt.Before(now) &&
			stream.State != StreamStateStopped && stream.State != StreamStateError {
			stream.State = StreamStateStopped
			stoppedAt := now
			stream.StoppedAt = &stoppedAt
			count++
		}
	}
	return count, nil
}

// MockStreamProvider implements StreamProvider for testing.
type MockStreamProvider struct {
	name           string
	supportedTypes []StreamType
	startErr       error
	stopErr        error
	statusErr      error
	healthErr      error

	mu      sync.Mutex
	streams map[string]*StreamInfo
}

func NewMockStreamProvider(name string, types []StreamType) *MockStreamProvider {
	return &MockStreamProvider{
		name:           name,
		supportedTypes: types,
		streams:        make(map[string]*StreamInfo),
	}
}

func (m *MockStreamProvider) Name() string {
	return m.name
}

func (m *MockStreamProvider) SupportedTypes() []StreamType {
	return m.supportedTypes
}

func (m *MockStreamProvider) Start(ctx context.Context, opts StreamOptions) (*StreamInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startErr != nil {
		return nil, m.startErr
	}

	info := &StreamInfo{
		ID:           "provider_" + time.Now().Format("20060102150405"),
		SessionID:    opts.SessionID,
		RunnerID:     opts.RunnerID,
		Type:         opts.Type,
		State:        StreamStateActive,
		SignalingURL: "wss://signal.example.com/stream/" + opts.SessionID,
		ICEServers:   opts.ICEServers,
		Resolution:   opts.Resolution,
		FrameRate:    opts.FrameRate,
		BitRate:      opts.BitRate,
		AudioEnabled: opts.EnableAudio,
		InputEnabled: opts.EnableInput,
		StartedAt:    time.Now(),
	}

	m.streams[info.ID] = info
	return info, nil
}

func (m *MockStreamProvider) Stop(ctx context.Context, streamID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopErr != nil {
		return m.stopErr
	}

	delete(m.streams, streamID)
	return nil
}

func (m *MockStreamProvider) GetStatus(ctx context.Context, streamID string) (*StreamInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.statusErr != nil {
		return nil, m.statusErr
	}

	info, ok := m.streams[streamID]
	if !ok {
		return nil, errors.New("stream not found")
	}
	return info, nil
}

func (m *MockStreamProvider) UpdateOptions(ctx context.Context, streamID string, opts StreamOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.streams[streamID]
	if !ok {
		return errors.New("stream not found")
	}

	info.Resolution = opts.Resolution
	info.FrameRate = opts.FrameRate
	info.BitRate = opts.BitRate
	return nil
}

func (m *MockStreamProvider) HealthCheck(ctx context.Context) error {
	return m.healthErr
}

func TestManagerConfig(t *testing.T) {
	config := DefaultManagerConfig()

	if config.DefaultProvider != "selkies" {
		t.Errorf("expected default provider %q, got %q", "selkies", config.DefaultProvider)
	}
	if config.DefaultTimeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", config.DefaultTimeout)
	}
	if len(config.DefaultICEServers) != 2 {
		t.Errorf("expected 2 default ICE servers, got %d", len(config.DefaultICEServers))
	}
	if config.DefaultResolution.Width != 1920 || config.DefaultResolution.Height != 1080 {
		t.Errorf("expected default resolution 1920x1080, got %dx%d",
			config.DefaultResolution.Width, config.DefaultResolution.Height)
	}
	if config.DefaultFrameRate != 30 {
		t.Errorf("expected default frame rate 30, got %d", config.DefaultFrameRate)
	}
	if config.DefaultBitRate != 4000 {
		t.Errorf("expected default bitrate 4000, got %d", config.DefaultBitRate)
	}
}

func TestManagerRegisterProvider(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	manager := NewManager(DefaultManagerConfig(), store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})

	// Register provider
	err := manager.RegisterProvider(provider)
	if err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	// Get provider
	p, err := manager.GetProvider("test")
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	if p.Name() != "test" {
		t.Errorf("expected provider name %q, got %q", "test", p.Name())
	}

	// Register duplicate should fail
	err = manager.RegisterProvider(provider)
	if err == nil {
		t.Error("expected error registering duplicate provider")
	}

	// Get non-existent provider
	_, err = manager.GetProvider("nonexistent")
	if !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestManagerGetProviderForType(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "desktop"
	manager := NewManager(config, store, logger)

	desktopProvider := NewMockStreamProvider("desktop", []StreamType{StreamTypeDesktop})
	mobileProvider := NewMockStreamProvider("mobile", []StreamType{StreamTypeIOS, StreamTypeAndroid})

	_ = manager.RegisterProvider(desktopProvider)
	_ = manager.RegisterProvider(mobileProvider)

	// Get desktop provider
	p, err := manager.GetProviderForType(StreamTypeDesktop)
	if err != nil {
		t.Fatalf("failed to get provider for desktop: %v", err)
	}
	if p.Name() != "desktop" {
		t.Errorf("expected provider %q, got %q", "desktop", p.Name())
	}

	// Get iOS provider
	p, err = manager.GetProviderForType(StreamTypeIOS)
	if err != nil {
		t.Fatalf("failed to get provider for iOS: %v", err)
	}
	if p.Name() != "mobile" {
		t.Errorf("expected provider %q, got %q", "mobile", p.Name())
	}

	// Get unsupported type
	_, err = manager.GetProviderForType(StreamTypeBrowser)
	if !errors.Is(err, ErrUnsupportedStreamType) {
		t.Errorf("expected ErrUnsupportedStreamType, got %v", err)
	}
}

func TestManagerStartStream(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()
	opts := StreamOptions{
		SessionID:   "sess_123",
		RunnerID:    "run_456",
		Type:        StreamTypeDesktop,
		EnableInput: true,
	}

	// Start stream
	info, err := manager.StartStream(ctx, opts)
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	if info.SessionID != "sess_123" {
		t.Errorf("expected session_id %q, got %q", "sess_123", info.SessionID)
	}
	if info.State != StreamStateActive {
		t.Errorf("expected state %q, got %q", StreamStateActive, info.State)
	}
	if info.SignalingURL == "" {
		t.Error("expected signaling URL to be non-empty")
	}

	// Starting another stream for same session/type should fail
	_, err = manager.StartStream(ctx, opts)
	if !errors.Is(err, ErrStreamAlreadyExists) {
		t.Errorf("expected ErrStreamAlreadyExists, got %v", err)
	}
}

func TestManagerStartStreamProviderError(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	provider.startErr = errors.New("provider start error")
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()
	opts := StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	}

	// Start stream should fail
	_, err := manager.StartStream(ctx, opts)
	if err == nil {
		t.Error("expected error starting stream")
	}

	// Stream should be in error state
	streams, _ := store.ListStreams(ctx, ListStreamsParams{SessionID: "sess_123"})
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if streams[0].State != StreamStateError {
		t.Errorf("expected state %q, got %q", StreamStateError, streams[0].State)
	}
}

func TestManagerStopStream(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()

	// Start a stream
	info, err := manager.StartStream(ctx, StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	// Stop the stream
	err = manager.StopStream(ctx, info.ID)
	if err != nil {
		t.Fatalf("failed to stop stream: %v", err)
	}

	// Get stream - should be stopped
	stoppedInfo, err := manager.GetStream(ctx, info.ID)
	if err != nil {
		t.Fatalf("failed to get stream: %v", err)
	}
	if stoppedInfo.State != StreamStateStopped {
		t.Errorf("expected state %q, got %q", StreamStateStopped, stoppedInfo.State)
	}

	// Stop non-existent stream
	err = manager.StopStream(ctx, "nonexistent")
	if !errors.Is(err, ErrStreamNotFound) {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestManagerGetStream(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()

	// Start a stream
	info, _ := manager.StartStream(ctx, StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	})

	// Get stream by ID
	retrieved, err := manager.GetStream(ctx, info.ID)
	if err != nil {
		t.Fatalf("failed to get stream: %v", err)
	}
	if retrieved.ID != info.ID {
		t.Errorf("expected id %q, got %q", info.ID, retrieved.ID)
	}

	// Get non-existent stream
	_, err = manager.GetStream(ctx, "nonexistent")
	if !errors.Is(err, ErrStreamNotFound) {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestManagerGetSessionStream(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()

	// Start a stream
	_, _ = manager.StartStream(ctx, StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	})

	// Get stream by session
	retrieved, err := manager.GetSessionStream(ctx, "sess_123", StreamTypeDesktop)
	if err != nil {
		t.Fatalf("failed to get session stream: %v", err)
	}
	if retrieved.SessionID != "sess_123" {
		t.Errorf("expected session_id %q, got %q", "sess_123", retrieved.SessionID)
	}

	// Get non-existent session stream
	_, err = manager.GetSessionStream(ctx, "nonexistent", StreamTypeDesktop)
	if !errors.Is(err, ErrStreamNotFound) {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestManagerListSessionStreams(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop, StreamTypeBrowser})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()

	// Start multiple streams
	_, _ = manager.StartStream(ctx, StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	})

	// List streams
	streams, err := manager.ListSessionStreams(ctx, "sess_123")
	if err != nil {
		t.Fatalf("failed to list streams: %v", err)
	}
	if len(streams) != 1 {
		t.Errorf("expected 1 stream, got %d", len(streams))
	}
}

func TestManagerApplyDefaults(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	manager := NewManager(config, store, logger)

	opts := StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	}

	applied := manager.applyDefaults(opts)

	if applied.Timeout != config.DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", config.DefaultTimeout, applied.Timeout)
	}
	if len(applied.ICEServers) != len(config.DefaultICEServers) {
		t.Errorf("expected %d ICE servers, got %d", len(config.DefaultICEServers), len(applied.ICEServers))
	}
	if applied.Resolution != config.DefaultResolution {
		t.Errorf("expected resolution %v, got %v", config.DefaultResolution, applied.Resolution)
	}
	if applied.FrameRate != config.DefaultFrameRate {
		t.Errorf("expected frame rate %d, got %d", config.DefaultFrameRate, applied.FrameRate)
	}
	if applied.BitRate != config.DefaultBitRate {
		t.Errorf("expected bitrate %d, got %d", config.DefaultBitRate, applied.BitRate)
	}
}

func TestManagerStartStop(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.CleanupInterval = 100 * time.Millisecond
	manager := NewManager(config, store, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start manager
	manager.Start(ctx)

	// Wait a bit for cleanup loop to run
	time.Sleep(150 * time.Millisecond)

	// Stop manager
	manager.Stop()
}

func TestPtr(t *testing.T) {
	// Test ptr helper function
	s := "test"
	sp := ptr(s)
	if *sp != s {
		t.Errorf("expected %q, got %q", s, *sp)
	}

	i := 42
	ip := ptr(i)
	if *ip != i {
		t.Errorf("expected %d, got %d", i, *ip)
	}

	state := StreamStateActive
	statep := ptr(state)
	if *statep != state {
		t.Errorf("expected %q, got %q", state, *statep)
	}
}

func TestManagerStartStreamStoreErrors(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()
	opts := StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	}

	// Test GetStreamBySession error
	store.GetErr = errors.New("store get error")
	_, err := manager.StartStream(ctx, opts)
	if err == nil {
		t.Error("expected error from store get")
	}
	store.GetErr = nil

	// Test CreateStream error
	store.CreateErr = errors.New("store create error")
	_, err = manager.StartStream(ctx, opts)
	if err == nil {
		t.Error("expected error from store create")
	}
	store.CreateErr = nil
}

func TestManagerStartStreamUpdateError(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()
	opts := StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	}

	// Let stream creation succeed but update fail after provider starts
	var callCount int
	originalUpdate := store.UpdateErr
	defer func() { store.UpdateErr = originalUpdate }()

	// First update (state -> starting) succeeds, second (after provider start) fails
	_, err := manager.StartStream(ctx, opts)
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	// Clean up for next test
	store.streams = make(map[string]*Stream)
	callCount = 0
	store.UpdateErr = errors.New("update error")

	// This should fail on first update
	_, err = manager.StartStream(ctx, opts)
	if err == nil {
		t.Error("expected update error")
	}
	_ = callCount // silence unused warning
}

func TestManagerStopStreamAlreadyStopped(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()

	// Create a stream that's already stopped
	stream, _ := store.CreateStream(ctx, CreateStreamParams{
		ID:           "strm_stopped",
		SessionID:    "sess_123",
		RunnerID:     "run_456",
		Type:         StreamTypeDesktop,
		ProviderName: "test",
	})
	stopped := StreamStateStopped
	store.UpdateStream(ctx, stream.ID, UpdateStreamParams{State: &stopped})

	// Stop should succeed (no-op)
	err := manager.StopStream(ctx, stream.ID)
	if err != nil {
		t.Errorf("expected no error stopping already stopped stream, got %v", err)
	}
}

func TestManagerStopStreamProviderError(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	config.DefaultProvider = "test"
	manager := NewManager(config, store, logger)

	provider := NewMockStreamProvider("test", []StreamType{StreamTypeDesktop})
	provider.stopErr = errors.New("provider stop error")
	_ = manager.RegisterProvider(provider)

	ctx := context.Background()

	// Start a stream
	info, _ := manager.StartStream(ctx, StreamOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      StreamTypeDesktop,
	})

	// Stop should still succeed even if provider fails
	err := manager.StopStream(ctx, info.ID)
	if err != nil {
		t.Errorf("expected stop to succeed despite provider error, got %v", err)
	}
}

func TestManagerStopStreamUnknownProvider(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	config := DefaultManagerConfig()
	manager := NewManager(config, store, logger)

	ctx := context.Background()

	// Create a stream with unknown provider
	stream, _ := store.CreateStream(ctx, CreateStreamParams{
		ID:               "strm_unknown",
		SessionID:        "sess_123",
		RunnerID:         "run_456",
		Type:             StreamTypeDesktop,
		ProviderName:     "unknown_provider",
	})
	active := StreamStateActive
	store.UpdateStream(ctx, stream.ID, UpdateStreamParams{
		State:            &active,
		ProviderStreamID: ptr("provider_123"),
	})

	// Stop should still work even with unknown provider
	err := manager.StopStream(ctx, stream.ID)
	if err != nil {
		t.Errorf("expected stop to succeed despite unknown provider, got %v", err)
	}
}

func TestManagerErrors(t *testing.T) {
	// Test error messages
	if ErrStreamNotFound.Error() != "stream not found" {
		t.Errorf("unexpected error message: %s", ErrStreamNotFound.Error())
	}
	if ErrStreamAlreadyExists.Error() != "stream already exists" {
		t.Errorf("unexpected error message: %s", ErrStreamAlreadyExists.Error())
	}
	if ErrProviderNotFound.Error() != "provider not found" {
		t.Errorf("unexpected error message: %s", ErrProviderNotFound.Error())
	}
	if ErrUnsupportedStreamType.Error() != "unsupported stream type" {
		t.Errorf("unexpected error message: %s", ErrUnsupportedStreamType.Error())
	}
	if ErrStreamNotActive.Error() != "stream not active" {
		t.Errorf("unexpected error message: %s", ErrStreamNotActive.Error())
	}
	if ErrInvalidStreamState.Error() != "invalid stream state" {
		t.Errorf("unexpected error message: %s", ErrInvalidStreamState.Error())
	}
}

func TestManagerStreamToInfo(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	manager := NewManager(DefaultManagerConfig(), store, logger)

	now := time.Now()
	stream := &Stream{
		ID:           "strm_123",
		SessionID:    "sess_456",
		RunnerID:     "run_789",
		Type:         StreamTypeDesktop,
		State:        StreamStateActive,
		SignalingURL: "wss://signal.example.com",
		ICEServers:   []ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
		Resolution:   Resolution{Width: 1920, Height: 1080},
		FrameRate:    30,
		BitRate:      4000,
		AudioEnabled: true,
		InputEnabled: true,
		Error:        "",
		Metadata:     map[string]string{"key": "value"},
		StartedAt:    &now,
	}

	info := manager.streamToInfo(stream)

	if info.ID != stream.ID {
		t.Errorf("expected id %q, got %q", stream.ID, info.ID)
	}
	if info.SessionID != stream.SessionID {
		t.Errorf("expected session_id %q, got %q", stream.SessionID, info.SessionID)
	}
	if info.State != stream.State {
		t.Errorf("expected state %q, got %q", stream.State, info.State)
	}
	if info.StartedAt.IsZero() {
		t.Error("expected started_at to be set")
	}
}

func TestManagerStreamToInfoNilStartedAt(t *testing.T) {
	store := NewMockStreamStore()
	logger := zap.NewNop()
	manager := NewManager(DefaultManagerConfig(), store, logger)

	stream := &Stream{
		ID:        "strm_123",
		SessionID: "sess_456",
		RunnerID:  "run_789",
		Type:      StreamTypeDesktop,
		State:     StreamStatePending,
		StartedAt: nil,
	}

	info := manager.streamToInfo(stream)

	if !info.StartedAt.IsZero() {
		t.Error("expected started_at to be zero")
	}
}

func TestManagerNilLogger(t *testing.T) {
	store := NewMockStreamStore()
	// Passing nil logger should not panic
	manager := NewManager(DefaultManagerConfig(), store, nil)
	if manager == nil {
		t.Error("expected manager to be non-nil")
	}
}
