package admin

import (
	"encoding/json"
	"net/http"
	"sync"
)

// ServiceStatus represents the status of a service.
type ServiceStatus struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Status  string `json:"status"` // "ok", "error", "unknown"
	Message string `json:"message,omitempty"`
}

// StatusResponse contains the status of all services.
type StatusResponse struct {
	Services []ServiceStatus `json:"services"`
}

// ServiceRegistry tracks running services.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]ServiceStatus
}

// Registry is the global service registry, set by main.go after starting services.
var Registry = &ServiceRegistry{
	services: make(map[string]ServiceStatus),
}

// Register adds or updates a service status.
func (r *ServiceRegistry) Register(name string, port int, status, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[name] = ServiceStatus{
		Name:    name,
		Port:    port,
		Status:  status,
		Message: message,
	}
}

// GetAll returns all registered services.
func (r *ServiceRegistry) GetAll() []ServiceStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ServiceStatus, 0, len(r.services))
	for _, s := range r.services {
		result = append(result, s)
	}
	return result
}

// statusHandler returns the status of all services.
func statusHandler(w http.ResponseWriter, _ *http.Request) {
	resp := StatusResponse{
		Services: Registry.GetAll(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
