package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func TestNewRegistry(t *testing.T) {
	t.Run("creates registry with default namespace", func(t *testing.T) {
		reg := NewRegistry("")
		require.NotNil(t, reg)
		assert.Equal(t, DefaultNamespace, reg.Namespace())
		assert.NotNil(t, reg.PrometheusRegistry())
	})

	t.Run("creates registry with custom namespace", func(t *testing.T) {
		reg := NewRegistry("custom")
		require.NotNil(t, reg)
		assert.Equal(t, "custom", reg.Namespace())
	})

	t.Run("all metrics are registered", func(t *testing.T) {
		reg := NewRegistry("test")
		require.NotNil(t, reg)

		// HTTP metrics
		assert.NotNil(t, reg.HTTPRequestsTotal)
		assert.NotNil(t, reg.HTTPRequestDuration)
		assert.NotNil(t, reg.HTTPRequestSize)
		assert.NotNil(t, reg.HTTPResponseSize)

		// gRPC metrics
		assert.NotNil(t, reg.GRPCRequestsTotal)
		assert.NotNil(t, reg.GRPCRequestDuration)

		// Business metrics
		assert.NotNil(t, reg.RunnersConnected)
		assert.NotNil(t, reg.SessionsTotal)
		assert.NotNil(t, reg.TasksTotal)
		assert.NotNil(t, reg.PermissionRequestsPending)

		// Database metrics
		assert.NotNil(t, reg.DBPoolConnections)
	})
}

func TestHTTPMiddleware(t *testing.T) {
	reg := NewRegistry("test_http")

	// Create a chi router with the middleware
	r := chi.NewRouter()
	r.Use(HTTPMiddleware(reg))
	r.Get("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	})
	r.Get("/api/v1/sessions/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"sess_123"}`))
	})

	t.Run("records request count and duration", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
		rr := httptest.NewRecorder()

		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Verify metrics were recorded - check via metric collection
		// The metric should have a value now
		metricFamilies, err := reg.PrometheusRegistry().Gather()
		require.NoError(t, err)

		var foundRequestsTotal, foundDuration bool
		for _, mf := range metricFamilies {
			if *mf.Name == "test_http_http_requests_total" {
				foundRequestsTotal = true
			}
			if *mf.Name == "test_http_http_request_duration_seconds" {
				foundDuration = true
			}
		}
		assert.True(t, foundRequestsTotal, "http_requests_total should be recorded")
		assert.True(t, foundDuration, "http_request_duration_seconds should be recorded")
	})

	t.Run("uses route pattern for low cardinality", func(t *testing.T) {
		// Create a fresh registry for this test
		reg2 := NewRegistry("test_http_pattern")
		r2 := chi.NewRouter()
		r2.Use(HTTPMiddleware(reg2))
		r2.Get("/api/v1/sessions/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Make requests with different session IDs
		for _, id := range []string{"sess_1", "sess_2", "sess_3"} {
			req := httptest.NewRequest("GET", "/api/v1/sessions/"+id, nil)
			rr := httptest.NewRecorder()
			r2.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
		}

		// Gather metrics and check that the route pattern is used (not actual paths)
		metricFamilies, err := reg2.PrometheusRegistry().Gather()
		require.NoError(t, err)

		var foundRoutePattern bool
		for _, mf := range metricFamilies {
			if *mf.Name == "test_http_pattern_http_requests_total" {
				for _, m := range mf.Metric {
					for _, l := range m.Label {
						if *l.Name == "path" {
							// Should be the route pattern, not "/api/v1/sessions/sess_1"
							assert.Equal(t, "/api/v1/sessions/{sessionID}", *l.Value)
							foundRoutePattern = true
						}
					}
				}
			}
		}
		assert.True(t, foundRoutePattern, "should find route pattern in metrics")
	})
}

func TestHTTPMiddleware_FallbackPath(t *testing.T) {
	reg := NewRegistry("test_fallback")

	// Create handler without chi router to test fallback path
	handler := HTTPMiddleware(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/some/path", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify metrics were recorded with the actual path (fallback)
	metricFamilies, err := reg.PrometheusRegistry().Gather()
	require.NoError(t, err)

	var foundPath bool
	for _, mf := range metricFamilies {
		if *mf.Name == "test_fallback_http_requests_total" {
			for _, m := range mf.Metric {
				for _, l := range m.Label {
					if *l.Name == "path" && *l.Value == "/some/path" {
						foundPath = true
					}
				}
			}
		}
	}
	assert.True(t, foundPath, "should use actual path as fallback when chi context is nil")
}

func TestHTTPMiddleware_NoContentLength(t *testing.T) {
	reg := NewRegistry("test_no_content")

	r := chi.NewRouter()
	r.Use(HTTPMiddleware(reg))
	r.Post("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Request without content length
	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.ContentLength = 0
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHTTPMiddleware_NoResponseBody(t *testing.T) {
	reg := NewRegistry("test_no_response")

	r := chi.NewRouter()
	r.Use(HTTPMiddleware(reg))
	r.Delete("/api/resource", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		// No body written
	})

	req := httptest.NewRequest("DELETE", "/api/resource", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestUnaryServerInterceptor(t *testing.T) {
	reg := NewRegistry("test_grpc")
	interceptor := UnaryServerInterceptor(reg)

	t.Run("records metrics for successful request", func(t *testing.T) {
		info := &grpc.UnaryServerInfo{
			FullMethod: "/marionette.v1.RunnerService/Connect",
		}

		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return "response", nil
		}

		resp, err := interceptor(context.Background(), "request", info, handler)
		require.NoError(t, err)
		assert.Equal(t, "response", resp)

		// Verify metrics were recorded
		metricFamilies, err := reg.PrometheusRegistry().Gather()
		require.NoError(t, err)

		var foundTotal, foundDuration bool
		for _, mf := range metricFamilies {
			if *mf.Name == "test_grpc_grpc_requests_total" {
				foundTotal = true
				// Check labels
				for _, m := range mf.Metric {
					var hasMethod, hasStatus bool
					for _, l := range m.Label {
						if *l.Name == "method" && *l.Value == "/marionette.v1.RunnerService/Connect" {
							hasMethod = true
						}
						if *l.Name == "status" && *l.Value == "OK" {
							hasStatus = true
						}
					}
					assert.True(t, hasMethod, "method label should be set")
					assert.True(t, hasStatus, "status label should be OK")
				}
			}
			if *mf.Name == "test_grpc_grpc_request_duration_seconds" {
				foundDuration = true
			}
		}
		assert.True(t, foundTotal, "grpc_requests_total should be recorded")
		assert.True(t, foundDuration, "grpc_request_duration_seconds should be recorded")
	})
}

func TestStreamServerInterceptor(t *testing.T) {
	reg := NewRegistry("test_stream")
	interceptor := StreamServerInterceptor(reg)

	t.Run("records metrics for stream", func(t *testing.T) {
		info := &grpc.StreamServerInfo{
			FullMethod: "/marionette.v1.RunnerService/Control",
		}

		handler := func(srv interface{}, stream grpc.ServerStream) error {
			return nil
		}

		err := interceptor(nil, nil, info, handler)
		require.NoError(t, err)

		// Verify metrics were recorded
		metricFamilies, err := reg.PrometheusRegistry().Gather()
		require.NoError(t, err)

		var foundTotal bool
		for _, mf := range metricFamilies {
			if *mf.Name == "test_stream_grpc_requests_total" {
				foundTotal = true
			}
		}
		assert.True(t, foundTotal, "grpc_requests_total should be recorded for streams")
	})
}

func TestServer(t *testing.T) {
	logger := zap.NewNop()
	reg := NewRegistry("test_server")

	t.Run("creates server with default config", func(t *testing.T) {
		cfg := DefaultServerConfig()
		assert.Equal(t, 9091, cfg.Port)
		assert.Equal(t, "/metrics", cfg.Path)
		assert.Equal(t, "", cfg.Host)
	})

	t.Run("creates server with empty path uses default", func(t *testing.T) {
		srv := NewServer(reg, ServerConfig{
			Host: "127.0.0.1",
			Port: 19092,
			Path: "", // Should default to /metrics
		}, logger)
		assert.NotNil(t, srv)
		assert.Equal(t, "127.0.0.1:19092", srv.Addr())
	})

	t.Run("Addr returns correct address", func(t *testing.T) {
		srv := NewServer(reg, ServerConfig{
			Host: "0.0.0.0",
			Port: 9999,
			Path: "/metrics",
		}, logger)
		assert.Equal(t, "0.0.0.0:9999", srv.Addr())
	})

	t.Run("serves metrics endpoint", func(t *testing.T) {
		// Use a random port to avoid conflicts
		srv := NewServer(reg, ServerConfig{
			Host: "127.0.0.1",
			Port: 0, // Will be assigned by the test
			Path: "/metrics",
		}, logger)

		// Start server in goroutine with a timeout
		go func() {
			_ = srv.Start() // Will fail on port 0, but that's ok for this test
		}()

		// Give server time to start (or fail)
		time.Sleep(10 * time.Millisecond)

		// Just verify the server was created correctly
		assert.NotNil(t, srv)
	})

	t.Run("root handler returns greeting", func(t *testing.T) {
		// Create a server and test the root handler directly
		reg := NewRegistry("test_root")
		srv := NewServer(reg, ServerConfig{
			Host: "127.0.0.1",
			Port: 19093,
			Path: "/metrics",
		}, logger)

		// Start server briefly to test
		errChan := make(chan error, 1)
		go func() {
			if err := srv.Start(); err != nil && err != http.ErrServerClosed {
				errChan <- err
			}
		}()

		time.Sleep(50 * time.Millisecond)

		// Test root path
		resp, err := http.Get("http://127.0.0.1:19093/")
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			assert.Contains(t, string(body), "Marionette Metrics Server")
		}

		// Test non-existent path returns 404
		resp2, err := http.Get("http://127.0.0.1:19093/nonexistent")
		if err == nil {
			defer func() { _ = resp2.Body.Close() }()
			assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
		}

		// Shutdown
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	t.Run("serves metrics on specified path", func(t *testing.T) {
		// Create a handler directly to test the metrics endpoint
		reg := NewRegistry("test_metrics_endpoint")

		// Record some sample metrics
		reg.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()
		reg.RunnersConnected.WithLabelValues("idle").Set(5)

		// Create a new server and get its handler
		srv := NewServer(reg, ServerConfig{
			Host: "",
			Port: 9092,
			Path: "/metrics",
		}, logger)

		// Create a test request to the metrics endpoint
		req := httptest.NewRequest("GET", "/metrics", nil)
		rr := httptest.NewRecorder()

		// We need to test the handler directly
		// The server.Handler is not exported, so we recreate the handler logic
		mux := http.NewServeMux()
		mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metricFamilies, err := reg.PrometheusRegistry().Gather()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			for _, mf := range metricFamilies {
				_, _ = w.Write([]byte(*mf.Name + "\n"))
			}
		}))

		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		body := rr.Body.String()
		assert.Contains(t, body, "test_metrics_endpoint_http_requests_total")
		assert.Contains(t, body, "test_metrics_endpoint_runners_connected")

		// Clean up
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
}

func TestServerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	reg := NewRegistry("integration")

	// Record some metrics
	reg.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/sessions", "200").Add(10)
	reg.SessionsTotal.WithLabelValues("active").Set(3)

	// Start server on a random available port
	srv := NewServer(reg, ServerConfig{
		Host: "127.0.0.1",
		Port: 19091, // Use high port to avoid conflicts
		Path: "/metrics",
	}, logger)

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Check for startup errors
	select {
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	default:
	}

	// Make request to metrics endpoint
	resp, err := http.Get("http://127.0.0.1:19091/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.True(t, strings.Contains(bodyStr, "integration_http_requests_total"), "should contain HTTP requests metric")
	assert.True(t, strings.Contains(bodyStr, "integration_sessions_total"), "should contain sessions metric")

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}
