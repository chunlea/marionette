// Package api provides the public HTTP API server.
package api //nolint:revive // api is a standard package name for HTTP APIs

import (
	"encoding/json"
	"net/http"
)

// HealthResponse is the response for health check endpoints.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// healthHandler returns the health status of the server.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		Status:  "ok",
		Version: "dev",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
