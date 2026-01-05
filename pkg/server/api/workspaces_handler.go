package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// handleCreateWorkspace handles POST /api/v1/workspaces.
func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaces == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Workspace service not configured")
		return
	}

	var req CreateWorkspaceOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	workspace, err := s.workspaces.Create(r.Context(), req)
	if err != nil {
		handleWorkspaceError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, workspace)
}

// handleListWorkspaces handles GET /api/v1/workspaces.
func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	if s.workspaces == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Workspace service not configured")
		return
	}

	opts := ListWorkspacesOptions{
		Limit:  parseIntQuery(r, "limit", 50),
		Cursor: r.URL.Query().Get("cursor"),
	}

	result, err := s.workspaces.List(r.Context(), opts)
	if err != nil {
		handleWorkspaceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetWorkspace handles GET /api/v1/workspaces/{workspaceID}.
func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaces == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Workspace service not configured")
		return
	}

	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Workspace ID is required")
		return
	}

	workspace, err := s.workspaces.Get(r.Context(), workspaceID)
	if err != nil {
		handleWorkspaceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, workspace)
}

// handleUpdateWorkspace handles PATCH /api/v1/workspaces/{workspaceID}.
func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaces == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Workspace service not configured")
		return
	}

	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Workspace ID is required")
		return
	}

	var req UpdateWorkspaceOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	workspace, err := s.workspaces.Update(r.Context(), workspaceID, req)
	if err != nil {
		handleWorkspaceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, workspace)
}

// handleDeleteWorkspace handles DELETE /api/v1/workspaces/{workspaceID}.
func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaces == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Workspace service not configured")
		return
	}

	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Workspace ID is required")
		return
	}

	if err := s.workspaces.Delete(r.Context(), workspaceID); err != nil {
		handleWorkspaceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleWorkspaceError converts workspace errors to HTTP responses.
func handleWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrWorkspaceNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "Workspace not found")
	case errors.Is(err, core.ErrWorkspaceDeleted):
		WriteError(w, http.StatusGone, "deleted", "Workspace has been deleted")
	case errors.Is(err, core.ErrWorkspaceInUse):
		WriteError(w, http.StatusConflict, "in_use", "Workspace is in use by a session")
	case errors.Is(err, core.ErrWorkspaceAlreadyExists):
		WriteError(w, http.StatusConflict, "already_exists", "Workspace already exists")
	case errors.Is(err, core.ErrInvalidWorkspaceName):
		WriteError(w, http.StatusBadRequest, "invalid_name", "Invalid workspace name")
	case errors.Is(err, store.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "Workspace not found")
	default:
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
