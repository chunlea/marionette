package public

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// PermissionService defines operations for managing permission requests.
type PermissionService interface {
	// Get retrieves a permission request by ID.
	Get(ctx context.Context, id string) (*store.PermissionRequest, error)

	// List returns permission requests matching the filter options.
	List(ctx context.Context, opts ListPermissionsOptions) (*store.ListResult[store.PermissionRequest], error)

	// Approve approves a pending permission request.
	Approve(ctx context.Context, id string, opts ApproveOptions) error

	// Deny denies a pending permission request.
	Deny(ctx context.Context, id string, opts DenyOptions) error
}

// ListPermissionsOptions contains options for listing permission requests.
type ListPermissionsOptions struct {
	Limit     int      `json:"limit,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	TaskID    string   `json:"task_id,omitempty"`
	Status    []string `json:"status,omitempty"`
	RiskLevel []string `json:"risk_level,omitempty"`
}

// ApproveOptions contains options for approving a permission request.
type ApproveOptions struct {
	Reason string `json:"reason,omitempty"`
}

// DenyOptions contains options for denying a permission request.
type DenyOptions struct {
	Reason string `json:"reason,omitempty"`
}
