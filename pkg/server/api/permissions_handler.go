package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleListPermissions handles GET /api/v1/permissions.
func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	if s.permissions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Permission service not configured")
		return
	}

	opts := ListPermissionsOptions{
		Limit:     parseIntQuery(r, "limit", 50),
		Cursor:    r.URL.Query().Get("cursor"),
		SessionID: r.URL.Query().Get("session_id"),
		TaskID:    r.URL.Query().Get("task_id"),
		Status:    r.URL.Query()["status"],
		RiskLevel: r.URL.Query()["risk_level"],
	}

	result, err := s.permissions.List(r.Context(), opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetPermission handles GET /api/v1/permissions/{permissionID}.
func (s *Server) handleGetPermission(w http.ResponseWriter, r *http.Request) {
	if s.permissions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Permission service not configured")
		return
	}

	permissionID := chi.URLParam(r, "permissionID")
	if permissionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Permission ID is required")
		return
	}

	permission, err := s.permissions.Get(r.Context(), permissionID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, permission)
}

// handleApprovePermission handles POST /api/v1/permissions/{permissionID}/approve.
func (s *Server) handleApprovePermission(w http.ResponseWriter, r *http.Request) {
	if s.permissions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Permission service not configured")
		return
	}

	permissionID := chi.URLParam(r, "permissionID")
	if permissionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Permission ID is required")
		return
	}

	var opts ApproveOptions
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
			return
		}
	}

	if err := s.permissions.Approve(r.Context(), permissionID, opts); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDenyPermission handles POST /api/v1/permissions/{permissionID}/deny.
func (s *Server) handleDenyPermission(w http.ResponseWriter, r *http.Request) {
	if s.permissions == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Permission service not configured")
		return
	}

	permissionID := chi.URLParam(r, "permissionID")
	if permissionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Permission ID is required")
		return
	}

	var opts DenyOptions
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
			return
		}
	}

	if err := s.permissions.Deny(r.Context(), permissionID, opts); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
