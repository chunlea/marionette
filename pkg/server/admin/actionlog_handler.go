package admin

import (
	"errors"
	"net/http"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/go-chi/chi/v5"
)

// handleListActionLogs handles GET /admin/api/v1/action-logs.
func (s *Server) handleListActionLogs(w http.ResponseWriter, r *http.Request) {
	if s.actionLogs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "action log service not configured")
		return
	}

	opts := ListActionLogsOptions{
		Limit:  parseLimit(r.URL.Query().Get("limit")),
		Cursor: r.URL.Query().Get("cursor"),
	}

	result, err := s.actionLogs.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list action logs", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list action logs")
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetActionLog handles GET /admin/api/v1/action-logs/{logID}.
func (s *Server) handleGetActionLog(w http.ResponseWriter, r *http.Request) {
	if s.actionLogs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "action log service not configured")
		return
	}

	logID := chi.URLParam(r, "logID")
	if logID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "log ID is required")
		return
	}

	log, err := s.actionLogs.Get(r.Context(), logID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "action log not found")
			return
		}
		s.logger.Error("failed to get action log", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get action log")
		return
	}

	WriteJSON(w, http.StatusOK, log)
}
