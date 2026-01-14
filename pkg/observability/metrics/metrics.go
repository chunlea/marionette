// Package metrics provides Prometheus metrics for the Marionette server.
// It includes HTTP request metrics, gRPC metrics, and business metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const (
	// DefaultNamespace is the default Prometheus namespace for all metrics.
	DefaultNamespace = "marionette"
)

// Registry holds all Prometheus metrics for the application.
type Registry struct {
	namespace string
	registry  *prometheus.Registry

	// HTTP metrics
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestSize     *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec

	// gRPC metrics
	GRPCRequestsTotal   *prometheus.CounterVec
	GRPCRequestDuration *prometheus.HistogramVec

	// Business metrics
	RunnersConnected          *prometheus.GaugeVec
	SessionsTotal             *prometheus.GaugeVec
	TasksTotal                *prometheus.CounterVec
	PermissionRequestsPending prometheus.Gauge

	// Database metrics
	DBPoolConnections *prometheus.GaugeVec
}

// NewRegistry creates a new metrics registry with all metrics registered.
func NewRegistry(namespace string) *Registry {
	if namespace == "" {
		namespace = DefaultNamespace
	}

	reg := prometheus.NewRegistry()

	r := &Registry{
		namespace: namespace,
		registry:  reg,

		// HTTP metrics
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		HTTPRequestSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_size_bytes",
				Help:      "HTTP request size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, 7), // 100B to 100MB
			},
			[]string{"method", "path"},
		),
		HTTPResponseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_response_size_bytes",
				Help:      "HTTP response size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, 7), // 100B to 100MB
			},
			[]string{"method", "path"},
		),

		// gRPC metrics
		GRPCRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "grpc_requests_total",
				Help:      "Total number of gRPC requests",
			},
			[]string{"method", "status"},
		),
		GRPCRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "grpc_request_duration_seconds",
				Help:      "gRPC request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method"},
		),

		// Business metrics
		RunnersConnected: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "runners_connected",
				Help:      "Number of connected runners by status",
			},
			[]string{"status"},
		),
		SessionsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "sessions_total",
				Help:      "Total number of sessions by status",
			},
			[]string{"status"},
		),
		TasksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tasks_total",
				Help:      "Total number of tasks by status",
			},
			[]string{"status"},
		),
		PermissionRequestsPending: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "permission_requests_pending",
				Help:      "Number of pending permission requests",
			},
		),

		// Database metrics
		DBPoolConnections: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "db_pool_connections",
				Help:      "Number of database pool connections by state",
			},
			[]string{"state"},
		),
	}

	// Register all metrics
	reg.MustRegister(
		// HTTP
		r.HTTPRequestsTotal,
		r.HTTPRequestDuration,
		r.HTTPRequestSize,
		r.HTTPResponseSize,
		// gRPC
		r.GRPCRequestsTotal,
		r.GRPCRequestDuration,
		// Business
		r.RunnersConnected,
		r.SessionsTotal,
		r.TasksTotal,
		r.PermissionRequestsPending,
		// Database
		r.DBPoolConnections,
	)

	// Register Go runtime metrics
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return r
}

// PrometheusRegistry returns the underlying Prometheus registry.
func (r *Registry) PrometheusRegistry() *prometheus.Registry {
	return r.registry
}

// Namespace returns the metrics namespace.
func (r *Registry) Namespace() string {
	return r.namespace
}
