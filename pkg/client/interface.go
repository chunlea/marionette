// Package client provides a client interface for the Marionette API.
package client

import (
	"context"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// The SDK's resource types are aliases of the API's wire contract, not of the
// database models. Aliasing pkg/store meant the SDK promised fields the server
// does not send (and vice versa) with nothing to catch the difference;
// apitypes is the same package the server serializes and the OpenAPI document
// is generated from, so the three cannot drift.

// Session is an alias for apitypes.Session.
type Session = apitypes.Session

// Task is an alias for apitypes.Task.
type Task = apitypes.Task

// TaskRun is an alias for apitypes.TaskRun.
type TaskRun = apitypes.TaskRun

// Log is an alias for apitypes.Log.
type Log = apitypes.Log

// Runner is an alias for apitypes.Runner.
type Runner = apitypes.Runner

// PermissionRequest is an alias for apitypes.PermissionRequest.
type PermissionRequest = apitypes.PermissionRequest

// Workspace is an alias for apitypes.Workspace.
type Workspace = apitypes.Workspace

// ScheduledTask is an alias for apitypes.ScheduledTask.
type ScheduledTask = apitypes.ScheduledTask

// Tunnel is an alias for apitypes.Tunnel.
type Tunnel = apitypes.Tunnel

// ListResult is an alias for the API's list envelope. The admin API answers
// with the same envelope, so one type serves both.
type ListResult[T any] = apitypes.ListResponse[T]

// The admin API's resources. These alias admintypes for the same reason the
// public ones alias apitypes: the SDK used to name pkg/store here, so it
// promised callers columns the server withholds — every hash and encrypted
// blob among them.

// APIKey is an alias for admintypes.APIKey.
type APIKey = admintypes.APIKey

// RunnerToken is an alias for admintypes.RunnerToken.
type RunnerToken = admintypes.RunnerToken

// AgentConfig is an alias for admintypes.AgentConfig.
type AgentConfig = admintypes.AgentConfig

// ProviderConfig is an alias for admintypes.ProviderConfig.
type ProviderConfig = admintypes.ProviderConfig

// Profile is an alias for admintypes.Profile.
type Profile = admintypes.Profile

// AdminRunner is the operator's view of a runner, which unlike the public
// Runner names the provider behind it.
type AdminRunner = admintypes.Runner

// Webhook is an alias for admintypes.Webhook.
type Webhook = admintypes.Webhook

// WebhookEvent is an alias for admintypes.WebhookEvent.
type WebhookEvent = admintypes.WebhookEvent

// ActionLog is an alias for admintypes.ActionLog.
type ActionLog = admintypes.ActionLog

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

	// GetSessionLogs returns every log line a session produced, archived and
	// live alike.
	GetSessionLogs(ctx context.Context, id string, opts GetLogsOptions) (LogIterator, error)

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

	// Scheduled Tasks
	CreateScheduledTask(ctx context.Context, opts CreateScheduledTaskOptions) (*ScheduledTask, error)
	GetScheduledTask(ctx context.Context, id string) (*ScheduledTask, error)
	ListScheduledTasks(ctx context.Context, opts ListScheduledTasksOptions) (*ListResult[ScheduledTask], error)
	UpdateScheduledTask(ctx context.Context, id string, opts UpdateScheduledTaskOptions) (*ScheduledTask, error)
	DeleteScheduledTask(ctx context.Context, id string) error
	PauseScheduledTask(ctx context.Context, id string) (*ScheduledTask, error)
	ResumeScheduledTask(ctx context.Context, id string) (*ScheduledTask, error)
	TriggerScheduledTask(ctx context.Context, id string) (*Task, error)
}

// LogIterator allows iterating over log entries.
type LogIterator interface {
	// Next returns the next log entry.
	// Returns io.EOF when there are no more entries.
	Next() (*Log, error)

	// Close releases resources associated with the iterator.
	Close() error
}
