package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// handleCreateSession handles POST /api/v1/sessions.
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session service not configured")
		return
	}

	var req CreateSessionOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	// Validate required fields
	if req.Agent == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "Agent is required")
		return
	}

	session, err := s.sessions.Create(r.Context(), req)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, session)
}

// handleListSessions handles GET /api/v1/sessions.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session service not configured")
		return
	}

	opts := ListSessionsOptions{
		Limit:         parseIntQuery(r, "limit", 50),
		Cursor:        r.URL.Query().Get("cursor"),
		Status:        r.URL.Query()["status"],
		Agent:         r.URL.Query().Get("agent"),
		LifecycleMode: r.URL.Query().Get("lifecycle_mode"),
		Labels:        parseLabelsQuery(r),
	}

	result, err := s.sessions.List(r.Context(), opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetSession handles GET /api/v1/sessions/{sessionID}.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	session, err := s.sessions.Get(r.Context(), sessionID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, session)
}

// handleSuspendSession handles POST /api/v1/sessions/{sessionID}/suspend.
func (s *Server) handleSuspendSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	if err := s.sessions.Suspend(r.Context(), sessionID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleResumeSession handles POST /api/v1/sessions/{sessionID}/resume.
func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	if err := s.sessions.Resume(r.Context(), sessionID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTerminateSession handles DELETE /api/v1/sessions/{sessionID}.
func (s *Server) handleTerminateSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Session service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Session ID is required")
		return
	}

	if err := s.sessions.Terminate(r.Context(), sessionID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleServiceError converts service errors to HTTP responses.
func handleServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "Resource not found")
		return
	}

	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		WriteError(w, http.StatusBadRequest, "validation_error", validationErr.Error())
		return
	}

	var invalidStateErr *InvalidStateError
	if errors.As(err, &invalidStateErr) {
		WriteError(w, http.StatusConflict, "invalid_state", invalidStateErr.Error())
		return
	}

	var notAuthorizedErr *NotAuthorizedError
	if errors.As(err, &notAuthorizedErr) {
		WriteError(w, http.StatusForbidden, "forbidden", notAuthorizedErr.Error())
		return
	}

	var maxRetriesErr *MaxRetriesExceededError
	if errors.As(err, &maxRetriesErr) {
		WriteError(w, http.StatusConflict, "max_retries_exceeded", maxRetriesErr.Error())
		return
	}

	WriteError(w, http.StatusInternalServerError, "internal_error", "Internal server error")
}

// parseIntQuery parses an integer query parameter with a default value.
//
//nolint:unparam // key parameter is designed for reuse with different query params
func parseIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	if n, err := strconv.Atoi(val); err == nil && n > 0 {
		return n
	}
	return defaultVal
}

// parseLabelsQuery parses label query parameters in the format labels[key]=value.
func parseLabelsQuery(r *http.Request) map[string]string {
	labels := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(key) > 7 && key[:7] == "labels[" && key[len(key)-1] == ']' {
			labelKey := key[7 : len(key)-1]
			if len(values) > 0 {
				labels[labelKey] = values[0]
			}
		}
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}
