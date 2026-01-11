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

// Runner is an alias for store.Runner.
type Runner = store.Runner

// PermissionRequest is an alias for store.PermissionRequest.
type PermissionRequest = store.PermissionRequest

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

	// Runners (read-only)
	GetRunner(ctx context.Context, id string) (*Runner, error)
	ListRunners(ctx context.Context, opts ListRunnersOptions) (*ListResult[Runner], error)

	// Permissions
	GetPermission(ctx context.Context, id string) (*PermissionRequest, error)
	ListPermissions(ctx context.Context, opts ListPermissionsOptions) (*ListResult[PermissionRequest], error)
	ApprovePermission(ctx context.Context, id string, reason string) error
	DenyPermission(ctx context.Context, id string, reason string) error

	// Tunnels
	CreateTunnel(ctx context.Context, opts CreateTunnelOptions) (*Tunnel, error)
	GetTunnel(ctx context.Context, id string) (*Tunnel, error)
	ListTunnels(ctx context.Context, opts ListTunnelsOptions) (*ListResult[Tunnel], error)
	CloseTunnel(ctx context.Context, id string) error
}

// LogIterator allows iterating over log entries.
type LogIterator interface {
	// Next returns the next log entry.
	// Returns io.EOF when there are no more entries.
	Next() (*Log, error)

	// Close releases resources associated with the iterator.
	Close() error
}
