package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/store"
)

// CreateRunnerTokenRequest is the request body for creating a runner token.
type CreateRunnerTokenRequest struct {
	PoolName  string            `json:"pool_name"`
	Labels    map[string]string `json:"labels,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

func (s *Server) handleCreateRunnerToken(w http.ResponseWriter, r *http.Request) {
	if s.runnerTokens == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner token service not configured")
		return
	}

	var req CreateRunnerTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	// Validate pool_name is required
	if strings.TrimSpace(req.PoolName) == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "pool_name is required")
		return
	}

	token, rawToken, err := s.runnerTokens.Create(r.Context(), CreateRunnerTokenOptions{
		PoolName:  req.PoolName,
		Labels:    req.Labels,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to create runner token", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create runner token")
		return
	}

	WriteJSON(w, http.StatusCreated, admintypes.CreatedRunnerToken{
		Token:    toRunnerTokenResponse(token),
		RawToken: rawToken,
	})
}

func (s *Server) handleListRunnerTokens(w http.ResponseWriter, r *http.Request) {
	if s.runnerTokens == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner token service not configured")
		return
	}

	query := r.URL.Query()
	opts := ListRunnerTokensOptions{
		Labels:   parseLabels(query.Get("labels")),
		Limit:    parseLimit(query.Get("limit")),
		Cursor:   query.Get("cursor"),
		PoolName: query.Get("pool_name"),
	}

	// Parse status filter (comma-separated)
	if status := query.Get("status"); status != "" {
		opts.Status = strings.Split(status, ",")
	}

	// Parse include_revoked flag
	if includeRevoked := query.Get("include_revoked"); includeRevoked != "" {
		opts.IncludeRevoked, _ = strconv.ParseBool(includeRevoked)
	}

	result, err := s.runnerTokens.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list runner tokens", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list runner tokens")
		return
	}

	WriteJSON(w, http.StatusOK, toListResponse(result, toRunnerTokenResponse))
}

func (s *Server) handleGetRunnerToken(w http.ResponseWriter, r *http.Request) {
	if s.runnerTokens == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner token service not configured")
		return
	}

	tokenID := chi.URLParam(r, "tokenID")
	if tokenID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "token ID is required")
		return
	}

	token, err := s.runnerTokens.Get(r.Context(), tokenID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "runner token not found")
			return
		}
		s.logger.Error("failed to get runner token", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get runner token")
		return
	}

	WriteJSON(w, http.StatusOK, toRunnerTokenResponse(token))
}

// RevokeRunnerTokenRequest is the request body for revoking a runner token.
type RevokeRunnerTokenRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handleRevokeRunnerToken(w http.ResponseWriter, r *http.Request) {
	if s.runnerTokens == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner token service not configured")
		return
	}

	tokenID := chi.URLParam(r, "tokenID")
	if tokenID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "token ID is required")
		return
	}

	var req RevokeRunnerTokenRequest
	// Body is optional for revoke
	_ = json.NewDecoder(r.Body).Decode(&req)

	err := s.runnerTokens.Revoke(r.Context(), tokenID, req.Reason)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "runner token not found")
			return
		}
		s.logger.Error("failed to revoke runner token", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to revoke runner token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateRunnerToken(w http.ResponseWriter, r *http.Request) {
	if s.runnerTokens == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "runner token service not configured")
		return
	}

	tokenID := chi.URLParam(r, "tokenID")
	if tokenID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "token ID is required")
		return
	}

	token, rawToken, err := s.runnerTokens.Rotate(r.Context(), tokenID)
	if err != nil {
		if err == store.ErrNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "runner token not found")
			return
		}
		if IsValidation(err) {
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		s.logger.Error("failed to rotate runner token", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to rotate runner token")
		return
	}

	WriteJSON(w, http.StatusOK, admintypes.CreatedRunnerToken{
		Token:    toRunnerTokenResponse(token),
		RawToken: rawToken,
	})
}
