package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// WorkspaceService defines operations for managing workspaces.
type WorkspaceService interface {
	// Create creates a new workspace.
	Create(ctx context.Context, opts CreateWorkspaceOptions) (*store.Workspace, error)

	// Get retrieves a workspace by ID.
	Get(ctx context.Context, id string) (*store.Workspace, error)

	// List returns workspaces matching the filter options.
	List(ctx context.Context, opts ListWorkspacesOptions) (*store.ListResult[store.Workspace], error)

	// Update updates a workspace's mutable fields.
	Update(ctx context.Context, id string, opts UpdateWorkspaceOptions) (*store.Workspace, error)

	// Delete soft-deletes a workspace.
	Delete(ctx context.Context, id string) error
}

// CreateWorkspaceOptions contains options for creating a workspace.
type CreateWorkspaceOptions struct {
	Name        string            `json:"name,omitempty"`
	Persist     *bool             `json:"persist,omitempty"`
	StorageType string            `json:"storage_type,omitempty"`
	Mobility    string            `json:"mobility,omitempty"`
	DiskQuotaMB *int              `json:"disk_quota_mb,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ListWorkspacesOptions contains options for listing workspaces.
type ListWorkspacesOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// UpdateWorkspaceOptions contains options for updating a workspace.
type UpdateWorkspaceOptions struct {
	Name        *string           `json:"name,omitempty"`
	Persist     *bool             `json:"persist,omitempty"`
	DiskQuotaMB *int              `json:"disk_quota_mb,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}
