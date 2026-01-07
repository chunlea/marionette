package admin

import (
	"context"
	"encoding/json"
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
	router chi.Router
	logger *zap.Logger

	// Services
	apiKeys          APIKeyService
	agentConfigs     AgentConfigService
	providerConfigs  ProviderConfigService
	runners          RunnerAdminService
	sessionActivator SessionActivator
	actionLogs       ActionLogService

	// Basic auth credentials
	username string
	password string
}

// Config holds configuration for the admin server.
type Config struct {
	Host     string
	Port     int
	Username string // Basic auth username
	Password string // Basic auth password
}

// Option is a functional option for configuring the server.
type Option func(*Server)

// WithAPIKeyService sets the API key service.
func WithAPIKeyService(s APIKeyService) Option {
	return func(srv *Server) {
		srv.apiKeys = s
	}
}

// WithAgentConfigService sets the agent config service.
func WithAgentConfigService(s AgentConfigService) Option {
	return func(srv *Server) {
		srv.agentConfigs = s
	}
}

// WithProviderConfigService sets the provider config service.
func WithProviderConfigService(s ProviderConfigService) Option {
	return func(srv *Server) {
		srv.providerConfigs = s
	}
}

// WithRunnerAdminService sets the runner admin service.
func WithRunnerAdminService(s RunnerAdminService) Option {
	return func(srv *Server) {
		srv.runners = s
	}
}

// WithSessionActivator sets the session activator (for testing).
func WithSessionActivator(s SessionActivator) Option {
	return func(srv *Server) {
		srv.sessionActivator = s
	}
}

// WithActionLogService sets the action log service.
func WithActionLogService(s ActionLogService) Option {
	return func(srv *Server) {
		srv.actionLogs = s
	}
}

// New creates a new admin server.
func New(cfg Config, logger *zap.Logger, opts ...Option) *Server {
	srv := &Server{
		logger:   logger,
		username: cfg.Username,
		password: cfg.Password,
	}

	// Apply options
	for _, opt := range opts {
		opt(srv)
	}

	r := chi.NewRouter()
	srv.router = r

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Health endpoints (no auth required)
	r.Get("/health", healthHandler)
	r.Get("/healthz", healthHandler)

	// Documentation endpoints (no auth required)
	r.Get("/docs", srv.handleSwaggerUI)
	r.Get("/openapi.yaml", srv.handleOpenAPISpec)

	// Status endpoint - returns all service statuses
	r.Get("/api/status", statusHandler)

	// Admin API routes (with basic auth)
	r.Route("/admin/api/v1", func(r chi.Router) {
		// Apply basic auth middleware if credentials are set
		if srv.username != "" && srv.password != "" {
			r.Use(srv.BasicAuthMiddleware)
		}

		// API Keys
		r.Route("/keys", func(r chi.Router) {
			r.Post("/", srv.handleCreateAPIKey)
			r.Get("/", srv.handleListAPIKeys)
			r.Get("/{keyID}", srv.handleGetAPIKey)
			r.Delete("/{keyID}", srv.handleRevokeAPIKey)
		})

		// Agent Configs
		r.Route("/agent-configs", func(r chi.Router) {
			r.Post("/", srv.handleCreateAgentConfig)
			r.Get("/", srv.handleListAgentConfigs)
			r.Get("/{configID}", srv.handleGetAgentConfig)
			r.Put("/{configID}", srv.handleUpdateAgentConfig)
			r.Delete("/{configID}", srv.handleDeleteAgentConfig)
		})

		// Provider Configs
		r.Route("/provider-configs", func(r chi.Router) {
			r.Post("/", srv.handleCreateProviderConfig)
			r.Get("/", srv.handleListProviderConfigs)
			r.Get("/{configID}", srv.handleGetProviderConfig)
			r.Put("/{configID}", srv.handleUpdateProviderConfig)
			r.Delete("/{configID}", srv.handleDeleteProviderConfig)
		})

		// Runners
		r.Route("/runners", func(r chi.Router) {
			r.Post("/spawn", srv.handleSpawnRunner)
			r.Get("/", srv.handleListRunners)
			r.Get("/{runnerID}", srv.handleGetRunner)
			r.Delete("/{runnerID}", srv.handleDestroyRunner)
		})

		// Sessions (for testing)
		r.Route("/sessions", func(r chi.Router) {
			r.Post("/{sessionID}/activate", srv.handleActivateSession)
			r.Post("/{sessionID}/suspend", srv.handleSuspendSession)
		})

		// Action Logs
		r.Route("/action-logs", func(r chi.Router) {
			r.Get("/", srv.handleListActionLogs)
			r.Get("/{logID}", srv.handleGetActionLog)
		})
	})

	// Serve embedded frontend for all other routes
	r.Handle("/*", staticHandler())

	srv.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv
}

// Router returns the chi router for testing.
func (s *Server) Router() chi.Router {
	return s.router
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

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes an error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Code: code, Message: message})
}

// handleActivateSession handles POST /admin/api/v1/sessions/{sessionID}/activate.
func (s *Server) handleActivateSession(w http.ResponseWriter, r *http.Request) {
	if s.sessionActivator == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session activator not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	// Parse request body for runner_id
	var req struct {
		RunnerID string `json:"runner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	if req.RunnerID == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "runner_id is required")
		return
	}

	if err := s.sessionActivator.Activate(r.Context(), sessionID, req.RunnerID); err != nil {
		s.logger.Error("failed to activate session", zap.Error(err))
		WriteError(w, http.StatusInternalServerError, "activation_failed", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

// handleSuspendSession handles POST /admin/api/v1/sessions/{sessionID}/suspend.
func (s *Server) handleSuspendSession(w http.ResponseWriter, r *http.Request) {
	if s.sessionActivator == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session activator not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	// Parse request body for strategy (optional)
	var req struct {
		Strategy string `json:"strategy"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// Default strategy is terminate
	if req.Strategy == "" {
		req.Strategy = "terminate"
	}

	if err := s.sessionActivator.Suspend(r.Context(), sessionID, req.Strategy); err != nil {
		s.logger.Error("failed to suspend session", zap.Error(err))
		WriteError(w, http.StatusInternalServerError, "suspend_failed", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}
