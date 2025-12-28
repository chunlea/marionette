package api

import (
	"context"
	"io"

	"github.com/chunlea/marionette/pkg/store"
)

// TaskService defines operations for managing tasks.
type TaskService interface {
	// Create creates a new task in a session.
	Create(ctx context.Context, opts CreateTaskOptions) (*store.Task, error)

	// Get retrieves a task by ID.
	Get(ctx context.Context, id string) (*store.Task, error)

	// List returns tasks matching the filter options.
	List(ctx context.Context, opts ListTasksOptions) (*store.ListResult[store.Task], error)

	// Cancel cancels a pending or running task.
	Cancel(ctx context.Context, id string) error

	// Retry retries a failed task.
	Retry(ctx context.Context, id string) error

	// GetLogs returns logs for a task.
	GetLogs(ctx context.Context, taskID string, opts GetLogsOptions) (*store.ListResult[store.Log], error)

	// StreamLogs streams logs for a task in real-time.
	// The returned channel will be closed when the task completes or context is cancelled.
	StreamLogs(ctx context.Context, taskID string, opts StreamLogsOptions) (<-chan *store.Log, error)
}

// CreateTaskOptions contains options for creating a task.
type CreateTaskOptions struct {
	SessionID      string `json:"session_id"`
	Prompt         string `json:"prompt"`
	ContinueFrom   string `json:"continue_from,omitempty"` // Previous task ID to continue from
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
}

// ListTasksOptions contains options for listing tasks.
type ListTasksOptions struct {
	Limit     int      `json:"limit,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Status    []string `json:"status,omitempty"`
}

// GetLogsOptions contains options for retrieving task logs.
type GetLogsOptions struct {
	Limit  int      `json:"limit,omitempty"`
	Cursor string   `json:"cursor,omitempty"`
	Stream []string `json:"stream,omitempty"`
}

// StreamLogsOptions contains options for streaming task logs.
type StreamLogsOptions struct {
	Tail   int      `json:"tail,omitempty"` // Number of recent logs to include
	Stream []string `json:"stream,omitempty"`
}

// LogStream represents a streaming log connection.
type LogStream interface {
	// Next returns the next log entry, blocking until available.
	// Returns io.EOF when the stream is closed.
	Next() (*store.Log, error)

	// Close closes the stream.
	Close() error
}

// Ensure LogStream implements io.Closer.
var _ io.Closer = (LogStream)(nil)
