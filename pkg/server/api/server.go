// Package api provides the public HTTP API server for Marionette.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
)

// Server is the public HTTP API server.
type Server struct {
	server *http.Server
	router chi.Router
	logger *zap.Logger

	// Services
	sessions    SessionService
	tasks       TaskService
	runners     RunnerService
	permissions PermissionService
	workspaces  WorkspaceService

	// Tunnel service
	tunnels TunnelService

	// Streaming services
	logStream     LogStreamService
	eventStream   EventStreamService
	browserStream BrowserStreamService

	// Tunnel proxy
	tunnelProxy *TunnelProxyHandler

	// Auth
	apiKeyService *auth.APIKeyService
}

// Config holds configuration for the public API server.
type Config struct {
	Host string
	Port int
}

// Option is a functional option for configuring the server.
type Option func(*Server)

// WithSessionService sets the session service.
func WithSessionService(s SessionService) Option {
	return func(srv *Server) {
		srv.sessions = s
	}
}

// WithTaskService sets the task service.
func WithTaskService(s TaskService) Option {
	return func(srv *Server) {
		srv.tasks = s
	}
}

// WithRunnerService sets the runner service.
func WithRunnerService(s RunnerService) Option {
	return func(srv *Server) {
		srv.runners = s
	}
}

// WithPermissionService sets the permission service.
func WithPermissionService(s PermissionService) Option {
	return func(srv *Server) {
		srv.permissions = s
	}
}

// WithWorkspaceService sets the workspace service.
func WithWorkspaceService(s WorkspaceService) Option {
	return func(srv *Server) {
		srv.workspaces = s
	}
}

// WithAPIKeyService sets the API key service for authentication.
func WithAPIKeyService(s *auth.APIKeyService) Option {
	return func(srv *Server) {
		srv.apiKeyService = s
	}
}

// WithLogStreamService sets the log stream service for WebSocket log streaming.
func WithLogStreamService(s LogStreamService) Option {
	return func(srv *Server) {
		srv.logStream = s
	}
}

// WithEventStreamService sets the event stream service for WebSocket event streaming.
func WithEventStreamService(s EventStreamService) Option {
	return func(srv *Server) {
		srv.eventStream = s
	}
}

// WithBrowserStreamService sets the browser stream service for browser frame streaming.
func WithBrowserStreamService(s BrowserStreamService) Option {
	return func(srv *Server) {
		srv.browserStream = s
	}
}

// WithTunnelService sets the tunnel service.
func WithTunnelService(s TunnelService) Option {
	return func(srv *Server) {
		srv.tunnels = s
	}
}

// WithTunnelProxy sets the tunnel proxy handler.
func WithTunnelProxy(h *TunnelProxyHandler) Option {
	return func(srv *Server) {
		srv.tunnelProxy = h
	}
}

// New creates a new public API server.
func New(cfg Config, logger *zap.Logger, opts ...Option) *Server {
	srv := &Server{
		logger: logger,
	}

	// Apply options
	for _, opt := range opts {
		opt(srv)
	}

	// Create router
	r := chi.NewRouter()
	srv.router = r

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(RequestLogger(logger))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health endpoints (no auth required)
	r.Get("/health", srv.handleHealth)
	r.Get("/healthz", srv.handleHealth)

	// Documentation endpoints (no auth required)
	r.Get("/docs", srv.handleSwaggerUI)
	r.Get("/openapi.yaml", srv.handleOpenAPISpec)

	// Tunnel proxy (auth handled by handler - supports tunnel token or API key)
	if srv.tunnelProxy != nil {
		r.Route("/tunnels/{tunnelID}", func(r chi.Router) {
			// Handle root path: redirect if no trailing slash, otherwise proxy
			r.Get("/", func(w http.ResponseWriter, req *http.Request) {
				// Check if original request URL has trailing slash
				// req.RequestURI preserves the original path before chi routing
				path := strings.Split(req.RequestURI, "?")[0]
				if !strings.HasSuffix(path, "/") {
					// Redirect /tunnels/{tunnelID} to /tunnels/{tunnelID}/ for proper relative links
					tunnelID := chi.URLParam(req, "tunnelID")
					http.Redirect(w, req, "/tunnels/"+tunnelID+"/", http.StatusMovedPermanently)
					return
				}
				srv.tunnelProxy.ServeHTTP(w, req)
			})
			r.HandleFunc("/*", srv.tunnelProxy.ServeHTTP)
		})
	}

	// API v1 routes (auth required)
	r.Route("/api/v1", func(r chi.Router) {
		// Apply authentication middleware
		r.Use(srv.AuthMiddleware)

		// Sessions
		r.Route("/sessions", func(r chi.Router) {
			r.With(RequireScope("sessions:write")).Post("/", srv.handleCreateSession)
			r.With(RequireScope("sessions:read")).Get("/", srv.handleListSessions)
			r.With(RequireScope("sessions:read")).Get("/{sessionID}", srv.handleGetSession)
			r.With(RequireScope("sessions:write")).Post("/{sessionID}/suspend", srv.handleSuspendSession)
			r.With(RequireScope("sessions:write")).Post("/{sessionID}/resume", srv.handleResumeSession)
			r.With(RequireScope("sessions:write")).Delete("/{sessionID}", srv.handleTerminateSession)

			// Session tunnels
			r.With(RequireScope("tunnels:write")).Post("/{sessionID}/tunnels", srv.handleCreateTunnel)
			r.With(RequireScope("tunnels:read")).Get("/{sessionID}/tunnels", srv.handleListTunnels)
		})

		// Tasks
		r.Route("/tasks", func(r chi.Router) {
			r.With(RequireScope("tasks:write")).Post("/", srv.handleCreateTask)
			r.With(RequireScope("tasks:read")).Get("/", srv.handleListTasks)
			r.With(RequireScope("tasks:read")).Get("/{taskID}", srv.handleGetTask)
			r.With(RequireScope("tasks:write")).Post("/{taskID}/execute", srv.handleExecuteTask)
			r.With(RequireScope("tasks:write")).Post("/{taskID}/cancel", srv.handleCancelTask)
			r.With(RequireScope("tasks:write")).Post("/{taskID}/retry", srv.handleRetryTask)
			r.With(RequireScope("tasks:read")).Get("/{taskID}/logs", srv.handleGetTaskLogs)
		})

		// Runners
		r.Route("/runners", func(r chi.Router) {
			r.With(RequireScope("runners:read")).Get("/", srv.handleListRunners)
			r.With(RequireScope("runners:read")).Get("/{runnerID}", srv.handleGetRunner)
		})

		// Tunnels (get/close by ID)
		r.Route("/tunnels", func(r chi.Router) {
			r.With(RequireScope("tunnels:read")).Get("/{tunnelID}", srv.handleGetTunnel)
			r.With(RequireScope("tunnels:write")).Delete("/{tunnelID}", srv.handleCloseTunnel)
		})

		// Permissions
		r.Route("/permissions", func(r chi.Router) {
			r.With(RequireScope("permissions:read")).Get("/", srv.handleListPermissions)
			r.With(RequireScope("permissions:read")).Get("/{permissionID}", srv.handleGetPermission)
			r.With(RequireScope("permissions:write")).Post("/{permissionID}/approve", srv.handleApprovePermission)
			r.With(RequireScope("permissions:write")).Post("/{permissionID}/deny", srv.handleDenyPermission)
		})

		// Workspaces
		r.Route("/workspaces", func(r chi.Router) {
			r.With(RequireScope("workspaces:write")).Post("/", srv.handleCreateWorkspace)
			r.With(RequireScope("workspaces:read")).Get("/", srv.handleListWorkspaces)
			r.With(RequireScope("workspaces:read")).Get("/{workspaceID}", srv.handleGetWorkspace)
			r.With(RequireScope("workspaces:write")).Patch("/{workspaceID}", srv.handleUpdateWorkspace)
			r.With(RequireScope("workspaces:write")).Delete("/{workspaceID}", srv.handleDeleteWorkspace)
		})

		// WebSocket endpoints
		r.Route("/logs", func(r chi.Router) {
			r.With(RequireScope("tasks:read")).Get("/{taskID}/stream", srv.handleLogStream)
		})

		r.With(RequireScope("events:read")).Get("/events", srv.handleEventStream)

		// Browser streaming WebSocket endpoint
		r.Route("/streams", func(r chi.Router) {
			// WebSocket endpoint for browser frame streaming
			// Token-based auth is handled in the handler (query param ?token=xxx)
			r.With(RequireScope("streams:read")).Get("/{streamID}/ws", srv.handleBrowserStreamWS)
		})
	})

	srv.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return srv
}

// Router returns the underlying chi router for testing.
func (s *Server) Router() chi.Router {
	return s.router
}

// Start starts the API server.
func (s *Server) Start() error {
	s.logger.Info("starting public API server", zap.String("addr", s.server.Addr))
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("public api server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down public API server")
	return s.server.Shutdown(ctx)
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
