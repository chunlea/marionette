package api

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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

	// SendData sends data to the tunnel (for bidirectional streaming).
	SendData(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error

	// CloseConnection closes a tunnel connection.
	CloseConnection(connectionID string)
}

// TunnelInfo holds basic tunnel information for proxy validation.
type TunnelInfo struct {
	ID        string
	Type      string
	RunnerID  string
	SessionID string
	IsPublic  bool
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
// Supports: regular HTTP, WebSocket upgrades, SSE, and chunked responses.
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
		zap.Bool("is_websocket", isWebSocketUpgrade(r)),
	)

	// Validate tunnel exists and is not expired first
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

	// Authenticate the request (skip for public tunnels)
	if !info.IsPublic {
		if err := h.authenticate(r, tunnelID); err != nil {
			h.logger.Warn("tunnel proxy authentication failed",
				zap.String("tunnel_id", tunnelID),
				zap.Error(err),
			)
			// Return 401 with WWW-Authenticate header for Basic Auth prompt
			w.Header().Set("WWW-Authenticate", `Basic realm="Marionette Tunnel"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
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

	// Sanitize request: remove tunnel authentication info before forwarding
	h.sanitizeRequest(r)

	h.logger.Debug("proxying request",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("original_path", originalPath),
		zap.String("proxied_path", r.URL.Path),
	)

	// Handle WebSocket upgrade requests differently
	if isWebSocketUpgrade(r) {
		h.handleWebSocket(w, r, tunnelID, connectionID)
		return
	}

	// Handle regular HTTP with streaming support
	h.handleHTTPStream(w, r, tunnelID, connectionID)
}

// handleHTTPStream handles regular HTTP requests with streaming response support.
// This supports SSE (Server-Sent Events), chunked transfer encoding, and regular responses.
func (h *TunnelProxyHandler) handleHTTPStream(w http.ResponseWriter, r *http.Request, tunnelID, connectionID string) {
	ctx := r.Context()

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

	// Wait for response with timeout (for initial headers)
	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Collect initial data until we have complete HTTP headers
	var responseData []byte
	headersDone := false
	for !headersDone {
		select {
		case data, ok := <-responseCh:
			if !ok {
				// Channel closed before getting headers
				if len(responseData) == 0 {
					h.logger.Warn("empty response from tunnel",
						zap.String("tunnel_id", tunnelID),
						zap.String("connection_id", connectionID),
					)
					http.Error(w, "no response from tunnel", http.StatusBadGateway)
					return
				}
				headersDone = true
				break
			}
			responseData = append(responseData, data...)
			// Check if we have complete headers (look for \r\n\r\n)
			if bytes.Contains(responseData, []byte("\r\n\r\n")) {
				headersDone = true
			}
		case <-ctx.Done():
			h.logger.Warn("tunnel request timeout",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
			)
			http.Error(w, "request timeout", http.StatusGatewayTimeout)
			return
		}
	}

	// Find the header/body boundary
	headerEnd := bytes.Index(responseData, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		h.logger.Error("malformed response: no header boundary",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
		)
		http.Error(w, "invalid response from tunnel", http.StatusBadGateway)
		return
	}

	// Parse just the headers
	headerBytes := responseData[:headerEnd+4] // Include \r\n\r\n
	bodyStart := responseData[headerEnd+4:]

	// Parse the HTTP response headers
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(headerBytes)), nil)
	if err != nil {
		h.logger.Error("failed to parse response headers",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "invalid response from tunnel", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Check if this is a streaming response
	contentType := resp.Header.Get("Content-Type")
	transferEncoding := resp.Header.Get("Transfer-Encoding")
	isStreaming := strings.HasPrefix(contentType, "text/event-stream") ||
		strings.EqualFold(transferEncoding, "chunked") ||
		resp.ContentLength < 0

	h.logger.Debug("response type detected",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("content_type", contentType),
		zap.String("transfer_encoding", transferEncoding),
		zap.Bool("is_streaming", isStreaming),
		zap.Int64("content_length", resp.ContentLength),
	)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Get the flusher for streaming
	flusher, _ := w.(http.Flusher)

	// Write any body data we already have
	if len(bodyStart) > 0 {
		if _, err := w.Write(bodyStart); err != nil {
			h.logger.Warn("error writing initial body",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
				zap.Error(err),
			)
			return
		}
		if flusher != nil && isStreaming {
			flusher.Flush()
		}
	}

	// Stream remaining data
	for {
		select {
		case data, ok := <-responseCh:
			if !ok {
				// Channel closed, we're done
				h.logger.Debug("tunnel proxy request completed",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
					zap.Int("status", resp.StatusCode),
				)
				return
			}
			if len(data) > 0 {
				if _, err := w.Write(data); err != nil {
					h.logger.Warn("error writing response body",
						zap.String("tunnel_id", tunnelID),
						zap.String("connection_id", connectionID),
						zap.Error(err),
					)
					return
				}
				// Flush immediately for streaming responses
				if flusher != nil && isStreaming {
					flusher.Flush()
				}
			}
		case <-ctx.Done():
			h.logger.Warn("response stream interrupted",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
			)
			return
		}
	}
}

// handleWebSocket handles WebSocket upgrade requests by hijacking the connection
// and establishing a bidirectional relay through the tunnel.
func (h *TunnelProxyHandler) handleWebSocket(w http.ResponseWriter, r *http.Request, tunnelID, connectionID string) {
	ctx := r.Context()

	h.logger.Debug("handling websocket upgrade",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
	)

	// Serialize the HTTP request (including WebSocket upgrade headers)
	serialized, err := h.httpProxy.SerializeRequest(r)
	if err != nil {
		h.logger.Error("failed to serialize websocket request",
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
		h.logger.Error("failed to send websocket request through tunnel",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "tunnel unavailable", http.StatusBadGateway)
		return
	}
	defer h.service.CloseConnection(connectionID)

	// Wait for the initial response (WebSocket upgrade response)
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Collect response until we have the complete HTTP response headers
	var responseData []byte
	headersDone := false
	for !headersDone {
		select {
		case data, ok := <-responseCh:
			if !ok {
				h.logger.Warn("tunnel closed before websocket upgrade",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
				)
				http.Error(w, "tunnel closed unexpectedly", http.StatusBadGateway)
				return
			}
			responseData = append(responseData, data...)
			// Check if we have complete headers (look for \r\n\r\n)
			if bytes.Contains(responseData, []byte("\r\n\r\n")) {
				headersDone = true
			}
		case <-ctx.Done():
			h.logger.Warn("websocket upgrade timeout",
				zap.String("tunnel_id", tunnelID),
				zap.String("connection_id", connectionID),
			)
			http.Error(w, "upgrade timeout", http.StatusGatewayTimeout)
			return
		}
	}

	// Parse the HTTP response
	resp, err := h.httpProxy.DeserializeResponse(responseData)
	if err != nil {
		h.logger.Error("failed to parse websocket upgrade response",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "invalid upgrade response", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Check if upgrade was accepted
	if resp.StatusCode != http.StatusSwitchingProtocols {
		h.logger.Warn("websocket upgrade rejected",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Int("status", resp.StatusCode),
		)
		// Forward the rejection response
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		h.logger.Error("response writer does not support hijacking",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
		)
		http.Error(w, "websocket not supported", http.StatusInternalServerError)
		return
	}

	clientConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		h.logger.Error("failed to hijack connection",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		http.Error(w, "failed to hijack connection", http.StatusInternalServerError)
		return
	}
	defer func() { _ = clientConn.Close() }()

	// Write the upgrade response to the client
	if _, err := bufrw.Write(responseData); err != nil {
		h.logger.Error("failed to write upgrade response",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		return
	}
	if err := bufrw.Flush(); err != nil {
		h.logger.Error("failed to flush upgrade response",
			zap.String("tunnel_id", tunnelID),
			zap.String("connection_id", connectionID),
			zap.Error(err),
		)
		return
	}

	h.logger.Info("websocket connection established",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
	)

	// Establish bidirectional relay
	ctx = context.Background() // Use a fresh context for the relay
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Tunnel
	go func() {
		defer wg.Done()
		h.relayClientToTunnel(ctx, clientConn, tunnelID, connectionID)
	}()

	// Tunnel -> Client
	go func() {
		defer wg.Done()
		h.relayTunnelToClient(ctx, responseCh, clientConn, tunnelID, connectionID)
	}()

	wg.Wait()
	h.logger.Info("websocket connection closed",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
	)
}

// relayClientToTunnel relays data from the client to the tunnel.
func (h *TunnelProxyHandler) relayClientToTunnel(ctx context.Context, conn net.Conn, tunnelID, connectionID string) {
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				h.logger.Debug("client read error",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
					zap.Error(err),
				)
			}
			// Send EOF to tunnel
			_ = h.service.SendData(ctx, tunnelID, connectionID, nil, true)
			return
		}
		if n > 0 {
			if err := h.service.SendData(ctx, tunnelID, connectionID, buf[:n], false); err != nil {
				h.logger.Debug("failed to send data to tunnel",
					zap.String("tunnel_id", tunnelID),
					zap.String("connection_id", connectionID),
					zap.Error(err),
				)
				return
			}
		}
	}
}

// relayTunnelToClient relays data from the tunnel to the client.
func (h *TunnelProxyHandler) relayTunnelToClient(ctx context.Context, responseCh <-chan []byte, conn net.Conn, tunnelID, connectionID string) {
	for {
		select {
		case data, ok := <-responseCh:
			if !ok {
				return
			}
			if len(data) > 0 {
				if _, err := conn.Write(data); err != nil {
					h.logger.Debug("client write error",
						zap.String("tunnel_id", tunnelID),
						zap.String("connection_id", connectionID),
						zap.Error(err),
					)
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// authenticate validates the request has proper authentication.
// Supports tunnel token (header, query param, Basic Auth) and API key authentication.
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
// Checks (in order):
//  1. X-Marionette-Tunnel-Token header
//  2. marionette_token query parameter
//  3. HTTP Basic Auth (password field)
func (h *TunnelProxyHandler) extractTunnelToken(r *http.Request) string {
	// Check X-Marionette-Tunnel-Token header
	if token := r.Header.Get("X-Marionette-Tunnel-Token"); strings.HasPrefix(token, "ttok_") {
		return token
	}

	// Check marionette_token query parameter
	if token := r.URL.Query().Get("marionette_token"); strings.HasPrefix(token, "ttok_") {
		return token
	}

	// Check HTTP Basic Auth (password = token, username ignored)
	if _, password, ok := r.BasicAuth(); ok && strings.HasPrefix(password, "ttok_") {
		return password
	}

	return ""
}

// sanitizeRequest removes tunnel authentication info from the request
// before forwarding it to the backend service.
// This prevents leaking tunnel tokens to the proxied service.
func (h *TunnelProxyHandler) sanitizeRequest(r *http.Request) {
	// Remove Marionette-specific headers
	r.Header.Del("X-Marionette-Tunnel-Token")
	r.Header.Del("X-Marionette-API-Key")

	// Remove Basic Auth if it contains a tunnel token
	if _, password, ok := r.BasicAuth(); ok && strings.HasPrefix(password, "ttok_") {
		r.Header.Del("Authorization")
	}

	// Remove marionette_token query parameter
	query := r.URL.Query()
	if query.Has("marionette_token") {
		query.Del("marionette_token")
		r.URL.RawQuery = query.Encode()
		// Update RequestURI as well
		r.RequestURI = r.URL.Path
		if r.URL.RawQuery != "" {
			r.RequestURI += "?" + r.URL.RawQuery
		}
	}
}
