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
}
