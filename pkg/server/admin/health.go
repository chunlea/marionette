// Package admin provides the admin HTTP API server.
package admin

import (
	"encoding/json"
	"net/http"
)

// HealthResponse is the response for health check endpoints.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

// healthHandler returns the health status of the admin server.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		Status:  "ok",
		Version: "dev",
		Service: "admin",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
