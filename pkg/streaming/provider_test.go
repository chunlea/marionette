package streaming

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a mock implementation of StreamProvider for testing.
type mockProvider struct {
	name           string
	supportedTypes []StreamType
	startFunc      func(ctx context.Context, opts StreamOptions) (*StreamInfo, error)
	stopFunc       func(ctx context.Context, providerStreamID string) error
	getInfoFunc    func(ctx context.Context, providerStreamID string) (*StreamInfo, error)
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) SupportedTypes() []StreamType {
	return m.supportedTypes
}

func (m *mockProvider) Start(ctx context.Context, opts StreamOptions) (*StreamInfo, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx, opts)
	}
	return &StreamInfo{ID: "mock_stream_id"}, nil
}

func (m *mockProvider) Stop(ctx context.Context, providerStreamID string) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, providerStreamID)
	}
	return nil
}

func (m *mockProvider) GetInfo(ctx context.Context, providerStreamID string) (*StreamInfo, error) {
	if m.getInfoFunc != nil {
		return m.getInfoFunc(ctx, providerStreamID)
	}
	return &StreamInfo{ID: providerStreamID}, nil
}

func newMockProvider(name string, types ...StreamType) *mockProvider {
	return &mockProvider{
		name:           name,
		supportedTypes: types,
	}
}

func TestNewProviderRegistry(t *testing.T) {
	registry := NewProviderRegistry()
	require.NotNil(t, registry)
	assert.Empty(t, registry.List())
}

func TestProviderRegistry_Register(t *testing.T) {
	registry := NewProviderRegistry()

	// Register a provider
	provider := newMockProvider("desktop", StreamTypeDesktop)
	err := registry.Register(provider)
	require.NoError(t, err)

	// Verify it was registered
	got, ok := registry.Get("desktop")
	assert.True(t, ok)
	assert.Equal(t, provider, got)
}

func TestProviderRegistry_Register_NilProvider(t *testing.T) {
	registry := NewProviderRegistry()
	err := registry.Register(nil)
	assert.ErrorIs(t, err, ErrProviderNotFound)
}

func TestProviderRegistry_Register_Duplicate(t *testing.T) {
	registry := NewProviderRegistry()

	// Register first provider
	provider1 := newMockProvider("desktop", StreamTypeDesktop)
	err := registry.Register(provider1)
	require.NoError(t, err)

	// Try to register another with the same name
	provider2 := newMockProvider("desktop", StreamTypeBrowser)
	err = registry.Register(provider2)
	assert.ErrorIs(t, err, ErrProviderExists)
}

func TestProviderRegistry_Get(t *testing.T) {
	registry := NewProviderRegistry()

	// Get non-existent provider
	_, ok := registry.Get("nonexistent")
	assert.False(t, ok)

	// Register and get provider
	provider := newMockProvider("test", StreamTypeDesktop)
	err := registry.Register(provider)
	require.NoError(t, err)

	got, ok := registry.Get("test")
	assert.True(t, ok)
	assert.Equal(t, provider, got)
}

func TestProviderRegistry_GetForType(t *testing.T) {
	registry := NewProviderRegistry()

	// Get for type with no providers
	_, err := registry.GetForType(StreamTypeDesktop)
	assert.ErrorIs(t, err, ErrNoProviderForType)

	// Register desktop provider
	desktopProvider := newMockProvider("desktop", StreamTypeDesktop)
	err = registry.Register(desktopProvider)
	require.NoError(t, err)

	// Register android provider
	androidProvider := newMockProvider("android", StreamTypeAndroid)
	err = registry.Register(androidProvider)
	require.NoError(t, err)

	// Get for desktop type
	got, err := registry.GetForType(StreamTypeDesktop)
	require.NoError(t, err)
	assert.Equal(t, desktopProvider, got)

	// Get for android type
	got, err = registry.GetForType(StreamTypeAndroid)
	require.NoError(t, err)
	assert.Equal(t, androidProvider, got)

	// Get for unsupported type
	_, err = registry.GetForType(StreamTypeIOS)
	assert.ErrorIs(t, err, ErrNoProviderForType)
}

func TestProviderRegistry_GetForType_MultipleTypes(t *testing.T) {
	registry := NewProviderRegistry()

	// Register provider that supports multiple types
	multiProvider := newMockProvider("multi", StreamTypeDesktop, StreamTypeBrowser)
	err := registry.Register(multiProvider)
	require.NoError(t, err)

	// Should find provider for both types
	got, err := registry.GetForType(StreamTypeDesktop)
	require.NoError(t, err)
	assert.Equal(t, multiProvider, got)

	got, err = registry.GetForType(StreamTypeBrowser)
	require.NoError(t, err)
	assert.Equal(t, multiProvider, got)
}

func TestProviderRegistry_List(t *testing.T) {
	registry := NewProviderRegistry()

	// Empty list
	assert.Empty(t, registry.List())

	// Add providers
	provider1 := newMockProvider("provider1", StreamTypeDesktop)
	provider2 := newMockProvider("provider2", StreamTypeAndroid)

	err := registry.Register(provider1)
	require.NoError(t, err)
	err = registry.Register(provider2)
	require.NoError(t, err)

	// List should contain both
	providers := registry.List()
	assert.Len(t, providers, 2)
}

func TestProviderRegistry_Unregister(t *testing.T) {
	registry := NewProviderRegistry()

	// Unregister non-existent
	ok := registry.Unregister("nonexistent")
	assert.False(t, ok)

	// Register and unregister
	provider := newMockProvider("test", StreamTypeDesktop)
	err := registry.Register(provider)
	require.NoError(t, err)

	ok = registry.Unregister("test")
	assert.True(t, ok)

	// Verify it's gone
	_, exists := registry.Get("test")
	assert.False(t, exists)

	// Can't unregister again
	ok = registry.Unregister("test")
	assert.False(t, ok)
}

func TestMockProvider_Start(t *testing.T) {
	provider := newMockProvider("test", StreamTypeDesktop)

	// Default behavior
	info, err := provider.Start(context.Background(), StreamOptions{})
	require.NoError(t, err)
	assert.Equal(t, "mock_stream_id", info.ID)

	// Custom behavior
	provider.startFunc = func(ctx context.Context, opts StreamOptions) (*StreamInfo, error) {
		return &StreamInfo{
			ID:         "custom_id",
			Resolution: Resolution{1920, 1080},
		}, nil
	}

	info, err = provider.Start(context.Background(), StreamOptions{})
	require.NoError(t, err)
	assert.Equal(t, "custom_id", info.ID)
	assert.Equal(t, 1920, info.Resolution.Width)
}

func TestMockProvider_Stop(t *testing.T) {
	provider := newMockProvider("test", StreamTypeDesktop)

	// Default behavior
	err := provider.Stop(context.Background(), "stream_id")
	assert.NoError(t, err)

	// Custom behavior with error
	provider.stopFunc = func(ctx context.Context, providerStreamID string) error {
		return ErrStreamNotFound
	}

	err = provider.Stop(context.Background(), "stream_id")
	assert.ErrorIs(t, err, ErrStreamNotFound)
}

func TestMockProvider_GetInfo(t *testing.T) {
	provider := newMockProvider("test", StreamTypeDesktop)

	// Default behavior
	info, err := provider.GetInfo(context.Background(), "stream_123")
	require.NoError(t, err)
	assert.Equal(t, "stream_123", info.ID)

	// Custom behavior
	provider.getInfoFunc = func(ctx context.Context, providerStreamID string) (*StreamInfo, error) {
		return &StreamInfo{
			ID:         providerStreamID,
			VideoCodec: "H264",
			AudioCodec: "opus",
		}, nil
	}

	info, err = provider.GetInfo(context.Background(), "stream_456")
	require.NoError(t, err)
	assert.Equal(t, "stream_456", info.ID)
	assert.Equal(t, "H264", info.VideoCodec)
}

// Ensure mockProvider implements StreamProvider
var _ StreamProvider = (*mockProvider)(nil)
