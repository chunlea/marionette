package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name   string
	closed bool
}

func (p *mockProvider) Name() string                    { return p.name }
func (p *mockProvider) Type() ProviderType              { return ProviderTypeManaged }
func (p *mockProvider) Capabilities() ProviderCapabilities { return ProviderCapabilities{} }
func (p *mockProvider) Spawn(ctx context.Context, opts SpawnOptions) (*RunnerInstance, error) {
	return nil, nil
}
func (p *mockProvider) Destroy(ctx context.Context, runnerID string) error { return nil }
func (p *mockProvider) Status(ctx context.Context, runnerID string) (*RunnerStatus, error) {
	return nil, nil
}
func (p *mockProvider) List(ctx context.Context) ([]*RunnerInstance, error) { return nil, nil }
func (p *mockProvider) Close() error {
	p.closed = true
	return nil
}

// mockStore implements the minimal store interface for testing.
type mockStore struct {
	configs map[string]*store.ProviderConfig
}

func (s *mockStore) GetProviderConfigByName(ctx context.Context, name string) (*store.ProviderConfig, error) {
	if cfg, ok := s.configs[name]; ok {
		return cfg, nil
	}
	return nil, &ErrProviderNotFound{Name: name}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockProvider{name: "test-provider"}
	err := r.Register("test-provider", p)
	require.NoError(t, err)

	got, err := r.Get(context.Background(), "test-provider")
	require.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry(nil)

	_, err := r.Get(context.Background(), "nonexistent")
	assert.Error(t, err)

	var notFoundErr *ErrProviderNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

func TestRegistry_DefaultProvider(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockProvider{name: "default-provider"}
	_ = r.Register("default-provider", p)
	r.SetDefault("default-provider")

	got, err := r.GetDefault(context.Background())
	require.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestRegistry_DefaultNotSet(t *testing.T) {
	r := NewRegistry(nil)

	_, err := r.GetDefault(context.Background())
	assert.Error(t, err)

	var notFoundErr *ErrProviderNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry(nil)

	_ = r.Register("provider-1", &mockProvider{name: "provider-1"})
	_ = r.Register("provider-2", &mockProvider{name: "provider-2"})

	names := r.List()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "provider-1")
	assert.Contains(t, names, "provider-2")
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockProvider{name: "test"}
	_ = r.Register("test", p)

	assert.True(t, r.Has("test"))

	r.Unregister("test")

	assert.False(t, r.Has("test"))
}

func TestRegistry_Close(t *testing.T) {
	r := NewRegistry(nil)

	p1 := &mockProvider{name: "p1"}
	p2 := &mockProvider{name: "p2"}

	_ = r.Register("p1", p1)
	_ = r.Register("p2", p2)

	err := r.Close()
	require.NoError(t, err)

	assert.True(t, p1.closed)
	assert.True(t, p2.closed)
	assert.Empty(t, r.List())
}

func TestRegistry_RegisterFactory(t *testing.T) {
	r := NewRegistry(nil)

	factory := func(cfg *store.ProviderConfig) (Provider, error) {
		return &mockProvider{name: cfg.Name}, nil
	}

	r.RegisterFactory("mock", factory)

	types := r.GetFactoryTypes()
	assert.Contains(t, types, "mock")
}

func TestRegistry_CreateFromConfig(t *testing.T) {
	r := NewRegistry(nil)

	factory := func(cfg *store.ProviderConfig) (Provider, error) {
		return &mockProvider{name: cfg.Name}, nil
	}
	r.RegisterFactory("mock", factory)

	cfg := &store.ProviderConfig{
		Name:     "test-config",
		Provider: "mock",
		Config:   json.RawMessage(`{}`),
	}

	p, err := r.CreateFromConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "test-config", p.Name())
}

func TestRegistry_LoadFromDB(t *testing.T) {
	mockS := &mockStore{
		configs: map[string]*store.ProviderConfig{
			"db-provider": {
				Name:     "db-provider",
				Provider: "mock",
				Config:   json.RawMessage(`{}`),
			},
		},
	}

	r := NewRegistryWithStore(mockS)

	factory := func(cfg *store.ProviderConfig) (Provider, error) {
		return &mockProvider{name: cfg.Name}, nil
	}
	r.RegisterFactory("mock", factory)

	// First call should load from DB
	p, err := r.Get(context.Background(), "db-provider")
	require.NoError(t, err)
	assert.Equal(t, "db-provider", p.Name())

	// Second call should use cache
	p2, err := r.Get(context.Background(), "db-provider")
	require.NoError(t, err)
	assert.Same(t, p, p2)
}

func TestRegistry_DefaultName(t *testing.T) {
	r := NewRegistry(nil)

	assert.Empty(t, r.DefaultName())

	r.SetDefault("my-default")
	assert.Equal(t, "my-default", r.DefaultName())
}

func TestRegistry_CreateFromConfig_NoFactory(t *testing.T) {
	r := NewRegistry(nil)

	cfg := &store.ProviderConfig{
		Name:     "test-config",
		Provider: "unknown",
		Config:   json.RawMessage(`{}`),
	}

	_, err := r.CreateFromConfig(cfg)
	assert.Error(t, err)

	var notFoundErr *ErrProviderNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

func TestRegistry_LoadFromDB_NoStore(t *testing.T) {
	r := NewRegistry(nil)

	_, err := r.Get(context.Background(), "nonexistent")
	assert.Error(t, err)

	var notFoundErr *ErrProviderNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

func TestRegistry_LoadFromDB_NoFactory(t *testing.T) {
	mockS := &mockStore{
		configs: map[string]*store.ProviderConfig{
			"db-provider": {
				Name:     "db-provider",
				Provider: "unknown", // No factory registered
				Config:   json.RawMessage(`{}`),
			},
		},
	}

	r := NewRegistryWithStore(mockS)

	_, err := r.Get(context.Background(), "db-provider")
	assert.Error(t, err)

	var notFoundErr *ErrProviderNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

// Provider tests

func TestDefaultSuspendConfig(t *testing.T) {
	cfg := DefaultSuspendConfig()

	assert.Equal(t, SuspendStrategyPause, cfg.Strategy)
	assert.Equal(t, 60*time.Second, cfg.MinDuration)
	assert.Equal(t, 24*time.Hour, cfg.MaxDuration)
	assert.Equal(t, SuspendStrategyTerminatePreserveStorage, cfg.Fallback)
}
