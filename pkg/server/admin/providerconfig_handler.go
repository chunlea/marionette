package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// CreateProviderConfigRequest is the request body for creating a provider config.
type CreateProviderConfigRequest struct {
	Name          string            `json:"name"`
	Provider      string            `json:"provider"`
	Config        map[string]any    `json:"config,omitempty"`
	SuspendConfig map[string]any    `json:"suspend_config,omitempty"`
	IsDefault     bool              `json:"is_default,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

func (s *Server) handleCreateProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.providerConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "provider config service not configured")
		return
	}

	var req CreateProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	config, err := s.providerConfigs.Create(r.Context(), CreateProviderConfigOptions(req))
	if err != nil {
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to create provider config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create provider config")
		return
	}

	WriteJSON(w, http.StatusCreated, config)
}

func (s *Server) handleListProviderConfigs(w http.ResponseWriter, r *http.Request) {
	if s.providerConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "provider config service not configured")
		return
	}

	opts := ListProviderConfigsOptions{
		Provider: r.URL.Query().Get("provider"),
		Labels:   parseLabels(r.URL.Query().Get("labels")),
		Limit:    parseLimit(r.URL.Query().Get("limit")),
		Cursor:   r.URL.Query().Get("cursor"),
	}

	result, err := s.providerConfigs.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list provider configs", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list provider configs")
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.providerConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "provider config service not configured")
		return
	}

	configID := chi.URLParam(r, "configID")
	if configID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "config ID is required")
		return
	}

	config, err := s.providerConfigs.Get(r.Context(), configID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "provider config not found")
			return
		}
		s.logger.Error("failed to get provider config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get provider config")
		return
	}

	WriteJSON(w, http.StatusOK, config)
}

// UpdateProviderConfigRequest is the request body for updating a provider config.
type UpdateProviderConfigRequest struct {
	Name          *string            `json:"name,omitempty"`
	Config        *map[string]any    `json:"config,omitempty"`
	SuspendConfig *map[string]any    `json:"suspend_config,omitempty"`
	IsDefault     *bool              `json:"is_default,omitempty"`
	Labels        *map[string]string `json:"labels,omitempty"`
}

func (s *Server) handleUpdateProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.providerConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "provider config service not configured")
		return
	}

	configID := chi.URLParam(r, "configID")
	if configID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "config ID is required")
		return
	}

	var req UpdateProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	config, err := s.providerConfigs.Update(r.Context(), configID, UpdateProviderConfigOptions(req))
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "provider config not found")
			return
		}
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to update provider config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update provider config")
		return
	}

	WriteJSON(w, http.StatusOK, config)
}

func (s *Server) handleDeleteProviderConfig(w http.ResponseWriter, r *http.Request) {
	if s.providerConfigs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "provider config service not configured")
		return
	}

	configID := chi.URLParam(r, "configID")
	if configID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "config ID is required")
		return
	}

	err := s.providerConfigs.Delete(r.Context(), configID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "provider config not found")
			return
		}
		s.logger.Error("failed to delete provider config", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete provider config")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
