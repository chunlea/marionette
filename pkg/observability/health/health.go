// Package health provides health check infrastructure for Kubernetes probes.
// It supports liveness and readiness checks with pluggable health check functions.
package health

import (
	"context"
	"maps"
	"sync"
	"time"
)

// Status represents the health check status.
type Status string

const (
	// StatusOK indicates the check passed.
	StatusOK Status = "ok"
	// StatusFail indicates the check failed.
	StatusFail Status = "fail"
)

// CheckResult represents the result of a single health check.
type CheckResult struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// Response represents the overall health check response.
type Response struct {
	Status Status                 `json:"status"`
	Checks map[string]CheckResult `json:"checks,omitempty"`
}

// CheckFunc is a function that performs a health check.
// It should return a CheckResult indicating the health status.
type CheckFunc func(ctx context.Context) CheckResult

// Checker manages health checks for the application.
// It supports registering multiple check functions and running them
// for liveness and readiness probes.
type Checker struct {
	checks map[string]CheckFunc
	mu     sync.RWMutex
}

// NewChecker creates a new health checker.
func NewChecker() *Checker {
	return &Checker{
		checks: make(map[string]CheckFunc),
	}
}

// Register adds a health check function with the given name.
// If a check with the same name already exists, it will be replaced.
func (c *Checker) Register(name string, fn CheckFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = fn
}

// Unregister removes a health check function by name.
func (c *Checker) Unregister(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.checks, name)
}

// CheckLiveness performs a liveness check.
// Liveness probes indicate whether the application is running.
// This always returns OK if the server can respond (process is alive).
func (c *Checker) CheckLiveness(_ context.Context) Response {
	return Response{
		Status: StatusOK,
	}
}

// CheckReadiness performs a readiness check.
// Readiness probes indicate whether the application is ready to accept traffic.
// This runs all registered checks and returns the aggregated result.
func (c *Checker) CheckReadiness(ctx context.Context) Response {
	c.mu.RLock()
	checks := maps.Clone(c.checks)
	c.mu.RUnlock()

	results := make(map[string]CheckResult, len(checks))
	overallStatus := StatusOK

	// Run checks concurrently
	var wg sync.WaitGroup
	var resultsMu sync.Mutex

	for name, fn := range checks {
		wg.Add(1)
		go func(name string, fn CheckFunc) {
			defer wg.Done()

			start := time.Now()
			result := fn(ctx)
			result.Latency = time.Since(start).Round(time.Microsecond).String()

			resultsMu.Lock()
			results[name] = result
			if result.Status == StatusFail {
				overallStatus = StatusFail
			}
			resultsMu.Unlock()
		}(name, fn)
	}

	wg.Wait()

	return Response{
		Status: overallStatus,
		Checks: results,
	}
}

// CheckCount returns the number of registered checks.
func (c *Checker) CheckCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.checks)
}
