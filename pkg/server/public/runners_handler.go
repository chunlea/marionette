package public

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleListRunners handles GET /api/v1/runners.
func (s *Server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	if s.runners == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Runner service not configured")
		return
	}

	opts := ListRunnersOptions{
		Limit:    parseIntQuery(r, "limit", 50),
		Cursor:   r.URL.Query().Get("cursor"),
		Status:   r.URL.Query()["status"],
		PoolName: r.URL.Query().Get("pool_name"),
		Labels:   parseLabelsQuery(r),
	}

	result, err := s.runners.List(r.Context(), opts)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetRunner handles GET /api/v1/runners/{runnerID}.
func (s *Server) handleGetRunner(w http.ResponseWriter, r *http.Request) {
	if s.runners == nil {
		WriteError(w, http.StatusInternalServerError, "service_unavailable", "Runner service not configured")
		return
	}

	runnerID := chi.URLParam(r, "runnerID")
	if runnerID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_id", "Runner ID is required")
		return
	}

	runner, err := s.runners.Get(r.Context(), runnerID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, runner)
}
