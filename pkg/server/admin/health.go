// Package admin provides the admin HTTP API server.
package admin

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/chunlea/marionette/pkg/observability/health"
)

// HealthResponse is the response for health check endpoints.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

// HealthService defines the interface for health checking.
type HealthService interface {
	CheckLiveness(ctx context.Context) health.Response
	CheckReadiness(ctx context.Context) health.Response
}

// healthHandler returns the health status of the admin server.
// This is the legacy endpoint for backward compatibility.
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

// livenessHandler returns the liveness probe status.
// Liveness probes indicate whether the application is running.
// Returns 200 OK if the server can respond.
func livenessHandler(hs HealthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := hs.CheckLiveness(r.Context())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// readinessHandler returns the readiness probe status.
// Readiness probes indicate whether the application is ready to accept traffic.
// Returns 200 OK if all checks pass, 503 Service Unavailable otherwise.
func readinessHandler(hs HealthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := hs.CheckReadiness(r.Context())

		w.Header().Set("Content-Type", "application/json")
		if resp.Status == health.StatusOK {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}
