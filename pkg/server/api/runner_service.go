package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// RunnerService defines operations for viewing runners.
// Runners are managed by the system, so this is read-only for the public API.
type RunnerService interface {
	// Get retrieves a runner by ID.
	Get(ctx context.Context, id string) (*store.Runner, error)

	// List returns runners matching the filter options.
	List(ctx context.Context, opts ListRunnersOptions) (*store.ListResult[store.Runner], error)
}

// ListRunnersOptions contains options for listing runners.
type ListRunnersOptions struct {
	Limit    int               `json:"limit,omitempty"`
	Cursor   string            `json:"cursor,omitempty"`
	Status   []string          `json:"status,omitempty"`
	PoolName string            `json:"pool_name,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}
