package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/go-chi/chi/v5"
)

// handleListActionLogs handles GET /admin/api/v1/action-logs.
func (s *Server) handleListActionLogs(w http.ResponseWriter, r *http.Request) {
	if s.actionLogs == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "action log service not configured")
		return
	}

	q := r.URL.Query()
	opts := ListActionLogsOptions{
		// Pagination
		Limit:  parseLimit(q.Get("limit")),
		Cursor: q.Get("cursor"),

		// Actor filters
		ActorType: q.Get("actor_type"),
		ActorID:   q.Get("actor_id"),

		// Action filters
		Action:       q.Get("action"),
		ActionPrefix: q.Get("action_prefix"),

		// Resource filters
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),

		// Context filters
		SessionID: q.Get("session_id"),
		TaskID:    q.Get("task_id"),
	}

	// Parse success filter
	if successStr := q.Get("success"); successStr != "" {
		success := successStr == "true"
		opts.Success = &success
	}

	// Parse time range filters
	if fromStr := q.Get("from"); fromStr != "" {
		from, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_parameter", "invalid 'from' time format, use RFC3339")
			return
		}
		opts.From = &from
	}
	if toStr := q.Get("to"); toStr != "" {
		to, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_parameter", "invalid 'to' time format, use RFC3339")
			return
		}
		opts.To = &to
	}

	result, err := s.actionLogs.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list action logs", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list action logs")
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toActionLogResponse))
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

	WriteJSON(w, http.StatusOK, toActionLogResponse(log))
}
