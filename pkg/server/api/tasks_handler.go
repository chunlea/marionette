package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// handleCreateTask handles POST /api/v1/tasks.
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	var req CreateTaskOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	// Validate required fields
	if req.SessionID == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "session_id is required")
		return
	}
	if req.Prompt == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "prompt is required")
		return
	}

	task, err := s.tasks.Create(r.Context(), req)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, toTaskResponse(task))
}

// handleListTasks handles GET /api/v1/tasks.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	opts := ListTasksOptions{
		Limit:     parseIntQuery(r, "limit", 50),
		Cursor:    r.URL.Query().Get("cursor"),
		SessionID: r.URL.Query().Get("session_id"),
		Status:    r.URL.Query()["status"],
	}

	result, err := s.tasks.List(r.Context(), opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toTaskResponse))
}

// handleGetTask handles GET /api/v1/tasks/{taskID}.
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Task ID is required")
		return
	}

	task, err := s.tasks.Get(r.Context(), taskID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toTaskResponse(task))
}

// handleExecuteTask handles POST /api/v1/tasks/{taskID}/execute.
func (s *Server) handleExecuteTask(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Task ID is required")
		return
	}

	if err := s.tasks.Execute(r.Context(), taskID); err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusAccepted, apitypes.TaskExecutionAccepted{Status: "executing"})
}

// handleCancelTask handles POST /api/v1/tasks/{taskID}/cancel.
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Task ID is required")
		return
	}

	if err := s.tasks.Cancel(r.Context(), taskID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRetryTask handles POST /api/v1/tasks/{taskID}/retry.
func (s *Server) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Task ID is required")
		return
	}

	if err := s.tasks.Retry(r.Context(), taskID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetTaskLogs handles GET /api/v1/tasks/{taskID}/logs.
// handleListTaskRuns handles GET /api/v1/tasks/{taskID}/runs.
func (s *Server) handleListTaskRuns(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Task ID is required")
		return
	}

	opts := ListTaskRunsOptions{
		Limit:  parseIntQuery(r, "limit", 50),
		Cursor: r.URL.Query().Get("cursor"),
		Status: r.URL.Query()["status"],
	}

	result, err := s.tasks.ListRuns(r.Context(), taskID, opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toTaskRunResponse))
}

func (s *Server) handleGetTaskLogs(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Task service not configured")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Task ID is required")
		return
	}

	opts := GetLogsOptions{
		Limit:  parseIntQuery(r, "limit", 100),
		Cursor: r.URL.Query().Get("cursor"),
		Level:  r.URL.Query()["level"],
		Stream: r.URL.Query()["stream"],
	}

	result, err := s.tasks.GetLogs(r.Context(), taskID, opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toLogResponse))
}
