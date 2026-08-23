package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// proxied stands up an upstream, wraps handler in the proxy middleware, and
// returns a client-facing server — the same shape the admin server gets when
// cmd/server passes the middleware to admin.New.
func proxied(t *testing.T, prefix string, upstream http.Handler, fallthroughHandler http.Handler) *httptest.Server {
	t.Helper()

	upstreamServer := httptest.NewServer(upstream)
	t.Cleanup(upstreamServer.Close)

	middleware, err := NewUpstreamProxy(prefix, upstreamServer.URL, zap.NewNop())
	require.NoError(t, err)

	front := httptest.NewServer(middleware(fallthroughHandler))
	t.Cleanup(front.Close)
	return front
}

func TestUpstreamProxyForwardsMatchingPaths(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Path", r.URL.RequestURI())
		w.Header().Set("X-Upstream-Auth", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("from the api"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("index.html"))
	})

	front := proxied(t, "/api/v1", upstream, spa)

	req, err := http.NewRequest(http.MethodGet, front.URL+"/api/v1/sessions?limit=5", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer mk_test")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "/api/v1/sessions?limit=5", resp.Header.Get("X-Upstream-Path"),
		"path and query must reach the API untouched")
	assert.Equal(t, "Bearer mk_test", resp.Header.Get("X-Upstream-Auth"),
		"the API authenticates every request itself, so its credential must survive the hop")
}

func TestUpstreamProxyLeavesOtherPathsAlone(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("request should not have been proxied")
		w.WriteHeader(http.StatusTeapot)
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("index.html"))
	})

	front := proxied(t, "/api/v1", upstream, spa)

	for _, path := range []string{
		"/",
		"/admin/api/v1/keys",
		"/assets/index.js",
		// The dashboard has a /admin/api-keys route; a prefix match that is not
		// segment-aware would swallow paths like this one.
		"/api/v1thing",
	} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(front.URL + path)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "index.html", readBody(t, resp))
		})
	}
}

func TestUpstreamProxyRelaysWebSockets(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The browser cannot set headers on a handshake, so the dashboard puts
		// the API key in the query string; it has to survive the hop.
		token := r.URL.Query().Get("token")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteMessage(websocket.TextMessage, []byte("token="+token))
	})

	front := proxied(t, "/api/v1", upstream, http.NotFoundHandler())

	wsURL := "ws" + strings.TrimPrefix(front.URL, "http") + "/api/v1/logs/task_1/stream?token=mk_test"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	defer func() { _ = resp.Body.Close() }()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "token=mk_test", string(message))
}

func TestUpstreamProxyReportsAnUnreachableAPI(t *testing.T) {
	// A closed port stands in for the API server being down.
	dead := httptest.NewServer(http.NotFoundHandler())
	target := dead.URL
	dead.Close()

	middleware, err := NewUpstreamProxy("/api/v1", target, zap.NewNop())
	require.NoError(t, err)
	front := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer front.Close()

	resp, err := http.Get(front.URL + "/api/v1/sessions")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, readBody(t, resp), "upstream_unavailable")
}

func TestNewUpstreamProxyRejectsUnusableConfiguration(t *testing.T) {
	for name, tc := range map[string]struct{ prefix, target string }{
		"empty prefix":    {"", "http://localhost:8080"},
		"relative prefix": {"api/v1", "http://localhost:8080"},
		"no scheme":       {"/api/v1", "localhost:8080"},
		"unparseable":     {"/api/v1", "://nope"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewUpstreamProxy(tc.prefix, tc.target, zap.NewNop())
			assert.Error(t, err)
		})
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 512)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(sb.String())
}
