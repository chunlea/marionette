package trace

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.False(t, cfg.Enabled)
	assert.Equal(t, "otlp", cfg.Exporter)
	assert.Equal(t, "localhost:4317", cfg.Endpoint)
	assert.Equal(t, "marionette-server", cfg.ServiceName)
	assert.Equal(t, 0.1, cfg.SampleRate)
	assert.True(t, cfg.Insecure)
}

func TestNewProvider_Disabled(t *testing.T) {
	logger := zap.NewNop()

	provider, err := NewProvider(context.Background(), Config{
		Enabled: false,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.False(t, provider.IsEnabled())

	// Shutdown should be a no-op
	err = provider.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNewProvider_Stdout(t *testing.T) {
	logger := zap.NewNop()

	provider, err := NewProvider(context.Background(), Config{
		Enabled:     true,
		Exporter:    "stdout",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.True(t, provider.IsEnabled())

	// Get a tracer
	tracer := provider.Tracer("test")
	assert.NotNil(t, tracer)

	// Shutdown
	err = provider.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNewProvider_Noop(t *testing.T) {
	logger := zap.NewNop()

	provider, err := NewProvider(context.Background(), Config{
		Enabled:     true,
		Exporter:    "noop",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.True(t, provider.IsEnabled())

	err = provider.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestNewProvider_InvalidExporter(t *testing.T) {
	logger := zap.NewNop()

	_, err := NewProvider(context.Background(), Config{
		Enabled:  true,
		Exporter: "invalid",
	}, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown exporter type")
}

func TestNewProvider_SampleRates(t *testing.T) {
	logger := zap.NewNop()

	testCases := []struct {
		name       string
		sampleRate float64
	}{
		{"always sample", 1.0},
		{"never sample", 0.0},
		{"10% sample", 0.1},
		{"above 100%", 1.5}, // Should be treated as always
		{"negative", -0.1},  // Should be treated as never
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewProvider(context.Background(), Config{
				Enabled:     true,
				Exporter:    "noop",
				ServiceName: "test-service",
				SampleRate:  tc.sampleRate,
			}, logger)

			require.NoError(t, err)
			require.NotNil(t, provider)

			err = provider.Shutdown(context.Background())
			require.NoError(t, err)
		})
	}
}

func TestHTTPMiddleware(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Set as global tracer provider
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create a chi router with the middleware
	r := chi.NewRouter()
	r.Use(HTTPMiddleware("test-service"))
	r.Get("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/api/v1/sessions/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("creates span for request", func(t *testing.T) {
		exporter.Reset()

		req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
		rr := httptest.NewRecorder()

		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Check that spans were created
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]
		assert.Equal(t, "/api/v1/sessions", span.Name)
	})

	t.Run("uses route pattern for span name", func(t *testing.T) {
		exporter.Reset()

		req := httptest.NewRequest("GET", "/api/v1/sessions/sess_123", nil)
		rr := httptest.NewRecorder()

		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]
		assert.Equal(t, "/api/v1/sessions/{sessionID}", span.Name)
	})

	t.Run("records error status for 4xx", func(t *testing.T) {
		exporter.Reset()

		// Route that returns 404
		req := httptest.NewRequest("GET", "/nonexistent", nil)
		rr := httptest.NewRecorder()

		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		// 404 should set error status
	})

	t.Run("propagates trace context", func(t *testing.T) {
		exporter.Reset()

		req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
		// Add trace context header
		req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		rr := httptest.NewRecorder()

		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]
		// Should have the parent trace ID
		assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", span.SpanContext.TraceID().String())
	})
}

func TestUnaryServerInterceptor(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otel.SetTracerProvider(tp)

	interceptor := UnaryServerInterceptor()

	t.Run("creates span for unary call", func(t *testing.T) {
		exporter.Reset()

		info := &grpc.UnaryServerInfo{
			FullMethod: "/marionette.v1.RunnerService/Connect",
		}

		handler := func(ctx context.Context, req any) (any, error) {
			return "response", nil
		}

		resp, err := interceptor(context.Background(), "request", info, handler)
		require.NoError(t, err)
		assert.Equal(t, "response", resp)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]
		assert.Equal(t, "/marionette.v1.RunnerService/Connect", span.Name)
	})
}

func TestStreamServerInterceptor(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otel.SetTracerProvider(tp)

	interceptor := StreamServerInterceptor()

	t.Run("creates span for stream", func(t *testing.T) {
		exporter.Reset()

		info := &grpc.StreamServerInfo{
			FullMethod:     "/marionette.v1.RunnerService/Control",
			IsClientStream: false,
			IsServerStream: true,
		}

		handler := func(srv any, stream grpc.ServerStream) error {
			return nil
		}

		// Create a mock server stream
		mockStream := &mockServerStream{ctx: context.Background()}

		err := interceptor(nil, mockStream, info, handler)
		require.NoError(t, err)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]
		assert.Equal(t, "/marionette.v1.RunnerService/Control", span.Name)
	})
}

// mockServerStream implements grpc.ServerStream for testing
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func TestMetadataCarrier(t *testing.T) {
	t.Run("Get returns value", func(t *testing.T) {
		mc := metadataCarrier{
			"traceparent": []string{"00-abc-def-01"},
		}
		assert.Equal(t, "00-abc-def-01", mc.Get("traceparent"))
	})

	t.Run("Get returns empty for missing key", func(t *testing.T) {
		mc := metadataCarrier{}
		assert.Equal(t, "", mc.Get("missing"))
	})

	t.Run("Set adds value", func(t *testing.T) {
		mc := metadataCarrier{}
		mc.Set("traceparent", "00-abc-def-01")
		assert.Equal(t, "00-abc-def-01", mc.Get("traceparent"))
	})

	t.Run("Keys returns all keys", func(t *testing.T) {
		mc := metadataCarrier{
			"key1": []string{"val1"},
			"key2": []string{"val2"},
		}
		keys := mc.Keys()
		assert.Len(t, keys, 2)
		assert.Contains(t, keys, "key1")
		assert.Contains(t, keys, "key2")
	})
}

func TestUnaryServerInterceptor_Error(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otel.SetTracerProvider(tp)

	interceptor := UnaryServerInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/marionette.v1.RunnerService/Connect",
	}

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(grpccodes.NotFound, "not found")
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	require.Error(t, err)
	assert.Nil(t, resp)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "/marionette.v1.RunnerService/Connect", span.Name)
	assert.Equal(t, codes.Error, span.Status.Code)
}

func TestStreamServerInterceptor_Error(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otel.SetTracerProvider(tp)

	interceptor := StreamServerInterceptor()

	info := &grpc.StreamServerInfo{
		FullMethod:     "/marionette.v1.RunnerService/Control",
		IsClientStream: false,
		IsServerStream: true,
	}

	handler := func(srv any, stream grpc.ServerStream) error {
		return status.Error(grpccodes.Internal, "internal error")
	}

	mockStream := &mockServerStream{ctx: context.Background()}

	err := interceptor(nil, mockStream, info, handler)
	require.Error(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "/marionette.v1.RunnerService/Control", span.Name)
	assert.Equal(t, codes.Error, span.Status.Code)
}

func TestProvider_Tracer_Enabled(t *testing.T) {
	logger := zap.NewNop()

	provider, err := NewProvider(context.Background(), Config{
		Enabled:     true,
		Exporter:    "noop",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.True(t, provider.IsEnabled())

	// Get tracer from enabled provider
	tracer := provider.Tracer("test")
	assert.NotNil(t, tracer)

	err = provider.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestResponseWriter_Flush(t *testing.T) {
	t.Run("delegates to underlying flusher", func(t *testing.T) {
		flushed := false
		underlying := &mockFlusher{
			ResponseWriter: httptest.NewRecorder(),
			onFlush:        func() { flushed = true },
		}

		rw := &responseWriter{ResponseWriter: underlying, statusCode: http.StatusOK}
		rw.Flush()

		assert.True(t, flushed)
	})

	t.Run("no-op if underlying does not implement Flusher", func(t *testing.T) {
		underlying := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: underlying, statusCode: http.StatusOK}

		// Should not panic
		rw.Flush()
	})
}

func TestResponseWriter_Hijack(t *testing.T) {
	t.Run("returns error if underlying does not implement Hijacker", func(t *testing.T) {
		underlying := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: underlying, statusCode: http.StatusOK}

		conn, buf, err := rw.Hijack()
		assert.Nil(t, conn)
		assert.Nil(t, buf)
		assert.Equal(t, http.ErrNotSupported, err)
	})

	t.Run("delegates to underlying hijacker", func(t *testing.T) {
		hijacked := false
		underlying := &mockHijacker{
			ResponseWriter: httptest.NewRecorder(),
			onHijack:       func() { hijacked = true },
		}

		rw := &responseWriter{ResponseWriter: underlying, statusCode: http.StatusOK}
		_, _, _ = rw.Hijack()

		assert.True(t, hijacked)
	})
}

func TestTracedServerStream_Context(t *testing.T) {
	ctx := context.WithValue(context.Background(), "key", "value")
	stream := &tracedServerStream{
		ctx: ctx,
	}

	assert.Equal(t, ctx, stream.Context())
	assert.Equal(t, "value", stream.Context().Value("key"))
}

func TestProvider_Tracer_Disabled(t *testing.T) {
	logger := zap.NewNop()

	provider, err := NewProvider(context.Background(), Config{
		Enabled: false,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.False(t, provider.IsEnabled())

	// Get tracer from disabled provider (should return global noop tracer)
	tracer := provider.Tracer("test")
	assert.NotNil(t, tracer)
}

func TestUnaryServerInterceptor_WithMetadata(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	interceptor := UnaryServerInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/marionette.v1.RunnerService/Connect",
	}

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	// Create context with gRPC metadata containing trace context
	md := metadata.New(map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, "request", info, handler)
	require.NoError(t, err)
	assert.Equal(t, "response", resp)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Should propagate trace context from metadata
	span := spans[0]
	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", span.SpanContext.TraceID().String())
}

func TestStreamServerInterceptor_WithMetadata(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	interceptor := StreamServerInterceptor()

	info := &grpc.StreamServerInfo{
		FullMethod:     "/marionette.v1.RunnerService/Control",
		IsClientStream: true,
		IsServerStream: true,
	}

	handler := func(srv any, stream grpc.ServerStream) error {
		return nil
	}

	// Create mock stream with metadata in context
	md := metadata.New(map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	mockStream := &mockServerStream{ctx: ctx}

	err := interceptor(nil, mockStream, info, handler)
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Should propagate trace context from metadata
	span := spans[0]
	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", span.SpanContext.TraceID().String())
}

func TestNewProvider_EmptyExporter(t *testing.T) {
	logger := zap.NewNop()

	// Empty exporter string should be treated as noop
	provider, err := NewProvider(context.Background(), Config{
		Enabled:     true,
		Exporter:    "",
		ServiceName: "test-service",
		SampleRate:  1.0,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.True(t, provider.IsEnabled())

	err = provider.Shutdown(context.Background())
	require.NoError(t, err)
}

// mockFlusher implements http.Flusher for testing
type mockFlusher struct {
	http.ResponseWriter
	onFlush func()
}

func (m *mockFlusher) Flush() {
	if m.onFlush != nil {
		m.onFlush()
	}
}

// mockHijacker implements http.Hijacker for testing
type mockHijacker struct {
	http.ResponseWriter
	onHijack func()
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if m.onHijack != nil {
		m.onHijack()
	}
	return nil, nil, nil
}
