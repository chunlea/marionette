package tunnel

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPProxy_SerializeRequest(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	tests := []struct {
		name    string
		method  string
		url     string
		body    string
		headers map[string]string
	}{
		{
			name:   "simple GET",
			method: "GET",
			url:    "http://example.com/path",
			body:   "",
		},
		{
			name:   "POST with body",
			method: "POST",
			url:    "http://example.com/api",
			body:   `{"key": "value"}`,
			headers: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name:   "GET with headers",
			method: "GET",
			url:    "http://example.com/",
			headers: map[string]string{
				"Authorization": "Bearer token123",
				"X-Custom":      "custom-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}

			req, err := http.NewRequest(tt.method, tt.url, body)
			require.NoError(t, err)

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			data, err := proxy.SerializeRequest(req)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Verify the serialized data contains expected content
			serialized := string(data)
			assert.Contains(t, serialized, tt.method)
			if tt.body != "" {
				assert.Contains(t, serialized, tt.body)
			}
		})
	}
}

func TestHTTPProxy_SerializeRequest_TooLarge(t *testing.T) {
	config := DefaultHTTPProxyConfig()
	config.MaxRequestSize = 100 // Very small limit
	proxy := NewHTTPProxy(config)

	// Create a request with a large body
	body := strings.Repeat("x", 1000)
	req, err := http.NewRequest("POST", "http://example.com/", strings.NewReader(body))
	require.NoError(t, err)
	req.ContentLength = int64(len(body))

	_, err = proxy.SerializeRequest(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestHTTPProxy_DeserializeResponse(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	tests := []struct {
		name           string
		rawResponse    string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "200 OK",
			rawResponse:    "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello",
			expectedStatus: 200,
			expectedBody:   "hello",
		},
		{
			name:           "404 Not Found",
			rawResponse:    "HTTP/1.1 404 Not Found\r\nContent-Length: 9\r\n\r\nnot found",
			expectedStatus: 404,
			expectedBody:   "not found",
		},
		{
			name:           "with headers",
			rawResponse:    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 12\r\n\r\n{\"ok\": true}",
			expectedStatus: 200,
			expectedBody:   `{"ok": true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := proxy.DeserializeResponse([]byte(tt.rawResponse))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedBody, string(body))
		})
	}
}

func TestHTTPProxy_DeserializeResponse_TooLarge(t *testing.T) {
	config := DefaultHTTPProxyConfig()
	config.MaxResponseSize = 100
	proxy := NewHTTPProxy(config)

	// Create a response larger than the limit
	largeBody := strings.Repeat("x", 200)
	rawResponse := "HTTP/1.1 200 OK\r\nContent-Length: 200\r\n\r\n" + largeBody

	_, err := proxy.DeserializeResponse([]byte(rawResponse))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestHTTPProxy_WriteResponse(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	// Create a mock response
	rawResponse := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nX-Custom: value\r\nContent-Length: 12\r\n\r\nHello World!"
	resp, err := proxy.DeserializeResponse([]byte(rawResponse))
	require.NoError(t, err)

	// Create a recorder
	w := httptest.NewRecorder()

	err = proxy.WriteResponse(w, resp)
	require.NoError(t, err)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, "text/plain", result.Header.Get("Content-Type"))
	assert.Equal(t, "value", result.Header.Get("X-Custom"))

	body, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello World!", string(body))
}

// mockHTTPConnectionHandler implements ConnectionHandler for testing HTTP proxy.
type mockHTTPConnectionHandler struct {
	connected    bool
	sentData     []byte
	responseData []byte
	sendErr      error
	receiveErr   error
}

func (m *mockHTTPConnectionHandler) SendTunnelData(_ context.Context, _ string, data []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentData = data
	return nil
}

func (m *mockHTTPConnectionHandler) ReceiveTunnelData(_ context.Context, _ string) ([]byte, error) {
	if m.receiveErr != nil {
		return nil, m.receiveErr
	}
	return m.responseData, nil
}

func (m *mockHTTPConnectionHandler) CloseTunnel(_ context.Context, _ string) error {
	return nil
}

func (m *mockHTTPConnectionHandler) IsConnected() bool {
	return m.connected
}

func TestHTTPProxy_ProxyHTTPRequest(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	// Create a mock handler that returns a valid HTTP response
	handler := &mockHTTPConnectionHandler{
		connected:    true,
		responseData: []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 7\r\n\r\nSuccess"),
	}

	// Create a test request
	req, err := http.NewRequest("GET", "http://localhost:3000/test", nil)
	require.NoError(t, err)

	// Create a recorder
	w := httptest.NewRecorder()

	// Proxy the request
	err = proxy.ProxyHTTPRequest(context.Background(), "tun_test", handler, w, req)
	require.NoError(t, err)

	// Verify the request was sent
	assert.NotEmpty(t, handler.sentData)
	assert.Contains(t, string(handler.sentData), "GET /test")

	// Verify the response
	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, 200, result.StatusCode)
	body, _ := io.ReadAll(result.Body)
	assert.Equal(t, "Success", string(body))
}

func TestHTTPProxy_ProxyHTTPRequest_SendError(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	handler := &mockHTTPConnectionHandler{
		connected: true,
		sendErr:   assert.AnError,
	}

	req, _ := http.NewRequest("GET", "http://localhost/", nil)
	w := httptest.NewRecorder()

	err := proxy.ProxyHTTPRequest(context.Background(), "tun_test", handler, w, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request")
}

func TestHTTPProxy_ProxyHTTPRequest_ReceiveError(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	handler := &mockHTTPConnectionHandler{
		connected:  true,
		receiveErr: assert.AnError,
	}

	req, _ := http.NewRequest("GET", "http://localhost/", nil)
	w := httptest.NewRecorder()

	err := proxy.ProxyHTTPRequest(context.Background(), "tun_test", handler, w, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to receive response")
}

func TestHTTPProxy_ProxyHTTPRequest_InvalidResponse(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	handler := &mockHTTPConnectionHandler{
		connected:    true,
		responseData: []byte("invalid http response"),
	}

	req, _ := http.NewRequest("GET", "http://localhost/", nil)
	w := httptest.NewRecorder()

	err := proxy.ProxyHTTPRequest(context.Background(), "tun_test", handler, w, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to deserialize response")
}

func TestHTTPProxy_ProxyHTTPRequest_POSTWithBody(t *testing.T) {
	proxy := NewHTTPProxy(DefaultHTTPProxyConfig())

	handler := &mockHTTPConnectionHandler{
		connected:    true,
		responseData: []byte("HTTP/1.1 201 Created\r\nContent-Length: 7\r\n\r\nCreated"),
	}

	body := `{"name": "test"}`
	req, err := http.NewRequest("POST", "http://localhost:3000/api/users", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	err = proxy.ProxyHTTPRequest(context.Background(), "tun_test", handler, w, req)
	require.NoError(t, err)

	// Verify the request body was included
	assert.Contains(t, string(handler.sentData), body)

	// Verify response
	result := w.Result()
	defer result.Body.Close()
	assert.Equal(t, 201, result.StatusCode)
}

func TestDefaultHTTPProxyConfig(t *testing.T) {
	config := DefaultHTTPProxyConfig()

	assert.Greater(t, config.ReadTimeout, time.Duration(0))
	assert.Greater(t, config.WriteTimeout, time.Duration(0))
	assert.Greater(t, config.MaxRequestSize, int64(0))
	assert.Greater(t, config.MaxResponseSize, int64(0))
}
