package api

import (
	"context"
	"errors"
	"fmt"

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
	logs    *ArchivedLogReader
}

// TaskAdapterOption configures a TaskAdapter.
type TaskAdapterOption func(*TaskAdapter)

// WithTaskLogArchive lets task log reads fall through to the session's archive.
//
// Optional, because a deployment with archiving switched off has nothing to
// fall through to and the hot rows are the whole story. When it is set, a task
// whose logs have been archived reads exactly as it did before they were.
func WithTaskLogArchive(reader *ArchivedLogReader) TaskAdapterOption {
	return func(a *TaskAdapter) { a.logs = reader }
}

// NewTaskAdapter creates a new TaskAdapter.
func NewTaskAdapter(manager *core.TaskManager, store store.Store, opts ...TaskAdapterOption) *TaskAdapter {
	a := &TaskAdapter{
		manager: manager,
		store:   store,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ListRuns returns the execution attempts of a task.
func (a *TaskAdapter) ListRuns(ctx context.Context, taskID string, opts ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	result, err := a.manager.ListRuns(ctx, taskID, core.ListTaskRunsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status: opts.Status,
	})
	if err != nil {
		if errors.Is(err, core.ErrTaskNotFound) {
			// handleServiceError keys 404 off the store sentinel, and
			// core.ErrTaskNotFound is a distinct value that would otherwise
			// fall through to a 500.
			return nil, fmt.Errorf("%w: task %s", store.ErrNotFound, taskID)
		}
		return nil, err
	}
	return result, nil
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

// GetLogs returns logs for a task, from the archive as well as the hot rows.
//
// Archiving is by session, so serving a task's archived logs means finding the
// session first and filtering the archive down to the task. That lookup only
// happens when there is an archive reader to use it: with archiving off this is
// the same single query it always was.
func (a *TaskAdapter) GetLogs(ctx context.Context, taskID string, opts GetLogsOptions) (*store.ListResult[store.Log], error) {
	if a.logs == nil {
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

	sessionID, err := a.sessionOf(ctx, taskID)
	if err != nil {
		return nil, err
	}

	return a.logs.Read(ctx, logQuery{
		SessionID: sessionID,
		TaskID:    taskID,
		Limit:     opts.Limit,
		Cursor:    opts.Cursor,
		Level:     opts.Level,
		Stream:    opts.Stream,
		Archived:  opts.Archived,
	})
}

// sessionOf resolves the session a task belongs to.
func (a *TaskAdapter) sessionOf(ctx context.Context, taskID string) (string, error) {
	task, err := a.manager.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, core.ErrTaskNotFound) {
			return "", fmt.Errorf("%w: task %s", store.ErrNotFound, taskID)
		}
		return "", err
	}
	return task.SessionID, nil
}

// StreamLogs streams logs for a task in real-time.
func (a *TaskAdapter) StreamLogs(ctx context.Context, taskID string, opts StreamLogsOptions) (<-chan *store.Log, error) {
	// For now, return an error indicating streaming is not yet implemented
	// Real implementation would use websocket or SSE
	return nil, errors.New("log streaming not yet implemented")
}

// Ensure TaskAdapter implements TaskService.
var _ TaskService = (*TaskAdapter)(nil)
