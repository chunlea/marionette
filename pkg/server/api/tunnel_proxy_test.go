package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
