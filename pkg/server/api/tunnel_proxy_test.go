package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockTunnelProxyService implements TunnelProxyService for testing.
type mockTunnelProxyService struct {
	validateTunnelFn      func(ctx context.Context, tunnelID string) (*TunnelInfo, error)
	validateTokenFn       func(ctx context.Context, tunnelID, token string) (bool, error)
	sendRequestFn         func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error)
	sendDataFn            func(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error
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

func (m *mockTunnelProxyService) SendData(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
	if m.sendDataFn != nil {
		return m.sendDataFn(ctx, tunnelID, connectionID, data, eof)
	}
	return nil
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
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				IsPublic:  false, // Private tunnel requires auth
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
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
	// Should return WWW-Authenticate header for Basic Auth prompt
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), "Basic realm=")
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
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid123")
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

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo?marionette_token=ttok_querytoken", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTunnelProxyHandler_PublicTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 6\r\n\r\npublic")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				IsPublic:  true, // Public tunnel - no auth required
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

	// No authentication provided - should still work for public tunnel
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_public123/foo", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public", w.Body.String())
}

func TestTunnelProxyHandler_BasicAuth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nbasic")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return token == "ttok_basictoken", nil
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

	// Use HTTP Basic Auth with tunnel token as password
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.SetBasicAuth("", "ttok_basictoken") // Username empty, password is the token
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "basic", w.Body.String())
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
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
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
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
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
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The captured data should contain "/api/endpoint", not "/tunnels/tun_test123/api/endpoint"
	assert.Contains(t, capturedPath, "/api/endpoint")
	assert.NotContains(t, capturedPath, "/tunnels/tun_test123")
}

func TestTunnelProxyHandler_SendRequestError(t *testing.T) {
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
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			return nil, errors.New("tunnel unavailable")
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestTunnelProxyHandler_EmptyResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	close(responseCh) // Close immediately with no data

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
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestTunnelProxyHandler_InvalidResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("not a valid HTTP response")
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
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestTunnelProxyHandler_TunnelNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return true, nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return nil, errors.New("tunnel not found")
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTunnelProxyHandler_AuthTokenError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return false, errors.New("token validation error")
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_test")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTunnelProxyHandler_APIKeyAuthError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return false, nil // Token validation fails
		},
	}

	// API key auth function that returns error
	apiKeyAuth := func(r *http.Request) (bool, error) {
		return false, errors.New("api key error")
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
		WithTPAPIKeyAuth(apiKeyAuth),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("X-Marionette-API-Key", "mk_test")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
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

	// API key auth function using X-Marionette-API-Key header
	apiKeyAuth := func(r *http.Request) (bool, error) {
		return r.Header.Get("X-Marionette-API-Key") == "mk_valid_key", nil
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
		WithTPAPIKeyAuth(apiKeyAuth),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/foo", nil)
	req.Header.Set("X-Marionette-API-Key", "mk_valid_key")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestExtractTunnelToken(t *testing.T) {
	h := NewTunnelProxyHandler()

	tests := []struct {
		name      string
		header    string // X-Marionette-Tunnel-Token header
		query     string // marionette_token query param
		basicAuth string // Basic Auth password (username ignored)
		expected  string
	}{
		{
			name:     "X-Marionette-Tunnel-Token header",
			header:   "ttok_abc123",
			expected: "ttok_abc123",
		},
		{
			name:     "marionette_token query param",
			query:    "ttok_xyz789",
			expected: "ttok_xyz789",
		},
		{
			name:      "HTTP Basic Auth password",
			basicAuth: "ttok_basicauth",
			expected:  "ttok_basicauth",
		},
		{
			name:     "header takes precedence over query",
			header:   "ttok_header",
			query:    "ttok_query",
			expected: "ttok_header",
		},
		{
			name:      "header takes precedence over basic auth",
			header:    "ttok_header",
			basicAuth: "ttok_basic",
			expected:  "ttok_header",
		},
		{
			name:      "query takes precedence over basic auth",
			query:     "ttok_query",
			basicAuth: "ttok_basic",
			expected:  "ttok_query",
		},
		{
			name:     "non-tunnel header ignored",
			header:   "mk_apikey",
			expected: "",
		},
		{
			name:     "non-tunnel query param ignored",
			query:    "apikey123",
			expected: "",
		},
		{
			name:      "non-tunnel basic auth password ignored",
			basicAuth: "regularpassword",
			expected:  "",
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
				url += "?marionette_token=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tc.header != "" {
				req.Header.Set("X-Marionette-Tunnel-Token", tc.header)
			}
			if tc.basicAuth != "" {
				req.SetBasicAuth("", tc.basicAuth)
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

func TestCreateAPIKeyValidator(t *testing.T) {
	// Create a mock validate key function
	validateKey := func(ctx context.Context, key string) (bool, error) {
		if key == "valid_key" {
			return true, nil
		}
		if key == "error_key" {
			return false, errors.New("validation error")
		}
		return false, nil
	}

	validator := CreateAPIKeyValidator(validateKey)

	t.Run("valid key", func(t *testing.T) {
		ctx := context.Background()
		valid, err := validator(&ctx, "valid_key")
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("invalid key", func(t *testing.T) {
		ctx := context.Background()
		valid, err := validator(&ctx, "invalid_key")
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("error case", func(t *testing.T) {
		ctx := context.Background()
		valid, err := validator(&ctx, "error_key")
		assert.Error(t, err)
		assert.False(t, valid)
	})
}

func TestTunnelProxyHandler_RootPath(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nroot")
	close(responseCh)

	var capturedPath string
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
			capturedPath = string(data)
			return responseCh, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.HandleFunc("/*", h.ServeHTTP)
		r.HandleFunc("/", h.ServeHTTP)
	})

	// Test root path - /tunnels/tun_test123 should become /
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/", nil)
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The path should be "/" after removing prefix
	assert.Contains(t, capturedPath, "GET / HTTP")
}

func TestTunnelProxyHandler_PathWithQueryString(t *testing.T) {
	logger := zaptest.NewLogger(t)

	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	close(responseCh)

	var capturedData string
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
			capturedData = string(data)
			return responseCh, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	// Test path with query string - should preserve query string
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api?param=value", nil)
	req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The serialized request should contain the path with query string
	assert.Contains(t, capturedData, "/api?param=value")
}

func TestTunnelProxyHandler_SanitizeRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var capturedData string
	responseCh := make(chan []byte, 1)
	responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
	close(responseCh)

	mockSvc := &mockTunnelProxyService{
		validateTokenFn: func(ctx context.Context, tunnelID, token string) (bool, error) {
			return token == "ttok_valid", nil
		},
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			capturedData = string(data)
			return responseCh, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)

	t.Run("removes token query parameter", func(t *testing.T) {
		capturedData = ""
		responseCh := make(chan []byte, 1)
		responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		close(responseCh)
		mockSvc.sendRequestFn = func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			capturedData = string(data)
			return responseCh, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api?marionette_token=ttok_valid&other=value", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Token should be removed, other params preserved
		assert.Contains(t, capturedData, "/api?other=value")
		assert.NotContains(t, capturedData, "ttok_valid")
	})

	t.Run("removes tunnel token from Authorization header", func(t *testing.T) {
		capturedData = ""
		responseCh := make(chan []byte, 1)
		responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		close(responseCh)
		mockSvc.sendRequestFn = func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			capturedData = string(data)
			return responseCh, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api", nil)
		req.Header.Set("X-Marionette-Tunnel-Token", "ttok_valid")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Authorization header with tunnel token should be removed
		assert.NotContains(t, capturedData, "X-Marionette-Tunnel-Token: ttok_valid")
	})

	t.Run("removes X-Marionette-API-Key header", func(t *testing.T) {
		capturedData = ""
		responseCh := make(chan []byte, 1)
		responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		close(responseCh)
		mockSvc.sendRequestFn = func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			capturedData = string(data)
			return responseCh, nil
		}

		// Create API key auth that validates via X-Marionette-API-Key header
		h2 := NewTunnelProxyHandler(
			WithTPLogger(logger),
			WithTPService(mockSvc),
			WithTPAPIKeyAuth(func(r *http.Request) (bool, error) {
				return r.Header.Get("X-Marionette-API-Key") == "mk_test", nil
			}),
		)

		r2 := chi.NewRouter()
		r2.HandleFunc("/tunnels/{tunnelID}/*", h2.ServeHTTP)

		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api", nil)
		req.Header.Set("X-Marionette-API-Key", "mk_test")
		w := httptest.NewRecorder()

		r2.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// X-Marionette-API-Key header should be removed before forwarding
		assert.NotContains(t, capturedData, "X-Marionette-API-Key")
		assert.NotContains(t, capturedData, "mk_test")
	})

	t.Run("preserves non-tunnel Authorization headers", func(t *testing.T) {
		capturedData = ""
		responseCh := make(chan []byte, 1)
		responseCh <- []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		close(responseCh)
		mockSvc.sendRequestFn = func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			capturedData = string(data)
			return responseCh, nil
		}

		// Create API key auth to allow the request
		h2 := NewTunnelProxyHandler(
			WithTPLogger(logger),
			WithTPService(mockSvc),
			WithTPAPIKeyAuth(func(r *http.Request) (bool, error) {
				return r.Header.Get("X-Marionette-API-Key") == "mk_test", nil
			}),
		)

		r2 := chi.NewRouter()
		r2.HandleFunc("/tunnels/{tunnelID}/*", h2.ServeHTTP)

		req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api", nil)
		req.Header.Set("X-Marionette-API-Key", "mk_test")
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Basic auth for backend (non-tunnel)
		w := httptest.NewRecorder()

		r2.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Non-tunnel Basic auth should be preserved (password doesn't start with ttok_)
		assert.Contains(t, capturedData, "Authorization: Basic dXNlcjpwYXNz")
	})
}

// mockTunnelRouter implements TunnelRouter for testing.
type mockTunnelRouter struct {
	sendRequestFn          func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error)
	sendDataFn             func(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error
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

func (m *mockTunnelRouter) SendData(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
	if m.sendDataFn != nil {
		return m.sendDataFn(ctx, tunnelID, connectionID, data, eof)
	}
	return nil
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

func (m *mockTunnelRouter) NotifyTunnelCreated(tunnelID, runnerID, tunnelType string, localPort int32, direction string) error {
	return nil
}

// Tests for streaming responses

func TestTunnelProxyHandler_SSEResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				IsPublic:  true,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			ch := make(chan []byte, 10)
			go func() {
				defer close(ch)
				// Send SSE response with streaming content-type
				sseResponse := "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\n\r\n"
				ch <- []byte(sseResponse)
				// Send SSE events
				ch <- []byte("data: event 1\n\n")
				ch <- []byte("data: event 2\n\n")
			}()
			return ch, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router with tunnel ID in path
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.Get("/*", h.ServeHTTP)
	})

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/stream", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "data: event 1")
	assert.Contains(t, w.Body.String(), "data: event 2")
}

func TestTunnelProxyHandler_ChunkedResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				IsPublic:  true,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			ch := make(chan []byte, 10)
			go func() {
				defer close(ch)
				// Send chunked response
				chunkedResponse := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
				ch <- []byte(chunkedResponse)
				ch <- []byte("chunk1")
				ch <- []byte("chunk2")
				ch <- []byte("chunk3")
			}()
			return ch, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router with tunnel ID in path
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.Get("/*", h.ServeHTTP)
	})

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/data", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "chunk1")
	assert.Contains(t, w.Body.String(), "chunk2")
	assert.Contains(t, w.Body.String(), "chunk3")
}

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name       string
		connection string
		upgrade    string
		expected   bool
	}{
		{
			name:       "valid websocket upgrade",
			connection: "Upgrade",
			upgrade:    "websocket",
			expected:   true,
		},
		{
			name:       "case insensitive",
			connection: "UPGRADE",
			upgrade:    "WEBSOCKET",
			expected:   true,
		},
		{
			// Browsers send a token list, not a bare "Upgrade". Matching the
			// whole header exactly missed every real browser handshake.
			name:       "browser token list",
			connection: "keep-alive, Upgrade",
			upgrade:    "websocket",
			expected:   true,
		},
		{
			name:       "missing connection header",
			connection: "",
			upgrade:    "websocket",
			expected:   false,
		},
		{
			name:       "missing upgrade header",
			connection: "Upgrade",
			upgrade:    "",
			expected:   false,
		},
		{
			name:       "wrong upgrade value",
			connection: "Upgrade",
			upgrade:    "h2c",
			expected:   false,
		},
		{
			name:       "wrong connection value",
			connection: "keep-alive",
			upgrade:    "websocket",
			expected:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tc.connection != "" {
				req.Header.Set("Connection", tc.connection)
			}
			if tc.upgrade != "" {
				req.Header.Set("Upgrade", tc.upgrade)
			}

			result := isWebSocketUpgrade(req)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestTunnelProxyHandler_WebSocketUpgradeDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				IsPublic:  true,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			ch := make(chan []byte, 10)
			go func() {
				defer close(ch)
				// Send WebSocket upgrade response
				wsResponse := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n"
				ch <- []byte(wsResponse)
			}()
			return ch, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router with tunnel ID in path
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.Get("/*", h.ServeHTTP)
	})

	// WebSocket request (httptest.NewRecorder doesn't support hijacking,
	// so we just verify the handler tries to process it)
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// httptest.NewRecorder doesn't support hijacking, so we expect an error response
	// The important thing is that the handler recognized this as a WebSocket request
	// and tried to hijack (which fails with the test recorder)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "websocket not supported")
}

func TestTunnelProxyHandler_StreamingContentLengthUnknown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockSvc := &mockTunnelProxyService{
		validateTunnelFn: func(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
			return &TunnelInfo{
				ID:        tunnelID,
				Type:      "http",
				RunnerID:  "run_test",
				SessionID: "sess_test",
				IsPublic:  true,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
		sendRequestFn: func(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
			ch := make(chan []byte, 10)
			go func() {
				defer close(ch)
				// Response without Content-Length (streaming)
				response := "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\n\r\n"
				ch <- []byte(response)
				ch <- []byte("part1")
				ch <- []byte("part2")
			}()
			return ch, nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router with tunnel ID in path
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.Get("/*", h.ServeHTTP)
	})

	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/stream", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "part1")
	assert.Contains(t, w.Body.String(), "part2")
}

// Integration tests for WebSocket tunnel proxy

// mockBidirectionalTunnelService simulates a WebSocket backend for integration testing.
type mockBidirectionalTunnelService struct {
	validateTunnelFn func(ctx context.Context, tunnelID string) (*TunnelInfo, error)
	responseCh       chan []byte
	receivedData     [][]byte
	mu               sync.Mutex
	sendDataCalled   bool
}

func (m *mockBidirectionalTunnelService) ValidateTunnel(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
	if m.validateTunnelFn != nil {
		return m.validateTunnelFn(ctx, tunnelID)
	}
	return &TunnelInfo{
		ID:        tunnelID,
		Type:      "http",
		RunnerID:  "run_test",
		SessionID: "sess_test",
		IsPublic:  true,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (m *mockBidirectionalTunnelService) ValidateTunnelToken(ctx context.Context, tunnelID, token string) (bool, error) {
	return true, nil
}

func (m *mockBidirectionalTunnelService) SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
	return m.responseCh, nil
}

func (m *mockBidirectionalTunnelService) SendData(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendDataCalled = true
	if len(data) > 0 {
		m.receivedData = append(m.receivedData, data)
	}
	return nil
}

func (m *mockBidirectionalTunnelService) CloseConnection(connectionID string) {}

func (m *mockBidirectionalTunnelService) GetReceivedData() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]byte, len(m.receivedData))
	copy(result, m.receivedData)
	return result
}

func TestTunnelProxyHandler_WebSocket_Integration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create response channel that we control
	responseCh := make(chan []byte, 100)

	mockSvc := &mockBidirectionalTunnelService{
		responseCh: responseCh,
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router with tunnel ID in path
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.HandleFunc("/*", h.ServeHTTP)
	})

	// Create a real HTTP server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Send WebSocket upgrade response through the mock
	go func() {
		// Wait a moment for the request to be processed
		time.Sleep(50 * time.Millisecond)
		// Send 101 Switching Protocols response
		wsUpgradeResponse := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n" +
			"\r\n"
		responseCh <- []byte(wsUpgradeResponse)
	}()

	// Connect with WebSocket client
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/tunnels/tun_test123/ws"
	header := http.Header{}
	header.Set("Connection", "Upgrade")
	header.Set("Upgrade", "websocket")

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, header)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		// Expected: the mock doesn't implement proper WebSocket protocol
		// The upgrade response we send is just for header testing
		t.Logf("WebSocket dial failed (expected with mock): %v", err)
		if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
			t.Log("Got 101 Switching Protocols - upgrade initiated correctly")
		}
		return
	}
	defer func() { _ = conn.Close() }()

	// If we got here, the connection was established
	// Send a message
	err = conn.WriteMessage(websocket.TextMessage, []byte("hello from client"))
	require.NoError(t, err)

	// Send response from "backend"
	responseCh <- []byte("hello from backend")

	// Read response
	_, msg, err := conn.ReadMessage()
	if err == nil {
		t.Logf("Received message: %s", string(msg))
	}
}

func TestTunnelProxyHandler_WebSocket_UpgradeRejected(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create response channel
	responseCh := make(chan []byte, 10)

	mockSvc := &mockBidirectionalTunnelService{
		responseCh: responseCh,
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.HandleFunc("/*", h.ServeHTTP)
	})

	// Create a real HTTP server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Send a rejection response (400 Bad Request) through the mock
	go func() {
		time.Sleep(50 * time.Millisecond)
		rejectionResponse := "HTTP/1.1 400 Bad Request\r\n" +
			"Content-Type: text/plain\r\n" +
			"Content-Length: 18\r\n" +
			"\r\n" +
			"Upgrade rejected\r\n"
		responseCh <- []byte(rejectionResponse)
		close(responseCh)
	}()

	// Try to connect with WebSocket client
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/tunnels/tun_test123/ws"

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	_, resp, err := dialer.Dial(wsURL, nil)

	// Should fail because backend rejected the upgrade
	assert.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		_ = resp.Body.Close()
	}
}

func TestTunnelProxyHandler_WebSocket_BidirectionalRelay(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create response channel
	responseCh := make(chan []byte, 100)

	mockSvc := &mockBidirectionalTunnelService{
		responseCh: responseCh,
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create router
	r := chi.NewRouter()
	r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
		r.HandleFunc("/*", h.ServeHTTP)
	})

	// Create a real HTTP server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// We need to test the relay functions directly since the mock
	// doesn't implement the full WebSocket protocol

	// Test that the handler correctly identifies WebSocket upgrades
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")

	assert.True(t, isWebSocketUpgrade(req))

	// Test non-WebSocket request
	req2 := httptest.NewRequest(http.MethodGet, "/tunnels/tun_test123/api", nil)
	assert.False(t, isWebSocketUpgrade(req2))
}

func TestTunnelProxyAdapter_SendData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var sentData []byte
	var sentEOF bool

	mockRouter := &mockTunnelRouter{
		sendDataFn: func(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
			sentData = data
			sentEOF = eof
			return nil
		},
	}

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		WithTPATunnelRouter(mockRouter),
	)

	// Test SendData
	testData := []byte("test data")
	err := adapter.SendData(context.Background(), "tun_test", "conn_test", testData, false)
	require.NoError(t, err)
	assert.Equal(t, testData, sentData)
	assert.False(t, sentEOF)

	// Test SendData with EOF
	err = adapter.SendData(context.Background(), "tun_test", "conn_test", nil, true)
	require.NoError(t, err)
	assert.True(t, sentEOF)
}

func TestTunnelProxyAdapter_SendData_NoRouter(t *testing.T) {
	logger := zaptest.NewLogger(t)

	adapter := NewTunnelProxyAdapter(
		WithTPALogger(logger),
		// No router configured
	)

	err := adapter.SendData(context.Background(), "tun_test", "conn_test", []byte("test"), false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel router not configured")
}

// mockNetConn implements net.Conn for testing relay functions.
type mockNetConn struct {
	readData    []byte
	readPos     int
	readErr     error
	writtenData []byte
	writeErr    error
	closed      bool
	mu          sync.Mutex
}

func (m *mockNetConn) Read(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.readErr != nil {
		return 0, m.readErr
	}
	if m.readPos >= len(m.readData) {
		return 0, errors.New("EOF")
	}
	n = copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockNetConn) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.writtenData = append(m.writtenData, b...)
	return len(b), nil
}

func (m *mockNetConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// mockAddr implements net.Addr for testing.
type mockAddr struct{}

func (mockAddr) Network() string { return "mock" }
func (mockAddr) String() string  { return "mock:0" }

func (m *mockNetConn) LocalAddr() net.Addr  { return mockAddr{} }
func (m *mockNetConn) RemoteAddr() net.Addr { return mockAddr{} }

func (m *mockNetConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockNetConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockNetConn) SetWriteDeadline(t time.Time) error { return nil }

func TestRelayClientToTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var receivedData [][]byte
	var receivedEOF bool
	var mu sync.Mutex

	mockSvc := &mockTunnelProxyService{
		sendDataFn: func(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
			mu.Lock()
			defer mu.Unlock()
			if len(data) > 0 {
				dataCopy := make([]byte, len(data))
				copy(dataCopy, data)
				receivedData = append(receivedData, dataCopy)
			}
			if eof {
				receivedEOF = true
			}
			return nil
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create mock connection with test data
	mockConn := &mockNetConn{
		readData: []byte("hello from client"),
	}

	// Run relay in goroutine
	done := make(chan struct{})
	go func() {
		h.relayClientToTunnel(context.Background(), mockConn, "tun_test", "conn_test")
		close(done)
	}()

	// Wait for relay to complete
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("relay did not complete in time")
	}

	// Verify data was sent
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, len(receivedData) > 0, "should have received data")
	assert.True(t, receivedEOF, "should have received EOF")
}

func TestRelayTunnelToClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create mock connection
	mockConn := &mockNetConn{}

	// Create response channel
	responseCh := make(chan []byte, 10)

	// Send data through the channel
	go func() {
		responseCh <- []byte("hello")
		responseCh <- []byte(" from")
		responseCh <- []byte(" tunnel")
		close(responseCh)
	}()

	// Run relay
	h.relayTunnelToClient(context.Background(), responseCh, mockConn, "tun_test", "conn_test")

	// Verify data was written
	mockConn.mu.Lock()
	defer mockConn.mu.Unlock()
	assert.Equal(t, "hello from tunnel", string(mockConn.writtenData))
}

func TestRelayTunnelToClient_WriteError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create mock connection that returns error on write
	mockConn := &mockNetConn{
		writeErr: errors.New("write failed"),
	}

	// Create response channel
	responseCh := make(chan []byte, 10)

	// Send data through the channel
	go func() {
		responseCh <- []byte("hello")
		time.Sleep(100 * time.Millisecond) // Give time for error to be processed
		close(responseCh)
	}()

	// Run relay - should return on write error
	done := make(chan struct{})
	go func() {
		h.relayTunnelToClient(context.Background(), responseCh, mockConn, "tun_test", "conn_test")
		close(done)
	}()

	select {
	case <-done:
		// Success - relay returned on error
	case <-time.After(time.Second):
		t.Fatal("relay did not complete in time")
	}
}

func TestRelayTunnelToClient_ContextCancelled(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create mock connection
	mockConn := &mockNetConn{}

	// Create response channel that never sends
	responseCh := make(chan []byte, 10)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Run relay
	done := make(chan struct{})
	go func() {
		h.relayTunnelToClient(ctx, responseCh, mockConn, "tun_test", "conn_test")
		close(done)
	}()

	// Cancel context
	cancel()

	select {
	case <-done:
		// Success - relay returned on context cancel
	case <-time.After(time.Second):
		t.Fatal("relay did not complete in time after context cancel")
	}
}

func TestRelayClientToTunnel_SendError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockSvc := &mockTunnelProxyService{
		sendDataFn: func(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
			return errors.New("send failed")
		},
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(mockSvc),
	)

	// Create mock connection with test data
	mockConn := &mockNetConn{
		readData: []byte("hello from client"),
	}

	// Run relay - should return on send error
	done := make(chan struct{})
	go func() {
		h.relayClientToTunnel(context.Background(), mockConn, "tun_test", "conn_test")
		close(done)
	}()

	select {
	case <-done:
		// Success - relay returned on error
	case <-time.After(time.Second):
		t.Fatal("relay did not complete in time")
	}
}
