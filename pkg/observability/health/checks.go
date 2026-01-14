package health

import (
	"context"
	"strconv"
	"time"
)

// Pinger is an interface for checking database connectivity.
// It's implemented by store.Store.
type Pinger interface {
	Ping(ctx context.Context) error
}

// DatabaseCheck creates a health check function for database connectivity.
// It calls the Ping method on the store with a 5-second timeout.
func DatabaseCheck(store Pinger) CheckFunc {
	return func(ctx context.Context) CheckResult {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := store.Ping(ctx); err != nil {
			return CheckResult{
				Status:  StatusFail,
				Message: err.Error(),
			}
		}
		return CheckResult{
			Status: StatusOK,
		}
	}
}

// ConnectionCounter is an interface for counting active connections.
type ConnectionCounter interface {
	Count() int
}

// ConnectionManagerCheck creates a health check function for the gRPC connection manager.
// It verifies that the connection manager is operational.
// Note: This check always passes if the manager is non-nil, since having 0 connections
// is a valid state (no agents connected yet).
// If cm is nil, returns a check that always fails.
func ConnectionManagerCheck(cm ConnectionCounter) CheckFunc {
	if cm == nil {
		return func(_ context.Context) CheckResult {
			return CheckResult{
				Status:  StatusFail,
				Message: "connection manager not initialized",
			}
		}
	}
	return func(_ context.Context) CheckResult {
		count := cm.Count()
		return CheckResult{
			Status:  StatusOK,
			Message: formatConnectionCount(count),
		}
	}
}

// formatConnectionCount formats the connection count message.
func formatConnectionCount(count int) string {
	if count == 1 {
		return "1 runner connected"
	}
	return strconv.Itoa(count) + " runners connected"
}

// CustomCheck creates a health check from a simple function that returns an error.
// If the function returns nil, the check passes; otherwise, it fails.
func CustomCheck(fn func(ctx context.Context) error) CheckFunc {
	return func(ctx context.Context) CheckResult {
		if err := fn(ctx); err != nil {
			return CheckResult{
				Status:  StatusFail,
				Message: err.Error(),
			}
		}
		return CheckResult{
			Status: StatusOK,
		}
	}
}
