package api

import (
	"context"
	"errors"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// logLister is the interface needed by TaskAdapter for log operations.
type logLister interface {
	ListLogs(ctx context.Context, opts store.ListLogsOptions) (*store.ListResult[store.Log], error)
}

// TaskAdapter adapts core.TaskManager to api.TaskService.
type TaskAdapter struct {
	manager *core.TaskManager
	store   logLister
}

// NewTaskAdapter creates a new TaskAdapter.
func NewTaskAdapter(manager *core.TaskManager, store store.Store) *TaskAdapter {
	return &TaskAdapter{
		manager: manager,
		store:   store,
	}
}

// Create creates a new task in a session.
func (a *TaskAdapter) Create(ctx context.Context, opts CreateTaskOptions) (*store.Task, error) {
	coreOpts := core.CreateTaskOptions{
		SessionID:      opts.SessionID,
		Prompt:         opts.Prompt,
		TimeoutSeconds: opts.TimeoutSeconds,
		MaxRetries:     opts.MaxRetries,
	}

	// TODO: Handle ContinueFrom option (load context from previous task)

	return a.manager.Create(ctx, coreOpts)
}

// Get retrieves a task by ID.
func (a *TaskAdapter) Get(ctx context.Context, id string) (*store.Task, error) {
	return a.manager.Get(ctx, id)
}

// List returns tasks matching the filter options.
func (a *TaskAdapter) List(ctx context.Context, opts ListTasksOptions) (*store.ListResult[store.Task], error) {
	coreOpts := store.ListTasksOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status: opts.Status,
	}
	if opts.SessionID != "" {
		coreOpts.SessionID = &opts.SessionID
	}
	return a.manager.List(ctx, coreOpts)
}

// Cancel cancels a pending or running task.
func (a *TaskAdapter) Cancel(ctx context.Context, id string) error {
	return a.manager.Cancel(ctx, id)
}

// Retry retries a failed task.
func (a *TaskAdapter) Retry(ctx context.Context, id string) error {
	_, err := a.manager.Retry(ctx, id)
	return err
}

// Execute starts execution of a pending task.
func (a *TaskAdapter) Execute(ctx context.Context, id string) error {
	return a.manager.Execute(ctx, id)
}

// GetLogs returns logs for a task.
func (a *TaskAdapter) GetLogs(ctx context.Context, taskID string, opts GetLogsOptions) (*store.ListResult[store.Log], error) {
	// Query logs directly from store
	storeOpts := store.ListLogsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		TaskID: &taskID,
		Level:  opts.Level,
		Stream: opts.Stream,
	}
	return a.store.ListLogs(ctx, storeOpts)
}

// StreamLogs streams logs for a task in real-time.
func (a *TaskAdapter) StreamLogs(ctx context.Context, taskID string, opts StreamLogsOptions) (<-chan *store.Log, error) {
	// For now, return an error indicating streaming is not yet implemented
	// Real implementation would use websocket or SSE
	return nil, errors.New("log streaming not yet implemented")
}

// Ensure TaskAdapter implements TaskService.
var _ TaskService = (*TaskAdapter)(nil)
