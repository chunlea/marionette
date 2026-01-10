package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockTunnelProxyService implements TunnelProxyService for testing.
type mockTunnelProxyService struct {
	validateTunnelFn      func(ctx context.Context, tunnelID string) (*TunnelInfo, error)
	validateTokenFn       func(ctx context.Context, tunnelID, token string) (bool, error)
	sendRequestFn         func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error)
	closeConnectionCalled bool
}

func (m *mockTunnelProxyService) ValidateTunnel(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
	if m.validateTunnelFn != nil {
		return m.validateTunnelFn(ctx, tunnelID)
	}
	return nil, nil
}

func (m *mockTunnelProxyService) ValidateTunnelToken(ctx context.Context, tunnelID, token string) (bool, error) {
	if m.validateTokenFn != nil {
		return m.validateTokenFn(ctx, tunnelID, token)
	}
	return false, nil
}

func (m *mockTunnelProxyService) SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
	if m.sendRequestFn != nil {
		return m.sendRequestFn(ctx, tunnelID, connectionID, data)
	}
	return nil, nil
}

func (m *mockTunnelProxyService) CloseConnection(connectionID string) {
	m.closeConnectionCalled = true
}

func TestNewTunnelProxyHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
	)

	require.NotNil(t, h)
	assert.NotNil(t, h.logger)
	assert.NotNil(t, h.httpProxy)
}

func TestTunnelProxyHandler_NoTunnelID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockSvc := &mockTunnelProxyService{}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create test request without tunnel ID in context
	req := httptest.NewRequest(http.MethodGet, "/tunnels/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTunnelProxyHandler_Unauthorized_NoToken(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return false, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create chi context with tunnel ID
	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTunnelProxyHandler_ValidTunnelToken(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	// Send HTTP response format
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return token == "ttok_valid123", nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			return responseCh, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("Authorization", "Bearer ttok_valid123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "hello", w.Body.String())
}

func TestTunnelProxyHandler_TokenInQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return token == "ttok_querytoken", nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			return responseCh, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo?token=ttok_querytoken", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTunnelProxyHandler_ExpiredTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return true, nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(-time.Hour), // Expired
			}, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("Authorization", "Bearer ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusGone, w.Code)
}

func TestTunnelProxyHandler_UnsupportedTunnelType(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return true, nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "tcp", // Not http
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("Authorization", "Bearer ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTunnelProxyHandler_PathRewriting(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var capturedPath string
	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return true, nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			// Parse the serialized request to get the path
			// The path should be /api/endpoint, not /tunnels/tun_test123/api/endpoint
			capturedPath = string(data) // Simple check - real parsing would be more complex
			return responseCh, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api/endpoint", nil)
	req.Header.Set("Authorization", "Bearer ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The captured data should contain "/api/endpoint", not "/tunnels/tun_test123/api/endpoint"
	assert.Contains(t, capturedPath, "/api/endpoint")
	assert.NotContains(t, capturedPath, "/tunnels/tun_test123")
}

func TestTunnelProxyHandler_APIKeyAuth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return false, nil // Token validation fails
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			return responseCh, nil
		},
	}

	// API key auth function
	apiKeyAuth := func(r *http.Request) (bool, error) {
		return r.Header.Get("X-API-Key") == "mk_valid_key", nil
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
		WithTPAPIKeyAuth(apiKeyAuth),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("X-API-Key", "mk_valid_key")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestExtractTunnelToken(t *testing.T) {
	h := NewTunnelProxyHandler()

	tests := []struct {
		name     string
		header   string
		query    string
		expected string
	}{
		{
			name:     "bearer token",
			header:   "Bearer ttok_abc123",
			expected: "ttok_abc123",
		},
		{
			name:     "query param",
			query:    "ttok_xyz789",
			expected: "ttok_xyz789",
		},
		{
			name:     "bearer token takes precedence",
			header:   "Bearer ttok_header",
			query:    "ttok_query",
			expected: "ttok_header",
		},
		{
			name:     "non-tunnel bearer token ignored",
			header:   "Bearer mk_apikey",
			expected: "",
		},
		{
			name:     "non-tunnel query param ignored",
			query:    "apikey123",
			expected: "",
		},
		{
			name:     "empty",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/test"
			if tc.query != "" {
				url += "?token=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			token := h.extractTunnelToken(req)
			assert.Equal(t, tc.expected, token)
		})
	}
}

// Tests for TunnelProxyAdapter

func TestNewTunnelProxyAdapter(t *testing.T) {
	logger := zaptest.NewLogger(t)

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
	)

	require.NotNil(t, adapter)
	assert.NotNil(t, adapter.logger)
}

func TestTunnelProxyAdapter_WithOptions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(tunnel.WithLogger(logger))
	tr := &mockTunnelRouter{}

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelManager(tm),
		WithTPATunnelRouter(tr),
	)

	require.NotNil(t, adapter)
	assert.NotNil(t, adapter.tunnelManager)
	assert.NotNil(t, adapter.tunnelRouter)
}

func TestTunnelProxyAdapter_ValidateTunnel_NoManager(t *testing.T) {
	adapter := NewTunnelProxyAdapter()

	_, err := adapter.ValidateTunnel(context.Background(), "tun_test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel manager not configured")
}

func TestTunnelProxyAdapter_ValidateTunnel_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(
		tunnel.WithLogger(logger),
		tunnel.WithBaseURL("http://localhost:8080"),
	)

	// Create a tunnel using the manager
	ctx := context.Background()
	tun, err := tm.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_test",
		RunnerID:  "run_test",
		Type:      "http",
		LocalPort: 8000,
	})
	require.NoError(t, err)

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelManager(tm),
	)

	info, err := adapter.ValidateTunnel(ctx, tun.ID)
	require.NoError(t, err)
	assert.Equal(t, tun.ID, info.ID)
	assert.Equal(t, "http", info.Type)
	assert.Equal(t, "run_test", info.RunnerID)
	assert.Equal(t, "sess_test", info.SessionID)
}

func TestTunnelProxyAdapter_ValidateTunnel_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(tunnel.WithLogger(logger))

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelManager(tm),
	)

	_, err := adapter.ValidateTunnel(context.Background(), "tun_nonexistent")
	assert.Error(t, err)
}

func TestTunnelProxyAdapter_ValidateTunnel_Closed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(
		tunnel.WithLogger(logger),
		tunnel.WithBaseURL("http://localhost:8080"),
	)

	ctx := context.Background()
	tun, err := tm.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_test",
		RunnerID:  "run_test",
		Type:      "http",
		LocalPort: 8000,
	})
	require.NoError(t, err)

	// Close the tunnel
	err = tm.Close(ctx, tun.ID)
	require.NoError(t, err)

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelManager(tm),
	)

	// Closed tunnel is removed from cache, so it returns "not found"
	_, err = adapter.ValidateTunnel(ctx, tun.ID)
	assert.Error(t, err)
}

func TestTunnelProxyAdapter_ValidateTunnelToken_NoManager(t *testing.T) {
	adapter := NewTunnelProxyAdapter()

	valid, err := adapter.ValidateTunnelToken(context.Background(), "tun_test", "ttok_test")
	assert.Error(t, err)
	assert.False(t, valid)
}

func TestTunnelProxyAdapter_ValidateTunnelToken_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(
		tunnel.WithLogger(logger),
		tunnel.WithBaseURL("http://localhost:8080"),
	)

	ctx := context.Background()
	tun, err := tm.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_test",
		RunnerID:  "run_test",
		Type:      "http",
		LocalPort: 8000,
	})
	require.NoError(t, err)

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelManager(tm),
	)

	// Use the actual token from the created tunnel
	valid, err := adapter.ValidateTunnelToken(ctx, tun.ID, tun.Token)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestTunnelProxyAdapter_ValidateTunnelToken_Invalid(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(
		tunnel.WithLogger(logger),
		tunnel.WithBaseURL("http://localhost:8080"),
	)

	ctx := context.Background()
	tun, err := tm.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_test",
		RunnerID:  "run_test",
		Type:      "http",
		LocalPort: 8000,
	})
	require.NoError(t, err)

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelManager(tm),
	)

	// Use wrong token
	valid, err := adapter.ValidateTunnelToken(ctx, tun.ID, "ttok_wrong")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestTunnelProxyAdapter_SendRequest_NoRouter(t *testing.T) {
	adapter := NewTunnelProxyAdapter()

	_, err := adapter.SendRequest(context.Background(), "tun_test", "conn_test", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel router not configured")
}

func TestTunnelProxyAdapter_SendRequest_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create mock router that returns test data
	mockRouter := &mockTunnelRouter{
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error) {
			ch := make(chan *pb.TunnelData, 2)
			ch <- &pb.TunnelData{Data: []byte("response part 1")}
			ch <- &pb.TunnelData{Data: []byte("response part 2"), Eof: true}
			close(ch)
			return ch, nil
		},
	}

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelRouter(mockRouter),
	)

	ctx := context.Background()
	responseCh, err := adapter.SendRequest(ctx, "tun_test", "conn_test", []byte("request"))
	require.NoError(t, err)

	// Collect all response data
	var allData []byte
	for data := range responseCh {
		allData = append(allData, data...)
	}

	assert.Equal(t, "response part 1response part 2", string(allData))
}

func TestTunnelProxyAdapter_SendRequest_RouterError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockRouter := &mockTunnelRouter{
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error) {
			return nil, errors.New("router error")
		},
	}

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelRouter(mockRouter),
	)

	_, err := adapter.SendRequest(context.Background(), "tun_test", "conn_test", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "router error")
}

func TestTunnelProxyAdapter_SendRequest_NilData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Router returns nil data (should be skipped)
	mockRouter := &mockTunnelRouter{
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error) {
			ch := make(chan *pb.TunnelData, 3)
			ch <- nil // Should be skipped
			ch <- &pb.TunnelData{Data: []byte("valid")}
			ch <- &pb.TunnelData{Data: nil} // Empty data, should be skipped
			close(ch)
			return ch, nil
		},
	}

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelRouter(mockRouter),
	)

	responseCh, err := adapter.SendRequest(context.Background(), "tun_test", "conn_test", []byte("request"))
	require.NoError(t, err)

	var allData []byte
	for data := range responseCh {
		allData = append(allData, data...)
	}

	assert.Equal(t, "valid", string(allData))
}

func TestTunnelProxyAdapter_CloseConnection_NoRouter(t *testing.T) {
	adapter := NewTunnelProxyAdapter()

	// Should not panic with nil router
	adapter.CloseConnection("conn_test")
}

func TestTunnelProxyAdapter_CloseConnection_Success(t *testing.T) {
	mockRouter := &mockTunnelRouter{}

	adapter := NewTunnelProxyAdapter(
		WithTPATunnelRouter(mockRouter),
	)

	adapter.CloseConnection("conn_test")
	assert.True(t, mockRouter.closeConnectionCalled)
	assert.Equal(t, "conn_test", mockRouter.lastClosedConnID)
}

func TestDefaultTunnelCleanupConfig(t *testing.T) {
	cfg := DefaultTunnelCleanupConfig()

	assert.Equal(t, 5*time.Minute, cfg.Interval)
	assert.Equal(t, 10*time.Minute, cfg.MaxAge)
}

// mockTunnelRouter implements TunnelRouter for testing.
type mockTunnelRouter struct {
	sendRequestFn          func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error)
	closeConnectionCalled  bool
	lastClosedConnID       string
	registerTunnelCalled   bool
	unregisterTunnelCalled bool
}

func (m *mockTunnelRouter) SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error) {
	if m.sendRequestFn != nil {
		return m.sendRequestFn(ctx, tunnelID, connectionID, data)
	}
	return nil, nil
}

func (m *mockTunnelRouter) CloseConnection(connectionID string) {
	m.closeConnectionCalled = true
	m.lastClosedConnID = connectionID
}

func (m *mockTunnelRouter) RegisterTunnel(tunnelID, runnerID string) {
	m.registerTunnelCalled = true
}

func (m *mockTunnelRouter) UnregisterTunnel(tunnelID string) {
	m.unregisterTunnelCalled = true
}
