package core

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// LogSubscriberManagerInterface defines the interface for managing real-time log subscribers.
// This is implemented by LogSubscriberManager and used for dependency injection.
type LogSubscriberManagerInterface interface {
	// Broadcast sends a log entry to all subscribers for the session.
	Broadcast(log *store.Log)
	// Subscribe registers a channel to receive logs for a session.
	Subscribe(sessionID string, ch chan *store.Log)
	// Unsubscribe removes a channel from session subscriptions.
	Unsubscribe(sessionID string, ch chan *store.Log)
}

// PermissionManagerInterface defines the interface for permission request management.
// This is used for dependency injection in other components.
type PermissionManagerInterface interface {
	// Create stores a new permission request from runner.
	Create(ctx context.Context, req *CreatePermissionRequestInput) (*store.PermissionRequest, error)
	// Respond approves or denies a permission request.
	Respond(ctx context.Context, permID string, approved bool, reason, respondedBy string) error
	// Get retrieves a permission request by ID.
	Get(ctx context.Context, permID string) (*store.PermissionRequest, error)
	// List retrieves permission requests with filters.
	List(ctx context.Context, opts ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error)
	// Cancel cancels a pending permission request.
	Cancel(ctx context.Context, permID string) error
}

// CreatePermissionRequestInput contains input for creating a permission request.
type CreatePermissionRequestInput struct {
	OriginalRequestID   string  // Original request ID from agent (e.g., tool_use_id)
	SessionID           string
	TaskID              string
	RunID               string
	Tool                string
	Action              string
	Context             string
	RiskLevel           string
	SuspendAfterSeconds int
	TenantID            *string
}

// ListPermissionRequestsOptions wraps store.ListPermissionRequestsOptions for convenience.
type ListPermissionRequestsOptions = store.ListPermissionRequestsOptions
