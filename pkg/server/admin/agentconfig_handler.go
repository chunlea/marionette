package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// CreateAgentConfigRequest is the request body for creating an agent config.
type CreateAgentConfigRequest struct {
	Name      string            `json:"name"`
	Agent     string            `json:"agent"`
	APIKey    string            `json:"api_key"`
	Model     string            `json:"model,omitempty"`
	BaseURL   string            `json:"base_url,omitempty"`
	Extra     map[string]any    `json:"extra,omitempty"`
	IsDefault bool              `json:"is_default,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

func (s *Server) handleCreateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if s.agentConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "agent config service not configured")
		return
	}

	var req CreateAgentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	config, err := s.agentConfigs.Create(r.Context(), CreateAgentConfigOptions(req))
	if err != nil {
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to create agent config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create agent config")
		return
	}

	WriteJSON(w, http.StatusCreated, config)
}

func (s *Server) handleListAgentConfigs(w http.ResponseWriter, r *http.Request) {
	if s.agentConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "agent config service not configured")
		return
	}

	opts := ListAgentConfigsOptions{
		Agent:  r.URL.Query().Get("agent"),
		Labels: parseLabels(r.URL.Query().Get("labels")),
		Limit:  parseLimit(r.URL.Query().Get("limit")),
		Cursor: r.URL.Query().Get("cursor"),
	}

	result, err := s.agentConfigs.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list agent configs", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list agent configs")
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	if s.agentConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "agent config service not configured")
		return
	}

	configID := chi.URLParam(r, "configID")
	if configID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "config ID is required")
		return
	}

	config, err := s.agentConfigs.Get(r.Context(), configID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "agent config not found")
			return
		}
		s.logger.Error("failed to get agent config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get agent config")
		return
	}

	WriteJSON(w, http.StatusOK, config)
}

// UpdateAgentConfigRequest is the request body for updating an agent config.
type UpdateAgentConfigRequest struct {
	Name      *string            `json:"name,omitempty"`
	APIKey    *string            `json:"api_key,omitempty"`
	Model     *string            `json:"model,omitempty"`
	BaseURL   *string            `json:"base_url,omitempty"`
	Extra     *map[string]any    `json:"extra,omitempty"`
	IsDefault *bool              `json:"is_default,omitempty"`
	Labels    *map[string]string `json:"labels,omitempty"`
}

func (s *Server) handleUpdateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if s.agentConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "agent config service not configured")
		return
	}

	configID := chi.URLParam(r, "configID")
	if configID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "config ID is required")
		return
	}

	var req UpdateAgentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	config, err := s.agentConfigs.Update(r.Context(), configID, UpdateAgentConfigOptions(req))
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "agent config not found")
			return
		}
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to update agent config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update agent config")
		return
	}

	WriteJSON(w, http.StatusOK, config)
}

func (s *Server) handleDeleteAgentConfig(w http.ResponseWriter, r *http.Request) {
	if s.agentConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "agent config service not configured")
		return
	}

	configID := chi.URLParam(r, "configID")
	if configID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "config ID is required")
		return
	}

	err := s.agentConfigs.Delete(r.Context(), configID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "agent config not found")
			return
		}
		s.logger.Error("failed to delete agent config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete agent config")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
