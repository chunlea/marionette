package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// SpawnRunnerRequest is the request body for spawning a runner.
type SpawnRunnerRequest struct {
	Name             string            `json:"name,omitempty"`
	ProviderConfigID string            `json:"provider_config_id"`
	ProfileID        string            `json:"profile_id,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

func (s *Server) handleSpawnRunner(w http.ResponseWriter, r *http.Request) {
	if s.runners == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner service not configured")
		return
	}

	var req SpawnRunnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	runner, err := s.runners.Spawn(r.Context(), SpawnRunnerOptions{
		Name:             req.Name,
		ProviderConfigID: req.ProviderConfigID,
		ProfileID:        req.ProfileID,
		Labels:           req.Labels,
	})
	if err != nil {
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to spawn runner", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to spawn runner")
		return
	}

	WriteJSON(w, http.StatusCreated, toRunnerResponse(runner))
}

func (s *Server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	if s.runners == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner service not configured")
		return
	}

	// Parse status filter (comma-separated)
	var status []string
	if statusParam := r.URL.Query().Get("status"); statusParam != "" {
		status = strings.Split(statusParam, ",")
	}

	opts := ListRunnersOptions{
		Status:   status,
		PoolName: r.URL.Query().Get("pool_name"),
		Labels:   parseLabels(r.URL.Query().Get("labels")),
		Limit:    parseLimit(r.URL.Query().Get("limit")),
		Cursor:   r.URL.Query().Get("cursor"),
	}

	result, err := s.runners.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list runners", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list runners")
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toRunnerResponse))
}

func (s *Server) handleGetRunner(w http.ResponseWriter, r *http.Request) {
	if s.runners == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner service not configured")
		return
	}

	runnerID := chi.URLParam(r, "runnerID")
	if runnerID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "runner ID is required")
		return
	}

	runner, err := s.runners.Get(r.Context(), runnerID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "runner not found")
			return
		}
		s.logger.Error("failed to get runner", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get runner")
		return
	}

	WriteJSON(w, http.StatusOK, toRunnerResponse(runner))
}

func (s *Server) handleDestroyRunner(w http.ResponseWriter, r *http.Request) {
	if s.runners == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner service not configured")
		return
	}

	runnerID := chi.URLParam(r, "runnerID")
	if runnerID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "runner ID is required")
		return
	}

	err := s.runners.Destroy(r.Context(), runnerID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "runner not found")
			return
		}
		if IsInvalidState(err) {
			WriteError(w, http.StatusConflict, "invalid_state", err.Error())
			return
		}
		s.logger.Error("failed to destroy runner", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to destroy runner")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
