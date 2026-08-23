package client

// CreateSessionOptions contains options for creating a session.
type CreateSessionOptions struct {
	// Name is the optional session name.
	Name string

	// Agent is the agent type (e.g., "claude", "codex").
	Agent string

	// AgentConfigID is the ID of a stored agent config to use.
	// Mutually exclusive with APIKey.
	AgentConfigID string

	// APIKey is the API key for BYOK mode.
	// Mutually exclusive with AgentConfigID.
	APIKey string

	// ProfileID is the ID of a profile to use for runner configuration.
	// If set, the profile's resources, network, and tunnel settings will be applied.
	ProfileID string

	// LifecycleMode is the session lifecycle mode.
	// Valid values: "on_demand", "always_on", "scheduled".
	LifecycleMode string

	// IdleTimeoutSeconds is the idle timeout for on_demand sessions.
	IdleTimeoutSeconds int

	// Labels are key-value pairs for organizing sessions.
	Labels map[string]string
}

// ListSessionsOptions contains options for listing sessions.
type ListSessionsOptions struct {
	// Limit is the maximum number of sessions to return.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// Status filters sessions by status.
	Status []string

	// Agent filters sessions by agent type.
	Agent string

	// Labels filters sessions by labels.
	Labels map[string]string
}

// CreateTaskOptions contains options for creating a task.
type CreateTaskOptions struct {
	// SessionID is the ID of the session to create the task in.
	SessionID string

	// Prompt is the task prompt.
	Prompt string

	// ContinueFrom is the ID of a previous task to continue from.
	ContinueFrom string

	// TimeoutSeconds is the task timeout.
	TimeoutSeconds int

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// Labels are key-value pairs for organizing tasks.
	Labels map[string]string
}

// ListTasksOptions contains options for listing tasks.
type ListTasksOptions struct {
	// Limit is the maximum number of tasks to return.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// SessionID filters tasks by session.
	SessionID string

	// Status filters tasks by status.
	Status []string
}

// GetLogsOptions contains options for getting task logs.
type GetLogsOptions struct {
	// Follow enables streaming of new log entries.
	Follow bool

	// Tail limits output to the last N entries.
	Tail int

	// SinceSequence returns logs after this sequence number.
	SinceSequence int64

	// Archived selects which copy to read: "" for both (oldest first), "true"
	// for the archive alone, "false" for the rows still in the database.
	Archived string
}

// ListRunnersOptions contains options for listing runners.
type ListRunnersOptions struct {
	// Limit is the maximum number of runners to return.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// Status filters runners by status.
	Status []string

	// PoolName filters runners by pool name.
	PoolName string

	// Labels filters runners by labels.
	Labels map[string]string
}

// ListPermissionsOptions contains options for listing permission requests.
type ListPermissionsOptions struct {
	// Limit is the maximum number of permissions to return.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// SessionID filters permissions by session.
	SessionID string

	// TaskID filters permissions by task.
	TaskID string

	// Status filters permissions by status (pending, approved, denied).
	Status []string

	// RiskLevel filters permissions by risk level.
	RiskLevel []string
}

// CreateTunnelOptions contains options for creating a tunnel.
type CreateTunnelOptions struct {
	// SessionID is the ID of the session to create the tunnel for.
	SessionID string

	// Type is the tunnel type ("http" or "tcp").
	Type string

	// LocalPort is the port on the runner to tunnel.
	LocalPort int

	// Public indicates if the tunnel should be publicly accessible without authentication.
	Public bool
}

// ListTunnelsOptions contains options for listing tunnels.
type ListTunnelsOptions struct {
	// SessionID filters tunnels by session.
	SessionID string
}

// CreateScheduledTaskOptions contains options for creating a scheduled task.
type CreateScheduledTaskOptions struct {
	// SessionID is the ID of the session to associate the scheduled task with.
	SessionID string

	// Name is the name of the scheduled task.
	Name string

	// Description is an optional description of the scheduled task.
	Description string

	// CronExpression is the cron expression defining the schedule.
	CronExpression string

	// Timezone is the timezone for the cron expression (e.g., "America/Los_Angeles").
	// Defaults to "UTC" if not specified.
	Timezone string

	// PromptTemplate is the template for generating task prompts.
	// May contain {{.Date}}, {{.RunNumber}}, etc.
	PromptTemplate string

	// TimeoutSeconds is the timeout for each triggered task.
	TimeoutSeconds int

	// MaxRetries is the maximum number of retry attempts for each triggered task.
	MaxRetries int

	// OnFailure defines the failure handling policy.
	// Valid values: "continue", "pause_on_failure", "disable_on_failure".
	OnFailure string

	// MaxConsecutiveFailures is the number of consecutive failures before
	// taking action (for "disable_on_failure" policy).
	MaxConsecutiveFailures *int

	// Labels are key-value pairs for organizing scheduled tasks.
	Labels map[string]string
}

// ListScheduledTasksOptions contains options for listing scheduled tasks.
type ListScheduledTasksOptions struct {
	// Limit is the maximum number of scheduled tasks to return.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// SessionID filters scheduled tasks by session.
	SessionID string

	// Status filters scheduled tasks by status (active, paused, disabled).
	Status []string
}

// UpdateScheduledTaskOptions contains options for updating a scheduled task.
type UpdateScheduledTaskOptions struct {
	// Name is the updated name (if not nil).
	Name *string

	// Description is the updated description (if not nil).
	Description *string

	// CronExpression is the updated cron expression (if not nil).
	CronExpression *string

	// Timezone is the updated timezone (if not nil).
	Timezone *string

	// PromptTemplate is the updated prompt template (if not nil).
	PromptTemplate *string

	// TimeoutSeconds is the updated timeout (if not nil).
	TimeoutSeconds *int

	// MaxRetries is the updated max retries (if not nil).
	MaxRetries *int

	// OnFailure is the updated failure policy (if not nil).
	OnFailure *string

	// MaxConsecutiveFailures is the updated max consecutive failures (if not nil).
	MaxConsecutiveFailures *int

	// Labels are the updated labels (if not nil).
	Labels map[string]string
}
