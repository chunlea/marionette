package admin

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// ActionLogStoreAdapter adapts store.Store to ActionLogService.
type ActionLogStoreAdapter struct {
	store store.Store
}

// NewActionLogStoreAdapter creates a new ActionLogStoreAdapter.
func NewActionLogStoreAdapter(s store.Store) *ActionLogStoreAdapter {
	return &ActionLogStoreAdapter{store: s}
}

// Get retrieves an action log by ID.
func (a *ActionLogStoreAdapter) Get(ctx context.Context, id string) (*store.ActionLog, error) {
	return a.store.GetActionLog(ctx, id)
}

// List returns action logs matching the given options.
func (a *ActionLogStoreAdapter) List(ctx context.Context, opts ListActionLogsOptions) (*ListResult[store.ActionLog], error) {
	storeOpts := store.ListActionLogsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:     opts.Limit,
			Cursor:    opts.Cursor,
			OrderDesc: true, // Always order by newest first
		},
	}

	// Convert filter options
	if opts.ActorType != "" {
		storeOpts.ActorType = &opts.ActorType
	}
	if opts.ActorID != "" {
		storeOpts.ActorID = &opts.ActorID
	}
	if opts.Action != "" {
		storeOpts.Action = &opts.Action
	}
	if opts.ActionPrefix != "" {
		storeOpts.ActionPrefix = &opts.ActionPrefix
	}
	if opts.ResourceType != "" {
		storeOpts.ResourceType = &opts.ResourceType
	}
	if opts.ResourceID != "" {
		storeOpts.ResourceID = &opts.ResourceID
	}
	if opts.SessionID != "" {
		storeOpts.SessionID = &opts.SessionID
	}
	if opts.TaskID != "" {
		storeOpts.TaskID = &opts.TaskID
	}
	if opts.Success != nil {
		storeOpts.Success = opts.Success
	}
	if opts.From != nil {
		storeOpts.From = opts.From
	}
	if opts.To != nil {
		storeOpts.To = opts.To
	}

	result, err := a.store.ListActionLogs(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	return &ListResult[store.ActionLog]{
		Items:      result.Items,
		TotalCount: result.TotalCount,
		NextCursor: result.NextCursor,
	}, nil
}

// Ensure ActionLogStoreAdapter implements ActionLogService.
var _ ActionLogService = (*ActionLogStoreAdapter)(nil)
