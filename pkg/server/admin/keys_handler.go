package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// CreateAPIKeyRequest is the request body for creating an API key.
type CreateAPIKeyRequest struct {
	Name        string            `json:"name"`
	Scopes      []string          `json:"scopes,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
}

// CreateAPIKeyResponse is the response for creating an API key.
type CreateAPIKeyResponse struct {
	Key      *store.APIKey `json:"key"`
	RawToken string        `json:"raw_token"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.apiKeys == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "API key service not configured")
		return
	}

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	key, rawToken, err := s.apiKeys.Create(r.Context(), CreateAPIKeyOptions(req))
	if err != nil {
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to create API key", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create API key")
		return
	}

	WriteJSON(w, http.StatusCreated, CreateAPIKeyResponse{
		Key:      key,
		RawToken: rawToken,
	})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if s.apiKeys == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "API key service not configured")
		return
	}

	opts := ListAPIKeysOptions{
		Labels: parseLabels(r.URL.Query().Get("labels")),
		Limit:  parseLimit(r.URL.Query().Get("limit")),
		Cursor: r.URL.Query().Get("cursor"),
	}

	result, err := s.apiKeys.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list API keys", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list API keys")
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.apiKeys == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "API key service not configured")
		return
	}

	keyID := chi.URLParam(r, "keyID")
	if keyID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "key ID is required")
		return
	}

	key, err := s.apiKeys.Get(r.Context(), keyID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "API key not found")
			return
		}
		s.logger.Error("failed to get API key", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get API key")
		return
	}

	WriteJSON(w, http.StatusOK, key)
}

// RevokeAPIKeyRequest is the request body for revoking an API key.
type RevokeAPIKeyRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.apiKeys == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "API key service not configured")
		return
	}

	keyID := chi.URLParam(r, "keyID")
	if keyID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "key ID is required")
		return
	}

	var req RevokeAPIKeyRequest
	// Body is optional for revoke
	_ = json.NewDecoder(r.Body).Decode(&req)

	err := s.apiKeys.Revoke(r.Context(), keyID, req.Reason)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "API key not found")
			return
		}
		s.logger.Error("failed to revoke API key", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to revoke API key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
