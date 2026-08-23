package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleCreateScheduledTask handles POST /api/v1/scheduled-tasks.
func (s *Server) handleCreateScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	var req CreateScheduledTaskOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	// Validate required fields
	if req.SessionID == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "session_id is required")
		return
	}
	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "name is required")
		return
	}
	if req.CronExpression == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "cron_expression is required")
		return
	}
	if req.PromptTemplate == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "prompt_template is required")
		return
	}

	task, err := s.scheduledTasks.Create(r.Context(), req)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, toScheduledTaskResponse(task))
}

// handleListScheduledTasks handles GET /api/v1/scheduled-tasks.
func (s *Server) handleListScheduledTasks(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	opts := ListScheduledTasksOptions{
		Limit:     parseIntQuery(r, "limit", 50),
		Cursor:    r.URL.Query().Get("cursor"),
		SessionID: r.URL.Query().Get("session_id"),
		Status:    r.URL.Query()["status"],
	}

	result, err := s.scheduledTasks.List(r.Context(), opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toScheduledTaskResponse))
}

// handleGetScheduledTask handles GET /api/v1/scheduled-tasks/{scheduledTaskID}.
func (s *Server) handleGetScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	taskID := chi.URLParam(r, "scheduledTaskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Scheduled task ID is required")
		return
	}

	task, err := s.scheduledTasks.Get(r.Context(), taskID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toScheduledTaskResponse(task))
}

// handleUpdateScheduledTask handles PATCH /api/v1/scheduled-tasks/{scheduledTaskID}.
func (s *Server) handleUpdateScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	taskID := chi.URLParam(r, "scheduledTaskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Scheduled task ID is required")
		return
	}

	var req UpdateScheduledTaskOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	task, err := s.scheduledTasks.Update(r.Context(), taskID, req)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toScheduledTaskResponse(task))
}

// handleDeleteScheduledTask handles DELETE /api/v1/scheduled-tasks/{scheduledTaskID}.
func (s *Server) handleDeleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	taskID := chi.URLParam(r, "scheduledTaskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Scheduled task ID is required")
		return
	}

	if err := s.scheduledTasks.Delete(r.Context(), taskID); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handlePauseScheduledTask handles POST /api/v1/scheduled-tasks/{scheduledTaskID}/pause.
func (s *Server) handlePauseScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	taskID := chi.URLParam(r, "scheduledTaskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Scheduled task ID is required")
		return
	}

	if err := s.scheduledTasks.Pause(r.Context(), taskID); err != nil {
		handleServiceError(w, err)
		return
	}

	// Return the updated task
	task, err := s.scheduledTasks.Get(r.Context(), taskID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toScheduledTaskResponse(task))
}

// handleResumeScheduledTask handles POST /api/v1/scheduled-tasks/{scheduledTaskID}/resume.
func (s *Server) handleResumeScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	taskID := chi.URLParam(r, "scheduledTaskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Scheduled task ID is required")
		return
	}

	if err := s.scheduledTasks.Resume(r.Context(), taskID); err != nil {
		handleServiceError(w, err)
		return
	}

	// Return the updated task
	task, err := s.scheduledTasks.Get(r.Context(), taskID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toScheduledTaskResponse(task))
}

// handleTriggerScheduledTask handles POST /api/v1/scheduled-tasks/{scheduledTaskID}/trigger.
func (s *Server) handleTriggerScheduledTask(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTasks == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Scheduled task service not configured")
		return
	}

	taskID := chi.URLParam(r, "scheduledTaskID")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Scheduled task ID is required")
		return
	}

	createdTask, err := s.scheduledTasks.Trigger(r.Context(), taskID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, toTaskResponse(createdTask))
}
