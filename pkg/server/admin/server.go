package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// Server is the admin API HTTP server.
type Server struct {
	server *http.Server
	logger *zap.Logger
}

// Config holds configuration for the admin server.
type Config struct {
	Port int
}

// New creates a new admin server.
func New(cfg Config, logger *zap.Logger) *Server {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Health endpoints
	r.Get("/health", healthHandler)
	r.Get("/healthz", healthHandler)

	// Status endpoint - returns all service statuses
	r.Get("/api/status", statusHandler)

	// Serve embedded frontend for all other routes
	r.Handle("/*", staticHandler())

	return &Server{
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Port),
			Handler:      r,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger: logger,
	}
}

// Start starts the admin server.
func (s *Server) Start() error {
	s.logger.Info("starting admin API server", zap.String("addr", s.server.Addr))
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("admin server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down admin API server")
	return s.server.Shutdown(ctx)
}
