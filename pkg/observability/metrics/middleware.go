package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// HTTPMiddleware returns a chi middleware that records HTTP metrics.
func HTTPMiddleware(reg *Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code and size
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// Process request
			next.ServeHTTP(ww, r)

			// Record metrics after request completes
			duration := time.Since(start).Seconds()
			status := strconv.Itoa(ww.Status())
			path := getRoutePath(r)

			reg.HTTPRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
			reg.HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)

			// Record request size if available
			if r.ContentLength > 0 {
				reg.HTTPRequestSize.WithLabelValues(r.Method, path).Observe(float64(r.ContentLength))
			}

			// Record response size
			if ww.BytesWritten() > 0 {
				reg.HTTPResponseSize.WithLabelValues(r.Method, path).Observe(float64(ww.BytesWritten()))
			}
		})
	}
}

// getRoutePath returns the route pattern for the request.
// This avoids high cardinality by using the pattern instead of actual path.
func getRoutePath(r *http.Request) string {
	// Try to get the route pattern from chi
	rctx := chi.RouteContext(r.Context())
	if rctx != nil && rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	// Fallback to path (may cause high cardinality for dynamic routes)
	return r.URL.Path
}
