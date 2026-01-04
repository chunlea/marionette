package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// PermissionManagerInterface defines the methods needed from core.PermissionManager.
type PermissionManagerInterface interface {
	Get(ctx context.Context, permID string) (*store.PermissionRequest, error)
	List(ctx context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error)
	Respond(ctx context.Context, permID string, approved bool, reason, respondedBy string) error
}

// PermissionAdapter adapts core.PermissionManager to api.PermissionService.
type PermissionAdapter struct {
	manager PermissionManagerInterface
}

// NewPermissionAdapter creates a new PermissionAdapter.
func NewPermissionAdapter(manager PermissionManagerInterface) *PermissionAdapter {
	return &PermissionAdapter{
		manager: manager,
	}
}

// Get retrieves a permission request by ID.
func (a *PermissionAdapter) Get(ctx context.Context, id string) (*store.PermissionRequest, error) {
	return a.manager.Get(ctx, id)
}

// List returns permission requests matching the filter options.
func (a *PermissionAdapter) List(ctx context.Context, opts ListPermissionsOptions) (*store.ListResult[store.PermissionRequest], error) {
	coreOpts := store.ListPermissionRequestsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status:    opts.Status,
		RiskLevel: opts.RiskLevel,
	}
	if opts.SessionID != "" {
		coreOpts.SessionID = &opts.SessionID
	}
	if opts.TaskID != "" {
		coreOpts.TaskID = &opts.TaskID
	}
	return a.manager.List(ctx, coreOpts)
}

// Approve approves a pending permission request.
func (a *PermissionAdapter) Approve(ctx context.Context, id string, opts ApproveOptions) error {
	// Use "api" as respondedBy since we don't have user context
	return a.manager.Respond(ctx, id, true, opts.Reason, "api")
}

// Deny denies a pending permission request.
func (a *PermissionAdapter) Deny(ctx context.Context, id string, opts DenyOptions) error {
	// Use "api" as respondedBy since we don't have user context
	return a.manager.Respond(ctx, id, false, opts.Reason, "api")
}

// Ensure PermissionAdapter implements PermissionService.
var _ PermissionService = (*PermissionAdapter)(nil)
