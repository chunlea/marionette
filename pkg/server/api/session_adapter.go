package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// SessionManagerInterface defines the methods needed from core.SessionManager.
type SessionManagerInterface interface {
	Create(ctx context.Context, opts core.CreateSessionOptions) (*store.Session, error)
	Get(ctx context.Context, sessionID string) (*store.Session, error)
	List(ctx context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error)
	Suspend(ctx context.Context, sessionID string, strategy string) error
	Resume(ctx context.Context, sessionID string) error
	Terminate(ctx context.Context, sessionID string) error
}

// SessionAdapter adapts core.SessionManager to api.SessionService.
type SessionAdapter struct {
	manager          SessionManagerInterface
	workspaceManager core.WorkspaceManagerInterface
	logs             *ArchivedLogReader
}

// SessionAdapterOption configures a SessionAdapter.
type SessionAdapterOption func(*SessionAdapter)

// WithSessionLogReader supplies the reader that serves session logs.
//
// Without it GetLogs reports that log retrieval is not configured rather than
// silently returning nothing: an empty page and "this server cannot answer
// that" are different answers, and only one of them is safe to believe.
func WithSessionLogReader(reader *ArchivedLogReader) SessionAdapterOption {
	return func(a *SessionAdapter) { a.logs = reader }
}

// NewSessionAdapter creates a new SessionAdapter.
func NewSessionAdapter(
	manager SessionManagerInterface,
	workspaceManager core.WorkspaceManagerInterface,
	opts ...SessionAdapterOption,
) *SessionAdapter {
	a := &SessionAdapter{
		manager:          manager,
		workspaceManager: workspaceManager,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// GetLogs returns the session's logs, from the archive and from PostgreSQL.
func (a *SessionAdapter) GetLogs(
	ctx context.Context,
	sessionID string,
	opts GetLogsOptions,
) (*store.ListResult[store.Log], error) {
	if a.logs == nil {
		return nil, ErrNotImplemented
	}

	// The session has to exist, and the lookup is what applies tenant
	// isolation: without it a caller could read any tenant's archive by
	// guessing a session id, because the reader queries by session rather than
	// through a policy-checked row the caller already holds.
	if _, err := a.manager.Get(ctx, sessionID); err != nil {
		return nil, err
	}

	return a.logs.Read(ctx, logQuery{
		SessionID: sessionID,
		Limit:     opts.Limit,
		Cursor:    opts.Cursor,
		Level:     opts.Level,
		Stream:    opts.Stream,
		Archived:  opts.Archived,
	})
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
	if opts.ProfileID != "" {
		coreOpts.ProfileID = &opts.ProfileID
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
	wsID, err := a.ensureWorkspace(ctx, opts)
	if err != nil {
		return nil, err
	}
	coreOpts.WorkspaceID = wsID

	return a.manager.Create(ctx, coreOpts)
}

// ensureWorkspace creates or retrieves a workspace for the session.
func (a *SessionAdapter) ensureWorkspace(ctx context.Context, opts CreateSessionOptions) (string, error) {
	// If an existing workspace ID is provided, use it
	if opts.WorkspaceID != "" {
		// Verify the workspace exists
		ws, err := a.workspaceManager.Get(ctx, opts.WorkspaceID)
		if err != nil {
			return "", err
		}
		return ws.ID, nil
	}

	// Create a new workspace
	wsOpts := core.CreateWorkspaceOptions{
		Name: opts.Name,
	}

	ws, err := a.workspaceManager.Create(ctx, wsOpts)
	if err != nil {
		return "", err
	}

	return ws.ID, nil
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
