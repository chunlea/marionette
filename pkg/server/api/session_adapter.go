package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// workspaceCreator is the interface needed by SessionAdapter for workspace operations.
type workspaceCreator interface {
	CreateWorkspace(ctx context.Context, ws *store.Workspace) error
}

// SessionAdapter adapts core.SessionManager to api.SessionService.
type SessionAdapter struct {
	manager *core.SessionManager
	store   workspaceCreator
}

// NewSessionAdapter creates a new SessionAdapter.
func NewSessionAdapter(manager *core.SessionManager, store store.Store) *SessionAdapter {
	return &SessionAdapter{
		manager: manager,
		store:   store,
	}
}

// Create creates a new session with the given options.
func (a *SessionAdapter) Create(ctx context.Context, opts CreateSessionOptions) (*store.Session, error) {
	// Convert API options to core options
	coreOpts := core.CreateSessionOptions{
		Agent:         opts.Agent,
		LifecycleMode: opts.LifecycleMode,
		NetworkPolicy: opts.NetworkPolicy,
		AllowedHosts:  opts.AllowedHosts,
		Labels:        opts.Labels,
	}

	if opts.Name != "" {
		coreOpts.Name = &opts.Name
	}
	if opts.AgentConfigID != "" {
		coreOpts.AgentConfigID = &opts.AgentConfigID
	}
	if opts.IdleTimeoutSeconds > 0 {
		coreOpts.IdleTimeout = &opts.IdleTimeoutSeconds
	}

	// Handle BYOK mode
	if opts.APIKey != "" {
		coreOpts.IsBYOK = true
		// Note: API key handling would be done by a separate credential manager
	}

	// Create or get workspace
	// For now, create a new workspace for each session
	wsID, err := a.ensureWorkspace(ctx, opts.Name)
	if err != nil {
		return nil, err
	}
	coreOpts.WorkspaceID = wsID

	return a.manager.Create(ctx, coreOpts)
}

// ensureWorkspace creates or retrieves a workspace for the session.
func (a *SessionAdapter) ensureWorkspace(ctx context.Context, name string) (string, error) {
	wsID := id.Workspace()
	wsName := name
	if wsName == "" {
		wsName = "workspace-" + wsID
	}

	ws := &store.Workspace{
		ID:          wsID,
		Name:        wsName,
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}

	if err := a.store.CreateWorkspace(ctx, ws); err != nil {
		return "", err
	}
	return wsID, nil
}

// Get retrieves a session by ID.
func (a *SessionAdapter) Get(ctx context.Context, id string) (*store.Session, error) {
	return a.manager.Get(ctx, id)
}

// List returns sessions matching the filter options.
func (a *SessionAdapter) List(ctx context.Context, opts ListSessionsOptions) (*store.ListResult[store.Session], error) {
	coreOpts := store.ListSessionsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status: opts.Status,
	}
	return a.manager.List(ctx, coreOpts)
}

// Suspend suspends an active session.
func (a *SessionAdapter) Suspend(ctx context.Context, id string) error {
	// Use default strategy "terminate" for now
	return a.manager.Suspend(ctx, id, "terminate")
}

// Resume resumes a suspended session.
func (a *SessionAdapter) Resume(ctx context.Context, id string) error {
	return a.manager.Resume(ctx, id)
}

// Terminate terminates a session.
func (a *SessionAdapter) Terminate(ctx context.Context, id string) error {
	return a.manager.Terminate(ctx, id)
}

// Ensure SessionAdapter implements SessionService.
var _ SessionService = (*SessionAdapter)(nil)
