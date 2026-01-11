package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/tunnel"
)

// handleCreateTunnel handles POST /api/v1/sessions/{sessionID}/tunnels.
func (s *Server) handleCreateTunnel(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Tunnel service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	var req CreateTunnelOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	// Set session ID from URL path
	req.SessionID = sessionID

	// Validate required fields
	if req.Type == "" {
		req.Type = "http" // Default to HTTP tunnel
	}
	if req.LocalPort <= 0 || req.LocalPort > 65535 {
		WriteError(w, http.StatusBadRequest, "validation_error", "local_port must be between 1 and 65535")
		return
	}

	tun, err := s.tunnels.Create(r.Context(), req)
	if err != nil {
		handleTunnelError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, tun)
}

// handleListTunnels handles GET /api/v1/sessions/{sessionID}/tunnels.
func (s *Server) handleListTunnels(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Tunnel service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	tunnels, err := s.tunnels.ListBySession(r.Context(), sessionID)
	if err != nil {
		handleTunnelError(w, err)
		return
	}

	// Return as ListResult for consistency with other endpoints
	result := struct {
		Items []*tunnel.Tunnel `json:"items"`
	}{
		Items: tunnels,
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetTunnel handles GET /api/v1/tunnels/{tunnelID}.
func (s *Server) handleGetTunnel(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Tunnel service not configured")
		return
	}

	tunnelID := chi.URLParam(r, "tunnelID")
	if tunnelID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Tunnel ID is required")
		return
	}

	tun, err := s.tunnels.Get(r.Context(), tunnelID)
	if err != nil {
		handleTunnelError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, tun)
}

// handleCloseTunnel handles DELETE /api/v1/tunnels/{tunnelID}.
func (s *Server) handleCloseTunnel(w http.ResponseWriter, r *http.Request) {
	if s.tunnels == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Tunnel service not configured")
		return
	}

	tunnelID := chi.URLParam(r, "tunnelID")
	if tunnelID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Tunnel ID is required")
		return
	}

	if err := s.tunnels.Close(r.Context(), tunnelID); err != nil {
		handleTunnelError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTunnelError converts tunnel errors to appropriate HTTP responses.
func handleTunnelError(w http.ResponseWriter, err error) {
	switch err {
	case tunnel.ErrTunnelNotFound:
		WriteError(w, http.StatusNotFound, "not_found", "Tunnel not found")
	case tunnel.ErrTunnelClosed:
		WriteError(w, http.StatusGone, "tunnel_closed", "Tunnel is closed")
	case tunnel.ErrTunnelExpired:
		WriteError(w, http.StatusGone, "tunnel_expired", "Tunnel has expired")
	case tunnel.ErrInvalidTunnelType:
		WriteError(w, http.StatusBadRequest, "invalid_type", "Invalid tunnel type")
	case tunnel.ErrRunnerNotConnected:
		WriteError(w, http.StatusServiceUnavailable, "runner_not_connected", "Runner is not connected")
	default:
		// Check for specific error messages
		errMsg := err.Error()
		if strings.Contains(errMsg, "no runner attached") {
			WriteError(w, http.StatusBadRequest, "no_runner", "Session has no runner attached. Wait for runner to connect.")
			return
		}
		if strings.Contains(errMsg, "store not configured") || strings.Contains(errMsg, "tunnel manager not configured") {
			WriteError(w, http.StatusInternalServerError, "service_unavailable", "Tunnel service not properly configured")
			return
		}
		handleServiceError(w, err)
	}
}
