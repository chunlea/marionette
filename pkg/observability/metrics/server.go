package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// ServerConfig holds configuration for the metrics server.
type ServerConfig struct {
	Host string
	Port int
	Path string
}

// DefaultServerConfig returns the default metrics server configuration.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Host: "",
		Port: 9091,
		Path: "/metrics",
	}
}

// Server is an HTTP server that exposes Prometheus metrics.
type Server struct {
	server   *http.Server
	registry *Registry
	logger   *zap.Logger
	config   ServerConfig
}

// NewServer creates a new metrics server.
func NewServer(reg *Registry, cfg ServerConfig, logger *zap.Logger) *Server {
	if cfg.Path == "" {
		cfg.Path = "/metrics"
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, promhttp.HandlerFor(
		reg.PrometheusRegistry(),
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	// Add a simple health check for the metrics server itself
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Marionette Metrics Server\n"))
	})

	return &Server{
		server: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		registry: reg,
		logger:   logger,
		config:   cfg,
	}
}

// Start starts the metrics server.
func (s *Server) Start() error {
	s.logger.Info("starting metrics server",
		zap.String("addr", s.server.Addr),
		zap.String("path", s.config.Path),
	)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("metrics server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the metrics server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down metrics server")
	return s.server.Shutdown(ctx)
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.server.Addr
}
