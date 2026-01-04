package api

import (
	"context"
	"encoding/json"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// WorkspaceAdapter adapts core.WorkspaceManager to api.WorkspaceService.
type WorkspaceAdapter struct {
	manager core.WorkspaceManagerInterface
}

// NewWorkspaceAdapter creates a new WorkspaceAdapter.
func NewWorkspaceAdapter(manager core.WorkspaceManagerInterface) *WorkspaceAdapter {
	return &WorkspaceAdapter{
		manager: manager,
	}
}

// Create creates a new workspace.
func (a *WorkspaceAdapter) Create(ctx context.Context, opts CreateWorkspaceOptions) (*store.Workspace, error) {
	coreOpts := core.CreateWorkspaceOptions{
		Name:        opts.Name,
		Persist:     opts.Persist,
		StorageType: opts.StorageType,
		Mobility:    opts.Mobility,
		DiskQuotaMB: opts.DiskQuotaMB,
		Labels:      opts.Labels,
		Annotations: opts.Annotations,
	}

	return a.manager.Create(ctx, coreOpts)
}

// Get retrieves a workspace by ID.
func (a *WorkspaceAdapter) Get(ctx context.Context, id string) (*store.Workspace, error) {
	return a.manager.Get(ctx, id)
}

// List returns workspaces matching the filter options.
func (a *WorkspaceAdapter) List(ctx context.Context, opts ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	coreOpts := core.ListWorkspacesOptions{
		Limit:  opts.Limit,
		Cursor: opts.Cursor,
	}
	return a.manager.List(ctx, coreOpts)
}

// Update updates a workspace's mutable fields.
func (a *WorkspaceAdapter) Update(ctx context.Context, id string, opts UpdateWorkspaceOptions) (*store.Workspace, error) {
	updates := store.WorkspaceUpdates{
		Name:        opts.Name,
		Persist:     opts.Persist,
		DiskQuotaMB: opts.DiskQuotaMB,
	}

	// Convert labels and annotations to JSON
	if opts.Labels != nil {
		labelsJSON, err := json.Marshal(opts.Labels)
		if err != nil {
			return nil, err
		}
		updates.Labels = labelsJSON
	}

	if opts.Annotations != nil {
		annotationsJSON, err := json.Marshal(opts.Annotations)
		if err != nil {
			return nil, err
		}
		updates.Annotations = annotationsJSON
	}

	return a.manager.Update(ctx, id, updates)
}

// Delete soft-deletes a workspace.
func (a *WorkspaceAdapter) Delete(ctx context.Context, id string) error {
	return a.manager.Delete(ctx, id)
}

// Ensure WorkspaceAdapter implements WorkspaceService.
var _ WorkspaceService = (*WorkspaceAdapter)(nil)
