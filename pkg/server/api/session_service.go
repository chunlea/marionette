// Package public provides the public HTTP API server for Marionette.
package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// SessionService defines operations for managing sessions.
// This interface is implemented by the core session manager and
// can be mocked for testing HTTP handlers.
type SessionService interface {
	// Create creates a new session with the given options.
	Create(ctx context.Context, opts CreateSessionOptions) (*store.Session, error)

	// Get retrieves a session by ID.
	Get(ctx context.Context, id string) (*store.Session, error)

	// List returns sessions matching the filter options.
	List(ctx context.Context, opts ListSessionsOptions) (*store.ListResult[store.Session], error)

	// Suspend suspends an active session, releasing its runner while preserving state.
	Suspend(ctx context.Context, id string) error

	// Resume resumes a suspended session, acquiring a new runner.
	Resume(ctx context.Context, id string) error

	// Terminate terminates a session and cleans up resources.
	Terminate(ctx context.Context, id string) error
}

// CreateSessionOptions contains options for creating a session.
type CreateSessionOptions struct {
	Name               string            `json:"name,omitempty"`
	Agent              string            `json:"agent"`
	AgentConfigID      string            `json:"agent_config_id,omitempty"`
	APIKey             string            `json:"api_key,omitempty"`      // For BYOK mode
	WorkspaceID        string            `json:"workspace_id,omitempty"` // Existing workspace to use
	ProfileID          string            `json:"profile_id,omitempty"`   // Profile for runner configuration
	LifecycleMode      string            `json:"lifecycle_mode,omitempty"`
	IdleTimeoutSeconds int               `json:"idle_timeout_seconds,omitempty"`
	NetworkPolicy      string            `json:"network_policy,omitempty"`
	AllowedHosts       []string          `json:"allowed_hosts,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
}

// ListSessionsOptions contains options for listing sessions.
type ListSessionsOptions struct {
	Limit         int               `json:"limit,omitempty"`
	Cursor        string            `json:"cursor,omitempty"`
	Status        []string          `json:"status,omitempty"`
	Agent         string            `json:"agent,omitempty"`
	LifecycleMode string            `json:"lifecycle_mode,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}
