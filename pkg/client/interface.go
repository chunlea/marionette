// Package client provides a client interface for the Marionette API.
package client

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// Session is an alias for store.Session.
type Session = store.Session

// Task is an alias for store.Task.
type Task = store.Task

// Log is an alias for store.Log.
type Log = store.Log

// ListResult is an alias for store.ListResult.
type ListResult[T any] = store.ListResult[T]

// Client provides access to the Marionette API.
type Client interface {
	// Sessions
	CreateSession(ctx context.Context, opts CreateSessionOptions) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error)
	SuspendSession(ctx context.Context, id string) error
	ResumeSession(ctx context.Context, id string) error
	TerminateSession(ctx context.Context, id string) error

	// Tasks
	CreateTask(ctx context.Context, opts CreateTaskOptions) (*Task, error)
	GetTask(ctx context.Context, id string) (*Task, error)
	ListTasks(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error)
	CancelTask(ctx context.Context, id string) error
	GetTaskLogs(ctx context.Context, id string, opts GetLogsOptions) (LogIterator, error)
}

// LogIterator allows iterating over log entries.
type LogIterator interface {
	// Next returns the next log entry.
	// Returns io.EOF when there are no more entries.
	Next() (*Log, error)

	// Close releases resources associated with the iterator.
	Close() error
}
