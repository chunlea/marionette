package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"time"
)

// HTTPProxyConfig holds configuration for HTTP proxying.
type HTTPProxyConfig struct {
	// ReadTimeout is the timeout for reading the request body.
	ReadTimeout time.Duration
	// WriteTimeout is the timeout for writing the response.
	WriteTimeout time.Duration
	// MaxRequestSize is the maximum size of the request body.
	MaxRequestSize int64
	// MaxResponseSize is the maximum size of the response body.
	MaxResponseSize int64
}

// DefaultHTTPProxyConfig returns default HTTP proxy configuration.
func DefaultHTTPProxyConfig() HTTPProxyConfig {
	return HTTPProxyConfig{
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxRequestSize:  10 * 1024 * 1024,  // 10MB
		MaxResponseSize: 100 * 1024 * 1024, // 100MB
	}
}

// HTTPProxy handles HTTP request proxying through tunnels.
type HTTPProxy struct {
	config HTTPProxyConfig
}

// NewHTTPProxy creates a new HTTP proxy with the given configuration.
func NewHTTPProxy(config HTTPProxyConfig) *HTTPProxy {
	return &HTTPProxy{config: config}
}

// SerializeRequest serializes an HTTP request for transmission.
// The format is the raw HTTP/1.1 wire format.
func (p *HTTPProxy) SerializeRequest(r *http.Request) ([]byte, error) {
	// Limit request body size
	if r.ContentLength > p.config.MaxRequestSize {
		return nil, fmt.Errorf("request body too large: %d > %d", r.ContentLength, p.config.MaxRequestSize)
	}

	// Use httputil.DumpRequest for proper serialization
	// This handles all the edge cases of HTTP request formatting
	dump, err := httputil.DumpRequest(r, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	return dump, nil
}

// DeserializeResponse deserializes an HTTP response from raw bytes.
func (p *HTTPProxy) DeserializeResponse(data []byte) (*http.Response, error) {
	if int64(len(data)) > p.config.MaxResponseSize {
		return nil, fmt.Errorf("response too large: %d > %d", len(data), p.config.MaxResponseSize)
	}

	// Parse the response using http.ReadResponse
	reader := bufio.NewReader(bytes.NewReader(data))
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp, nil
}

// WriteResponse writes an HTTP response to the ResponseWriter.
func (p *HTTPProxy) WriteResponse(w http.ResponseWriter, resp *http.Response) error {
	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy body
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
		_, err := io.Copy(w, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to write response body: %w", err)
		}
	}

	return nil
}

// ProxyHTTPRequest proxies an HTTP request through the tunnel.
func (p *HTTPProxy) ProxyHTTPRequest(
	ctx context.Context,
	tunnelID string,
	handler ConnectionHandler,
	w http.ResponseWriter,
	r *http.Request,
) error {
	// Serialize the request
	reqData, err := p.SerializeRequest(r)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %w", err)
	}

	// Send request to runner
	if err := handler.SendTunnelData(ctx, tunnelID, reqData); err != nil {
		return fmt.Errorf("failed to send request to runner: %w", err)
	}

	// Wait for response with timeout
	respCtx, cancel := context.WithTimeout(ctx, p.config.WriteTimeout)
	defer cancel()

	respData, err := handler.ReceiveTunnelData(respCtx, tunnelID)
	if err != nil {
		return fmt.Errorf("failed to receive response from runner: %w", err)
	}

	// Deserialize response
	resp, err := p.DeserializeResponse(respData)
	if err != nil {
		return fmt.Errorf("failed to deserialize response: %w", err)
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	// Write response to client
	if err := p.WriteResponse(w, resp); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	return nil
}

// handleHTTPProxyRequest is the internal implementation for HTTP proxying.
func (m *TunnelManager) handleHTTPProxyRequest(
	ctx context.Context,
	tunnelID string,
	handler ConnectionHandler,
	w http.ResponseWriter,
	r *http.Request,
) error {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())
	return proxy.ProxyHTTPRequest(ctx, tunnelID, handler, w, r)
}
