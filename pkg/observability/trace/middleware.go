package trace

import (
	"bufio"
	"context"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	tracerName = "github.com/chunlea/marionette/pkg/observability/trace"
)

// HTTPMiddleware returns a chi middleware that creates spans for HTTP requests.
// The serviceName parameter is recorded as a span attribute for filtering.
func HTTPMiddleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from incoming request
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span with URL path (will be updated to route pattern after handler)
			ctx, span := tracer.Start(ctx, r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.ServiceName(serviceName),
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPath(r.URL.Path),
					semconv.URLScheme(r.URL.Scheme),
					semconv.ServerAddress(r.Host),
					semconv.UserAgentOriginal(r.UserAgent()),
				),
			)
			defer span.End()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Process request with traced context
			next.ServeHTTP(wrapped, r.WithContext(ctx))

			// Update span name to route pattern (low cardinality) after handler runs
			// Chi populates RouteContext during routing, so it's available after ServeHTTP
			if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
				span.SetName(rctx.RoutePattern())
			}

			// Record response attributes
			span.SetAttributes(
				semconv.HTTPResponseStatusCode(wrapped.statusCode),
			)

			// Set span status based on HTTP status code
			if wrapped.statusCode >= 400 {
				span.SetStatus(codes.Error, http.StatusText(wrapped.statusCode))
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
// It also implements http.Flusher and http.Hijacker by delegating to the
// underlying ResponseWriter if it supports those interfaces.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher. It delegates to the underlying ResponseWriter
// if it implements http.Flusher, which is needed for SSE and streaming responses.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker. It delegates to the underlying ResponseWriter
// if it implements http.Hijacker, which is needed for WebSocket upgrades.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// UnaryServerInterceptor returns a gRPC unary server interceptor for tracing.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		// Extract trace context from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			ctx = propagator.Extract(ctx, metadataCarrier(md))
		}

		// Start span
		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.RPCSystemGRPC,
				semconv.RPCMethod(info.FullMethod),
			),
		)
		defer span.End()

		// Handle request
		resp, err := handler(ctx, req)

		// Record status
		if err != nil {
			st, _ := status.FromError(err)
			span.SetAttributes(attribute.String("rpc.grpc.status_code", st.Code().String()))
			span.SetStatus(codes.Error, st.Message())
		} else {
			span.SetAttributes(attribute.String("rpc.grpc.status_code", grpccodes.OK.String()))
		}

		return resp, err
	}
}

// StreamServerInterceptor returns a gRPC stream server interceptor for tracing.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx := ss.Context()

		// Extract trace context from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			ctx = propagator.Extract(ctx, metadataCarrier(md))
		}

		// Start span
		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.RPCSystemGRPC,
				semconv.RPCMethod(info.FullMethod),
				attribute.Bool("rpc.grpc.is_client_stream", info.IsClientStream),
				attribute.Bool("rpc.grpc.is_server_stream", info.IsServerStream),
			),
		)
		defer span.End()

		// Wrap stream with traced context
		wrappedStream := &tracedServerStream{
			ServerStream: ss,
			ctx:          ctx,
		}

		// Handle stream
		err := handler(srv, wrappedStream)

		// Record status
		if err != nil {
			st, _ := status.FromError(err)
			span.SetAttributes(attribute.String("rpc.grpc.status_code", st.Code().String()))
			span.SetStatus(codes.Error, st.Message())
		} else {
			span.SetAttributes(attribute.String("rpc.grpc.status_code", grpccodes.OK.String()))
		}

		return err
	}
}

// tracedServerStream wraps grpc.ServerStream to provide traced context.
type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedServerStream) Context() context.Context {
	return s.ctx
}

// metadataCarrier adapts gRPC metadata for trace context propagation.
type metadataCarrier metadata.MD

func (mc metadataCarrier) Get(key string) string {
	values := metadata.MD(mc).Get(key)
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func (mc metadataCarrier) Set(key, value string) {
	metadata.MD(mc).Set(key, value)
}

func (mc metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(mc))
	for k := range mc {
		keys = append(keys, k)
	}
	return keys
}
