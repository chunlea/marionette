package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRegistry_Register(t *testing.T) {
	registry := &ServiceRegistry{
		services: make(map[string]ServiceStatus),
	}

	registry.Register("TestService", 8080, "ok", "Running")

	services := registry.GetAll()
	require.Len(t, services, 1)
	assert.Equal(t, "TestService", services[0].Name)
	assert.Equal(t, 8080, services[0].Port)
	assert.Equal(t, "ok", services[0].Status)
	assert.Equal(t, "Running", services[0].Message)
}

func TestServiceRegistry_RegisterMultiple(t *testing.T) {
	registry := &ServiceRegistry{
		services: make(map[string]ServiceStatus),
	}

	registry.Register("Service1", 8080, "ok", "Running")
	registry.Register("Service2", 8081, "ok", "Running")
	registry.Register("Service3", 9090, "error", "Failed")

	services := registry.GetAll()
	assert.Len(t, services, 3)
}

func TestServiceRegistry_Update(t *testing.T) {
	registry := &ServiceRegistry{
		services: make(map[string]ServiceStatus),
	}

	registry.Register("TestService", 8080, "ok", "Running")
	registry.Register("TestService", 8080, "error", "Stopped")

	services := registry.GetAll()
	require.Len(t, services, 1)
	assert.Equal(t, "error", services[0].Status)
	assert.Equal(t, "Stopped", services[0].Message)
}

func TestStatusHandler(t *testing.T) {
	// Save original registry and restore after test
	originalServices := Registry.services
	Registry.services = make(map[string]ServiceStatus)
	defer func() { Registry.services = originalServices }()

	// Register test services
	Registry.Register("Public API", 8080, "ok", "Running")
	Registry.Register("Admin API", 8081, "ok", "Running")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	statusHandler(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var status StatusResponse
	err := json.NewDecoder(resp.Body).Decode(&status)
	require.NoError(t, err)

	assert.Len(t, status.Services, 2)
}

func TestStatusHandler_Empty(t *testing.T) {
	// Save original registry and restore after test
	originalServices := Registry.services
	Registry.services = make(map[string]ServiceStatus)
	defer func() { Registry.services = originalServices }()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	statusHandler(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var status StatusResponse
	err := json.NewDecoder(resp.Body).Decode(&status)
	require.NoError(t, err)

	assert.Empty(t, status.Services)
}
