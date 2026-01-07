package tunnel

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// mockStore implements a simple in-memory store for testing.
type mockStore struct {
	tunnels map[string]*storedTunnel
	mu      sync.RWMutex
}

type storedTunnel struct {
	tunnel      *Tunnel
	tokenHash   string
	hashVersion int
}

func newMockStore() *mockStore {
	return &mockStore{
		tunnels: make(map[string]*storedTunnel),
	}
}

func (s *mockStore) CreateTunnel(_ context.Context, tunnel *Tunnel, tokenHash string, hashVersion int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnels[tunnel.ID] = &storedTunnel{
		tunnel:      tunnel,
		tokenHash:   tokenHash,
		hashVersion: hashVersion,
	}
	return nil
}

func (s *mockStore) GetTunnel(_ context.Context, id string) (*Tunnel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.tunnels[id]; ok {
		return st.tunnel, nil
	}
	return nil, ErrTunnelNotFound
}

func (s *mockStore) GetTunnelByHash(_ context.Context, hash string) (*Tunnel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, st := range s.tunnels {
		if st.tokenHash == hash {
			return st.tunnel, nil
		}
	}
	return nil, ErrTunnelNotFound
}

func (s *mockStore) ListTunnels(_ context.Context, opts ListOptions) ([]*Tunnel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Tunnel
	for _, st := range s.tunnels {
		if opts.SessionID != "" && st.tunnel.SessionID != opts.SessionID {
			continue
		}
		if opts.RunnerID != "" && st.tunnel.RunnerID != opts.RunnerID {
			continue
		}
		if !opts.IncludeClosed && st.tunnel.ClosedAt != nil {
			continue
		}
		result = append(result, st.tunnel)
	}
	return result, nil
}

func (s *mockStore) UpdateTunnel(_ context.Context, id string, updates Updates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.tunnels[id]
	if !ok {
		return ErrTunnelNotFound
	}
	if updates.PublicURL != nil {
		st.tunnel.PublicURL = *updates.PublicURL
	}
	if updates.ClosedAt != nil {
		st.tunnel.ClosedAt = updates.ClosedAt
	}
	return nil
}

func (s *mockStore) DeleteExpiredTunnels(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	now := time.Now()
	for id, st := range s.tunnels {
		if st.tunnel.ExpiresAt.Before(now) {
			delete(s.tunnels, id)
			count++
		}
	}
	return count, nil
}

// mockConnectionHandler implements ConnectionHandler for testing.
type mockConnectionHandler struct {
	connected bool
	mu        sync.RWMutex
}

func newMockConnectionHandler() *mockConnectionHandler {
	return &mockConnectionHandler{connected: true}
}

func (h *mockConnectionHandler) SendTunnelData(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (h *mockConnectionHandler) ReceiveTunnelData(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (h *mockConnectionHandler) CloseTunnel(_ context.Context, _ string) error {
	return nil
}

func (h *mockConnectionHandler) IsConnected() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.connected
}

func (h *mockConnectionHandler) SetConnected(connected bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connected = connected
}

func TestNewTunnelManager(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("with defaults", func(t *testing.T) {
		m := NewTunnelManager()
		assert.NotNil(t, m)
		assert.NotNil(t, m.tunnels)
		assert.NotNil(t, m.handlers)
		assert.NotNil(t, m.idGen)
		assert.Equal(t, "http://localhost:8080", m.baseURL)
	})

	t.Run("with options", func(t *testing.T) {
		store := newMockStore()
		customIDGen := func() string { return "custom_id" }

		m := NewTunnelManager(
			WithStore(store),
			WithLogger(logger),
			WithBaseURL("http://example.com"),
			WithIDGen(customIDGen),
		)

		assert.NotNil(t, m)
		assert.Equal(t, store, m.store)
		assert.Equal(t, "http://example.com", m.baseURL)
		assert.Equal(t, "custom_id", m.idGen())
	})
}

func TestTunnelManager_Create(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
		WithIDGen(func() string { return "tun_test123" }),
	)

	t.Run("create valid tunnel", func(t *testing.T) {
		tunnel, err := m.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_123",
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3000,
		})

		require.NoError(t, err)
		require.NotNil(t, tunnel)
		assert.Equal(t, "tun_test123", tunnel.ID)
		assert.Equal(t, "sess_123", tunnel.SessionID)
		assert.Equal(t, "run_456", tunnel.RunnerID)
		assert.Equal(t, TypeHTTP, tunnel.Type)
		assert.Equal(t, DirectionOutbound, tunnel.Direction)
		assert.Equal(t, 3000, tunnel.LocalPort)
		assert.NotEmpty(t, tunnel.Token)
		assert.NotEmpty(t, tunnel.PublicURL)
		assert.True(t, tunnel.ExpiresAt.After(time.Now()))
	})

	t.Run("create with custom TTL", func(t *testing.T) {
		m2 := NewTunnelManager(
			WithStore(newMockStore()),
			WithLogger(logger),
		)

		tunnel, err := m2.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_123",
			RunnerID:  "run_456",
			Type:      TypeTCP,
			LocalPort: 5432,
			TTL:       30 * time.Minute,
		})

		require.NoError(t, err)
		// TTL should be approximately 30 minutes
		expectedExpiry := time.Now().Add(30 * time.Minute)
		assert.WithinDuration(t, expectedExpiry, tunnel.ExpiresAt, time.Second)
	})

	t.Run("TTL exceeds max", func(t *testing.T) {
		m2 := NewTunnelManager(
			WithStore(newMockStore()),
			WithLogger(logger),
		)

		tunnel, err := m2.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_123",
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3000,
			TTL:       48 * time.Hour, // Exceeds MaxTTL
		})

		require.NoError(t, err)
		// Should be capped at MaxTTL (24 hours)
		expectedExpiry := time.Now().Add(MaxTTL)
		assert.WithinDuration(t, expectedExpiry, tunnel.ExpiresAt, time.Second)
	})

	t.Run("invalid options", func(t *testing.T) {
		_, err := m.Create(context.Background(), CreateTunnelOptions{
			// Missing SessionID
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session_id is required")
	})
}

func TestTunnelManager_Get(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create a tunnel first
	tunnel, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	t.Run("get existing tunnel from cache", func(t *testing.T) {
		got, err := m.Get(context.Background(), tunnel.ID)
		require.NoError(t, err)
		assert.Equal(t, tunnel.ID, got.ID)
	})

	t.Run("get non-existent tunnel", func(t *testing.T) {
		_, err := m.Get(context.Background(), "non_existent")
		require.Error(t, err)
		assert.Equal(t, ErrTunnelNotFound, err)
	})
}

func TestTunnelManager_GetBySession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create tunnels for different sessions
	_, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_456",
		Type:      TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	_, err = m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_456",
		Type:      TypeTCP,
		LocalPort: 5432,
	})
	require.NoError(t, err)

	_, err = m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_2",
		RunnerID:  "run_789",
		Type:      TypeHTTP,
		LocalPort: 8080,
	})
	require.NoError(t, err)

	t.Run("get tunnels for session 1", func(t *testing.T) {
		tunnels, err := m.GetBySession(context.Background(), "sess_1")
		require.NoError(t, err)
		assert.Len(t, tunnels, 2)
	})

	t.Run("get tunnels for session 2", func(t *testing.T) {
		tunnels, err := m.GetBySession(context.Background(), "sess_2")
		require.NoError(t, err)
		assert.Len(t, tunnels, 1)
	})

	t.Run("get tunnels for non-existent session", func(t *testing.T) {
		tunnels, err := m.GetBySession(context.Background(), "sess_none")
		require.NoError(t, err)
		assert.Len(t, tunnels, 0)
	})
}

func TestTunnelManager_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create and register a handler
	handler := newMockConnectionHandler()
	m.RegisterHandler("run_456", handler)

	// Create a tunnel
	tunnel, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	t.Run("close existing tunnel", func(t *testing.T) {
		err := m.Close(context.Background(), tunnel.ID)
		require.NoError(t, err)

		// Should not be in cache anymore
		_, err = m.Get(context.Background(), tunnel.ID)
		require.Error(t, err)
	})

	t.Run("close non-existent tunnel", func(t *testing.T) {
		err := m.Close(context.Background(), "non_existent")
		require.Error(t, err)
		assert.Equal(t, ErrTunnelNotFound, err)
	})
}

func TestTunnelManager_CloseBySession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create multiple tunnels for same session
	for i := 0; i < 3; i++ {
		_, err := m.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_close",
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3000 + i,
		})
		require.NoError(t, err)
	}

	// Verify tunnels exist
	tunnels, err := m.GetBySession(context.Background(), "sess_close")
	require.NoError(t, err)
	assert.Len(t, tunnels, 3)

	// Close all tunnels for session
	err = m.CloseBySession(context.Background(), "sess_close")
	require.NoError(t, err)

	// Verify tunnels are closed
	tunnels, err = m.GetBySession(context.Background(), "sess_close")
	require.NoError(t, err)
	assert.Len(t, tunnels, 0)
}

func TestTunnelManager_ValidateToken(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create a tunnel
	tunnel, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	t.Run("valid token", func(t *testing.T) {
		validated, err := m.ValidateToken(context.Background(), tunnel.ID, tunnel.Token)
		require.NoError(t, err)
		assert.Equal(t, tunnel.ID, validated.ID)
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := m.ValidateToken(context.Background(), tunnel.ID, "invalid_token")
		require.Error(t, err)
		assert.Equal(t, ErrInvalidToken, err)
	})

	t.Run("non-existent tunnel", func(t *testing.T) {
		_, err := m.ValidateToken(context.Background(), "non_existent", tunnel.Token)
		require.Error(t, err)
		assert.Equal(t, ErrTunnelNotFound, err)
	})

	t.Run("closed tunnel", func(t *testing.T) {
		// Create and close a tunnel
		closedTunnel, err := m.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_123",
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3001,
		})
		require.NoError(t, err)
		token := closedTunnel.Token

		err = m.Close(context.Background(), closedTunnel.ID)
		require.NoError(t, err)

		_, err = m.ValidateToken(context.Background(), closedTunnel.ID, token)
		require.Error(t, err)
		assert.Equal(t, ErrTunnelNotFound, err)
	})
}

func TestTunnelManager_RegisterHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)

	m := NewTunnelManager(
		WithLogger(logger),
	)

	handler := newMockConnectionHandler()

	t.Run("register handler", func(t *testing.T) {
		m.RegisterHandler("run_123", handler)

		m.handlersMu.RLock()
		_, exists := m.handlers["run_123"]
		m.handlersMu.RUnlock()

		assert.True(t, exists)
	})

	t.Run("unregister handler", func(t *testing.T) {
		m.UnregisterHandler("run_123")

		m.handlersMu.RLock()
		_, exists := m.handlers["run_123"]
		m.handlersMu.RUnlock()

		assert.False(t, exists)
	})
}

func TestTunnelManager_GetActiveCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	assert.Equal(t, 0, m.GetActiveCount())

	// Create tunnels
	for i := 0; i < 5; i++ {
		_, err := m.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_123",
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3000 + i,
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 5, m.GetActiveCount())

	// Close one tunnel
	tunnels, _ := m.GetBySession(context.Background(), "sess_123")
	err := m.Close(context.Background(), tunnels[0].ID)
	require.NoError(t, err)

	assert.Equal(t, 4, m.GetActiveCount())
}

func TestTunnelManager_CleanupExpired(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create tunnels with very short TTL
	for i := 0; i < 3; i++ {
		_, err := m.Create(context.Background(), CreateTunnelOptions{
			SessionID: "sess_123",
			RunnerID:  "run_456",
			Type:      TypeHTTP,
			LocalPort: 3000 + i,
			TTL:       -time.Second, // Already expired
		})
		require.NoError(t, err)
	}

	// Create one non-expired tunnel
	_, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeHTTP,
		LocalPort: 4000,
		TTL:       time.Hour,
	})
	require.NoError(t, err)

	// Manually expire tunnels in cache
	m.tunnelsMu.Lock()
	for _, active := range m.tunnels {
		if active.LocalPort < 4000 {
			active.ExpiresAt = time.Now().Add(-time.Hour)
		}
	}
	m.tunnelsMu.Unlock()

	// Cleanup expired
	cleaned, err := m.CleanupExpired(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, cleaned)
	assert.Equal(t, 1, m.GetActiveCount())
}

func TestTunnelManager_HandleHTTPRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create a tunnel
	tunnel, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	t.Run("without handler", func(t *testing.T) {
		err := m.HandleHTTPRequest(context.Background(), tunnel.ID, nil, nil)
		assert.Equal(t, ErrRunnerNotConnected, err)
	})

	t.Run("with disconnected handler", func(t *testing.T) {
		handler := newMockConnectionHandler()
		handler.SetConnected(false)
		m.RegisterHandler("run_456", handler)

		err := m.HandleHTTPRequest(context.Background(), tunnel.ID, nil, nil)
		assert.Equal(t, ErrRunnerNotConnected, err)
	})

	t.Run("non-existent tunnel", func(t *testing.T) {
		err := m.HandleHTTPRequest(context.Background(), "non_existent", nil, nil)
		assert.Equal(t, ErrTunnelNotFound, err)
	})
}

func TestTunnelManager_HandleTCPConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Create a tunnel
	tunnel, err := m.Create(context.Background(), CreateTunnelOptions{
		SessionID: "sess_123",
		RunnerID:  "run_456",
		Type:      TypeTCP,
		LocalPort: 5432,
	})
	require.NoError(t, err)

	t.Run("without handler", func(t *testing.T) {
		err := m.HandleTCPConnection(context.Background(), tunnel.ID, nil)
		assert.Equal(t, ErrRunnerNotConnected, err)
	})

	t.Run("non-existent tunnel", func(t *testing.T) {
		err := m.HandleTCPConnection(context.Background(), "non_existent", nil)
		assert.Equal(t, ErrTunnelNotFound, err)
	})
}

func TestDefaultURLGenerator(t *testing.T) {
	gen := &defaultURLGenerator{baseURL: "https://tunnels.example.com"}

	tunnel := &Tunnel{
		ID:   "tun_abc123",
		Type: TypeHTTP,
	}

	url := gen.GenerateURL(tunnel)
	assert.Equal(t, "https://tunnels.example.com/tunnels/tun_abc123", url)
}

func TestTunnelManager_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	store := newMockStore()

	m := NewTunnelManager(
		WithStore(store),
		WithLogger(logger),
	)

	// Test concurrent creation
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := m.Create(context.Background(), CreateTunnelOptions{
				SessionID: "sess_concurrent",
				RunnerID:  "run_456",
				Type:      TypeHTTP,
				LocalPort: 3000 + idx,
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, m.GetActiveCount())

	// Test concurrent closure
	tunnels, err := m.GetBySession(context.Background(), "sess_concurrent")
	require.NoError(t, err)

	for _, tunnel := range tunnels[:50] {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = m.Close(context.Background(), id)
		}(tunnel.ID)
	}
	wg.Wait()

	assert.Equal(t, 50, m.GetActiveCount())
}
