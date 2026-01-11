package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderConfig_Validate(t *testing.T) {
	t.Run("empty endpoint", func(t *testing.T) {
		cfg := &ProviderConfig{}
		err := cfg.Validate()
		assert.Equal(t, ErrBrowserNotConnected, err)
	})

	t.Run("valid config with defaults", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222"}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, DefaultBufferSize, cfg.BufferSize)
		assert.Equal(t, 30, cfg.ConnectTimeoutSeconds)
		assert.NotNil(t, cfg.ReconnectEnabled)
		assert.True(t, *cfg.ReconnectEnabled)
		assert.Equal(t, 0, cfg.ReconnectMaxAttempts) // 0 means unlimited
		assert.Equal(t, 1, cfg.ReconnectDelaySeconds)
		assert.Equal(t, 30, cfg.ReconnectMaxDelaySeconds)
	})

	t.Run("negative buffer size uses default", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", BufferSize: -1}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, DefaultBufferSize, cfg.BufferSize)
	})

	t.Run("exceeds max buffer size", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", BufferSize: MaxBufferSize + 100}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, MaxBufferSize, cfg.BufferSize)
	})

	t.Run("negative connect timeout uses default", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", ConnectTimeoutSeconds: -1}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, 30, cfg.ConnectTimeoutSeconds)
	})

	t.Run("negative reconnect delay uses default", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", ReconnectDelaySeconds: -1}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.ReconnectDelaySeconds)
	})

	t.Run("negative reconnect max delay uses default", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", ReconnectMaxDelaySeconds: -1}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, 30, cfg.ReconnectMaxDelaySeconds)
	})

	t.Run("negative reconnect max attempts resets to zero", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", ReconnectMaxAttempts: -1}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, 5, cfg.ReconnectMaxAttempts) // -1 becomes 5 (default)
	})

	t.Run("zero reconnect max attempts means unlimited", func(t *testing.T) {
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", ReconnectMaxAttempts: 0}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, 0, cfg.ReconnectMaxAttempts) // 0 stays 0 (unlimited)
	})

	t.Run("explicit reconnect enabled false", func(t *testing.T) {
		disabled := false
		cfg := &ProviderConfig{CDPEndpoint: "ws://localhost:9222", ReconnectEnabled: &disabled}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.False(t, *cfg.ReconnectEnabled)
	})
}

func TestProviderConfig_Clone(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		var cfg *ProviderConfig
		cloned := cfg.Clone()
		assert.Nil(t, cloned)
	})

	t.Run("clone without reconnect enabled", func(t *testing.T) {
		cfg := &ProviderConfig{
			CDPEndpoint:           "ws://localhost:9222",
			BufferSize:            20,
			ConnectTimeoutSeconds: 60,
		}
		cloned := cfg.Clone()
		require.NotNil(t, cloned)
		assert.Equal(t, cfg.CDPEndpoint, cloned.CDPEndpoint)
		assert.Equal(t, cfg.BufferSize, cloned.BufferSize)
		assert.Equal(t, cfg.ConnectTimeoutSeconds, cloned.ConnectTimeoutSeconds)
		assert.Nil(t, cloned.ReconnectEnabled)

		// Modify original shouldn't affect clone
		cfg.BufferSize = 100
		assert.Equal(t, 20, cloned.BufferSize)
	})

	t.Run("clone with reconnect enabled", func(t *testing.T) {
		enabled := true
		cfg := &ProviderConfig{
			CDPEndpoint:      "ws://localhost:9222",
			ReconnectEnabled: &enabled,
		}
		cloned := cfg.Clone()
		require.NotNil(t, cloned)
		require.NotNil(t, cloned.ReconnectEnabled)
		assert.True(t, *cloned.ReconnectEnabled)

		// Modify original shouldn't affect clone
		*cfg.ReconnectEnabled = false
		assert.True(t, *cloned.ReconnectEnabled)
	})
}

// mockProviderFactory is a simple mock for testing Registry.
type mockProviderFactory struct {
	name string
}

func (f *mockProviderFactory) Create(_ *ProviderConfig) (Provider, error) {
	return nil, nil
}

func (f *mockProviderFactory) Name() string {
	return f.name
}

func TestRegistry(t *testing.T) {
	t.Run("new registry is empty", func(t *testing.T) {
		r := NewRegistry()
		assert.Empty(t, r.List())
	})

	t.Run("register and get factory", func(t *testing.T) {
		r := NewRegistry()
		factory := &mockProviderFactory{name: "cdp"}
		r.Register(factory)

		got, ok := r.Get("cdp")
		assert.True(t, ok)
		assert.Equal(t, factory, got)
	})

	t.Run("get non-existent factory", func(t *testing.T) {
		r := NewRegistry()
		got, ok := r.Get("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("list registered factories", func(t *testing.T) {
		r := NewRegistry()
		r.Register(&mockProviderFactory{name: "cdp"})
		r.Register(&mockProviderFactory{name: "vnc"})

		names := r.List()
		assert.Len(t, names, 2)
		assert.Contains(t, names, "cdp")
		assert.Contains(t, names, "vnc")
	})

	t.Run("register replaces existing", func(t *testing.T) {
		r := NewRegistry()
		factory1 := &mockProviderFactory{name: "cdp"}
		factory2 := &mockProviderFactory{name: "cdp"}
		r.Register(factory1)
		r.Register(factory2)

		got, _ := r.Get("cdp")
		assert.Equal(t, factory2, got)
	})
}
