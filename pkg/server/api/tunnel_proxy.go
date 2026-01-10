package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// ErrUnauthorized is returned when authentication fails.
var ErrUnauthorized = errors.New("unauthorized")

// TunnelProxyService provides tunnel proxy functionality.
type TunnelProxyService interface {
	// ValidateTunnel validates a tunnel exists and is not expired.
	// Returns the tunnel info if valid.
	ValidateTunnel(ctx context.Context, tunnelID string) (*TunnelInfo, error)

	// ValidateTunnelToken validates a tunnel token.
	// Returns true if the token is valid for the given tunnel.
	ValidateTunnelToken(ctx context.Context, tunnelID, token string) (bool, error)

	// SendRequest sends a serialized HTTP request through the tunnel.
	// Returns a channel for receiving response data.
	SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error)

	// CloseConnection closes a tunnel connection.
	CloseConnection(connectionID string)
}

// TunnelInfo holds basic tunnel information for proxy validation.
type TunnelInfo struct {
	ID        string
	Type      string
	RunnerID  string
	SessionID string
	ExpiresAt time.Time
}

// TunnelProxyHandler handles tunnel proxy requests.
type TunnelProxyHandler struct {
	logger     *zap.Logger
	service    TunnelProxyService
	httpProxy  *tunnel.HTTPProxy
	apiKeyAuth func(r *http.Request) (bool, error)
}

// TunnelProxyOption is a functional option for TunnelProxyHandler.
type TunnelProxyOption func(*TunnelProxyHandler)

// WithTPLogger sets the logger for the tunnel proxy handler.
func WithTPLogger(logger *zap.Logger) TunnelProxyOption {
	return func(h *TunnelProxyHandler) {
		h.logger = logger
	}
}

// WithTPService sets the service for the tunnel proxy handler.
func WithTPService(svc TunnelProxyService) TunnelProxyOption {
	return func(h *TunnelProxyHandler) {
		h.service = svc
	}
}

// WithTPAPIKeyAuth sets the API key authentication function.
func WithTPAPIKeyAuth(fn func(r *http.Request) (bool, error)) TunnelProxyOption {
	return func(h *TunnelProxyHandler) {
		h.apiKeyAuth = fn
	}
}

// NewTunnelProxyHandler creates a new TunnelProxyHandler.
func NewTunnelProxyHandler(opts ...TunnelProxyOption) *TunnelProxyHandler {
	h := &TunnelProxyHandler{
		logger:    zap.NewNop(),
		httpProxy: tunnel.NewHTTPProxy(tunnel.DefaultHTTPProxyConfig()),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP handles HTTP requests to the tunnel proxy.
// Route: ANY /tunnels/{tunnelID}/*
func (h *TunnelProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tunnelID := chi.URLParam(r, "tunnelID")
	if tunnelID == "" {
		http.Error(w, "tunnel ID required", http.StatusBadRequest)
		return
	}

	h.logger.Debug("tunnel proxy request",
		zap.String("tunnel_id", tunnelID),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
	)

	// Authenticate the request
	if err := h.authenticate(r, tunnelID); err != nil {
		h.logger.Warn("tunnel proxy authentication failed",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Validate tunnel exists and is not expired
	ctx := r.Context()
	info, err := h.service.ValidateTunnel(ctx, tunnelID)
	if err != nil {
		h.logger.Warn("tunnel validation failed",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		http.Error(w, "tunnel not found or expired", http.StatusNotFound)
		return
	}

	// Check if tunnel is expired
	if time.Now().After(info.ExpiresAt) {
		h.logger.Warn("tunnel expired",
			zap.String("tunnel_id", tunnelID),
			zap.Time("expires_at", info.ExpiresAt),
		)
		http.Error(w, "tunnel expired", http.StatusGone)
		return
	}

	// Only HTTP tunnels are supported for now
	if info.Type != "http" {
		h.logger.Warn("unsupported tunnel type for HTTP proxy",
			zap.String("tunnel_id", tunnelID),
			zap.String("type", info.Type),
		)
		http.Error(w, "tunnel type not supported for HTTP proxy", http.StatusBadRequest)
		return
	}

	// Generate connection ID for this request
	connectionID := id.New("conn")

	// Rewrite the request path to remove the tunnel prefix
	// /tunnels/{tunnelID}/foo/bar -> /foo/bar
	originalPath := r.URL.Path
	prefix := "/tunnels/" + tunnelID
	if strings.HasPrefix(originalPath, prefix) {
		newPath := strings.TrimPrefix(originalPath, prefix)
		if newPath == "" {
			newPath = "/"
		}
		r.URL.Path = newPath
		// Also update RequestURI for proper serialization
		r.RequestURI = newPath
		if r.URL.RawQuery != "" {
			r.RequestURI += "?" + r.URL.RawQuery
		}
	}

	h.logger.Debug("proxying request",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("original_path", originalPath),
		zap.String("proxied_path", r.URL.Path),
	)

	// Serialize the HTTP request
	serialized, err := h.httpProxy.SerializeRequest(r)
	if err != nil {
		h.logger.Error("failed to serialize request",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "failed to process request", http.StatusInternalServerError)
		return
	}

	// Send request through tunnel
	responseCh, err := h.service.SendRequest(ctx, tunnelID, connectionID, serialized)
	if err != nil {
		h.logger.Error("failed to send request through tunnel",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "tunnel unavailable", http.StatusBadGateway)
		return
	}
	defer h.service.CloseConnection(connectionID)

	// Wait for response with timeout
	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Collect response data
	var responseData []byte
	for {
		select {
		case data, ok := <-responseCh:
			if !ok {
				// Channel closed, we have all the data
				goto processResponse
			}
			responseData = append(responseData, data...)
		case <-ctx.Done():
			h.logger.Warn("tunnel request timeout",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
			)
			http.Error(w, "request timeout", http.StatusGatewayTimeout)
			return
		}
	}

processResponse:
	if len(responseData) == 0 {
		h.logger.Warn("empty response from tunnel",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
		)
		http.Error(w, "no response from tunnel", http.StatusBadGateway)
		return
	}

	// Deserialize the HTTP response
	resp, err := h.httpProxy.DeserializeResponse(responseData)
	if err != nil {
		h.logger.Error("failed to deserialize response",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "invalid response from tunnel", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.Warn("error copying response body",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
	}

	h.logger.Debug("tunnel proxy request completed",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.Int("status", resp.StatusCode),
	)
}

// authenticate validates the request has proper authentication.
// Supports both tunnel token and API key authentication.
func (h *TunnelProxyHandler) authenticate(r *http.Request, tunnelID string) error {
	// Try tunnel token first
	token := h.extractTunnelToken(r)
	if token != "" {
		valid, err := h.service.ValidateTunnelToken(r.Context(), tunnelID, token)
		if err != nil {
			return err
		}
		if valid {
			return nil
		}
	}

	// Try API key authentication
	if h.apiKeyAuth != nil {
		valid, err := h.apiKeyAuth(r)
		if err != nil {
			return err
		}
		if valid {
			return nil
		}
	}

	return ErrUnauthorized
}

// extractTunnelToken extracts the tunnel token from the request.
// Checks Authorization header and query parameter.
func (h *TunnelProxyHandler) extractTunnelToken(r *http.Request) string {
	// Check Authorization header: "Bearer ttok_xxx"
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		if strings.HasPrefix(token, "ttok_") {
			return token
		}
	}

	// Check query parameter: ?token=ttok_xxx
	token := r.URL.Query().Get("token")
	if strings.HasPrefix(token, "ttok_") {
		return token
	}

	return ""
}
