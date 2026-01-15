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

// Tunnel represents a tunnel.
type Tunnel struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	RunnerID  string `json:"runner_id"`
	Type      string `json:"type"`
	LocalPort int    `json:"local_port"`
	IsPublic  bool   `json:"is_public"`
	Token     string `json:"token,omitempty"`
	PublicURL string `json:"public_url"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}
