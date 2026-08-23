package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// CreateProfileRequest is the request body for creating a profile.
type CreateProfileRequest struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	ProviderConfigID string            `json:"provider_config_id,omitempty"`
	Resources        map[string]any    `json:"resources,omitempty"`
	Network          map[string]any    `json:"network,omitempty"`
	InitScript       string            `json:"init_script,omitempty"`
	CleanupScript    string            `json:"cleanup_script,omitempty"`
	Tunnels          []map[string]any  `json:"tunnels,omitempty"`
	Selector         map[string]any    `json:"selector,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty"`
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "profile service not configured")
		return
	}

	var req CreateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	profile, err := s.profiles.Create(r.Context(), CreateProfileOptions(req))
	if err != nil {
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to create profile", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create profile")
		return
	}

	WriteJSON(w, http.StatusCreated, toProfileResponse(profile))
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "profile service not configured")
		return
	}

	opts := ListProfilesOptions{
		ProviderConfigID: r.URL.Query().Get("provider_config_id"),
		IncludeBuiltin:   r.URL.Query().Get("include_builtin") == "true",
		Labels:           parseLabels(r.URL.Query().Get("labels")),
		Limit:            parseLimit(r.URL.Query().Get("limit")),
		Cursor:           r.URL.Query().Get("cursor"),
	}

	result, err := s.profiles.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list profiles", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list profiles")
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toProfileResponse))
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "profile service not configured")
		return
	}

	profileID := chi.URLParam(r, "profileID")
	if profileID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "profile ID is required")
		return
	}

	profile, err := s.profiles.Get(r.Context(), profileID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "profile not found")
			return
		}
		s.logger.Error("failed to get profile", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get profile")
		return
	}

	WriteJSON(w, http.StatusOK, toProfileResponse(profile))
}

// UpdateProfileRequest is the request body for updating a profile.
type UpdateProfileRequest struct {
	Name             *string            `json:"name,omitempty"`
	Description      *string            `json:"description,omitempty"`
	ProviderConfigID *string            `json:"provider_config_id,omitempty"`
	Resources        *map[string]any    `json:"resources,omitempty"`
	Network          *map[string]any    `json:"network,omitempty"`
	InitScript       *string            `json:"init_script,omitempty"`
	CleanupScript    *string            `json:"cleanup_script,omitempty"`
	Tunnels          *[]map[string]any  `json:"tunnels,omitempty"`
	Selector         *map[string]any    `json:"selector,omitempty"`
	Labels           *map[string]string `json:"labels,omitempty"`
	Annotations      *map[string]string `json:"annotations,omitempty"`
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "profile service not configured")
		return
	}

	profileID := chi.URLParam(r, "profileID")
	if profileID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "profile ID is required")
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	profile, err := s.profiles.Update(r.Context(), profileID, UpdateProfileOptions(req))
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "profile not found")
			return
		}
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to update profile", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update profile")
		return
	}

	WriteJSON(w, http.StatusOK, toProfileResponse(profile))
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "profile service not configured")
		return
	}

	profileID := chi.URLParam(r, "profileID")
	if profileID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "profile ID is required")
		return
	}

	err := s.profiles.Delete(r.Context(), profileID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "profile not found")
			return
		}
		s.logger.Error("failed to delete profile", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete profile")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
