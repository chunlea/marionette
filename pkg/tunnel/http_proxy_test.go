package tunnel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			defer func() { _ = resp.Body.Close() }()

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

	//nolint:bodyclose // Error case - response is nil when size check fails before parsing
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
	defer func() { _ = resp.Body.Close() }()

	// Create a recorder
	w := httptest.NewRecorder()

	err = proxy.WriteResponse(w, resp)
	require.NoError(t, err)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()

	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, "text/plain", result.Header.Get("Content-Type"))
	assert.Equal(t, "value", result.Header.Get("X-Custom"))

	body, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello World!", string(body))
}
