package store

// ListResult wraps paginated results with metadata.
type ListResult[T any] struct {
	Items      []*T   `json:"items"`
	TotalCount int64  `json:"total_count"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// BaseListOptions contains common pagination and ordering fields.
type BaseListOptions struct {
	Limit     int    // Max items to return (default: 50, max: 1000)
	Cursor    string // Cursor for pagination (opaque string)
	OrderBy   string // Field to order by (default varies by entity)
	OrderDesc bool   // Order descending (default: false)
}

// ListRunnersOptions for filtering runners.
type ListRunnersOptions struct {
	BaseListOptions
	Status   []string          // Filter by status (offline, idle, busy, paused)
	PoolName *string           // Filter by pool name
	Tainted  *bool             // Filter by tainted status
	Labels   map[string]string // Filter by labels (AND matching)
}

// ListSessionsOptions for filtering sessions.
type ListSessionsOptions struct {
	BaseListOptions
	Status        []string          // Filter by status
	RunnerID      *string           // Filter by attached runner
	WorkspaceID   *string           // Filter by workspace
	Agent         *string           // Filter by agent type
	LifecycleMode *string           // Filter by lifecycle mode
	Labels        map[string]string // Filter by labels
}

// ListTasksOptions for filtering tasks.
type ListTasksOptions struct {
	BaseListOptions
	SessionID *string  // Filter by session
	Status    []string // Filter by status
}

// ListTaskRunsOptions for filtering task runs.
type ListTaskRunsOptions struct {
	BaseListOptions
	TaskID   *string  // Filter by task
	RunnerID *string  // Filter by runner
	Status   []string // Filter by status
}

// ListScheduledTasksOptions for filtering scheduled tasks.
type ListScheduledTasksOptions struct {
	BaseListOptions
	SessionID *string  // Filter by session
	Status    []string // Filter by status (active, paused, disabled)
}

// ListWorkspacesOptions for filtering workspaces.
type ListWorkspacesOptions struct {
	BaseListOptions
	IncludeDeleted bool              // Include soft-deleted workspaces
	Labels         map[string]string // Filter by labels
}

// ListPermissionRequestsOptions for filtering permission requests.
type ListPermissionRequestsOptions struct {
	BaseListOptions
	SessionID *string  // Filter by session
	TaskID    *string  // Filter by task
	RunID     *string  // Filter by task run
	Status    []string // Filter by status (pending, approved, denied, canceled)
	RiskLevel []string // Filter by risk level
}

// ListAPIKeysOptions for filtering API keys.
type ListAPIKeysOptions struct {
	BaseListOptions
	IncludeRevoked bool              // Include revoked keys
	Labels         map[string]string // Filter by labels
}

// ListRunnerTokensOptions for filtering runner tokens.
type ListRunnerTokensOptions struct {
	BaseListOptions
	PoolName       *string  // Filter by pool name
	RunnerID       *string  // Filter by runner
	Status         []string // Filter by status
	IncludeRevoked bool     // Include revoked tokens
}

// ListAgentConfigsOptions for filtering agent configs.
type ListAgentConfigsOptions struct {
	BaseListOptions
	Agent *string // Filter by agent type
}

// ListProviderConfigsOptions for filtering provider configs.
type ListProviderConfigsOptions struct {
	BaseListOptions
	Provider *string // Filter by provider type
}

// ListProfilesOptions for filtering profiles.
type ListProfilesOptions struct {
	BaseListOptions
	ProviderConfigID *string // Filter by provider config
	IncludeBuiltin   bool    // Include built-in profiles
}

// ListSnapshotsOptions for filtering snapshots.
type ListSnapshotsOptions struct {
	BaseListOptions
	RunnerID *string // Filter by runner
}

// ListTunnelsOptions for filtering tunnels.
type ListTunnelsOptions struct {
	BaseListOptions
	SessionID     *string  // Filter by session
	RunnerID      *string  // Filter by runner
	Type          []string // Filter by tunnel type
	Direction     *string  // Filter by direction (inbound, outbound)
	IncludeClosed bool     // Include closed tunnels
}

// ListActionLogsOptions for filtering action logs.
type ListActionLogsOptions struct {
	BaseListOptions
	ActorType    *string // Filter by actor type
	ActorID      *string // Filter by actor ID
	Action       *string // Filter by action
	ResourceType *string // Filter by resource type
	ResourceID   *string // Filter by resource ID
	SessionID    *string // Filter by session
	TaskID       *string // Filter by task
	Success      *bool   // Filter by success status
}

// ListLogsOptions for filtering task logs.
type ListLogsOptions struct {
	BaseListOptions
	SessionID *string  // Filter by session
	TaskID    *string  // Filter by task
	RunID     *string  // Filter by task run
	RunnerID  *string  // Filter by runner
	Stream    []string // Filter by stream (stdout, stderr, system)
	Level     []string // Filter by level (debug, info, warn, error)
}

// ListLogArchivesOptions for filtering log archives.
type ListLogArchivesOptions struct {
	BaseListOptions
	IncludeDeleted bool // Include soft-deleted archives
}
