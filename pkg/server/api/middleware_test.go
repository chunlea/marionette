package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// websocketHandshake builds a request that looks like what a browser sends
// when it opens a WebSocket, with the API key where a browser can put it.
func websocketHandshake(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "keep-alive, Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return req
}

// The dashboard's log and event streams have always sent ?token=, and the
// stream handlers have always read it, but the auth middleware covered the
// whole /api/v1 subtree and only looked at the Authorization header — so every
// browser WebSocket got a 401 before reaching the handler that would have
// accepted it.
func TestWebSocketHandshakeAuthenticatesWithAQueryToken(t *testing.T) {
	logStream := NewMockLogStreamService()
	srv, _, token := testServer(t, WithLogStreamService(logStream))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, websocketHandshake("/api/v1/logs/task_1/stream?token="+token))

	// httptest.ResponseRecorder cannot be hijacked, so the upgrade itself
	// fails — but only after authentication has passed, which is what matters
	// here. A 401 would mean the request never reached the handler.
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
	assert.NotEqual(t, http.StatusForbidden, rec.Code)
}

func TestWebSocketHandshakeRejectsABadQueryToken(t *testing.T) {
	srv, _, _ := testServer(t, WithLogStreamService(NewMockLogStreamService()))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, websocketHandshake("/api/v1/logs/task_1/stream?token=mk_not_a_key"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebSocketHandshakeRequiresSomeCredential(t *testing.T) {
	srv, _, _ := testServer(t, WithLogStreamService(NewMockLogStreamService()))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, websocketHandshake("/api/v1/logs/task_1/stream"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// The query form is a concession to the WebSocket API, not a second way to
// authenticate: URLs reach access logs, proxies and Referer headers.
func TestOrdinaryRequestsCannotAuthenticateWithAQueryToken(t *testing.T) {
	srv, _, token := testServer(t)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?token="+token, nil))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTokenFromRequest(t *testing.T) {
	t.Run("bearer header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer mk_abc")
		got, err := tokenFromRequest(req)
		require.Nil(t, err)
		assert.Equal(t, "mk_abc", got)
	})

	t.Run("a header always wins over the query", func(t *testing.T) {
		req := websocketHandshake("/api/v1/events?token=mk_query")
		req.Header.Set("Authorization", "Bearer mk_header")
		got, err := tokenFromRequest(req)
		require.Nil(t, err)
		assert.Equal(t, "mk_header", got)
	})

	t.Run("malformed header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
		req.Header.Set("Authorization", "mk_abc")
		_, err := tokenFromRequest(req)
		require.NotNil(t, err)
		assert.Equal(t, "invalid_auth", err.code)
	})
}
