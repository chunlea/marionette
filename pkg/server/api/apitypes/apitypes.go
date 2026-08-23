// Package apitypes defines the wire contract of the public Marionette API.
//
// These types — and only these types — are what the API serializes. They are
// deliberately kept in a leaf package with no dependency on pkg/store or
// pkg/server: the server maps its internal models onto them, the Go SDK
// (pkg/client) aliases them, and the OpenAPI document is generated from them,
// so all three can never drift apart.
//
// Two rules govern what may appear here:
//
//   - Adding a column to the database must not change the API. Internal
//     bookkeeping (tenant_id, context snapshots, suspend snapshot ids, token
//     hashes, storage keys) is therefore absent by construction, not by a
//     `json:"-"` tag someone has to remember.
//   - No field is a raw JSON blob. Labels and annotations are string maps and
//     are always present, so clients never have to handle null.
package apitypes

import "time"

// ListResponse is the envelope every list endpoint returns.
type ListResponse[T any] struct {
	// Items holds the page of results. Never null; an empty page is [].
	Items []*T `json:"items"`
	// TotalCount is the number of items matching the filter, across all pages.
	TotalCount int64 `json:"total_count"`
	// HasMore reports whether another page follows this one.
	HasMore bool `json:"has_more"`
	// NextCursor is the cursor to pass to fetch the next page.
	NextCursor string `json:"next_cursor,omitempty"`
}

// ErrorResponse is the body of every non-2xx response.
type ErrorResponse struct {
	// Code is a stable, machine-readable error code.
	Code string `json:"code"`
	// Message is a human-readable description of what went wrong.
	Message string `json:"message"`
}

// Session is a long-lived work context that outlives individual runners.
type Session struct {
	ID           string  `json:"id"`
	Name         *string `json:"name,omitempty"`
	Status       string  `json:"status" enum:"pending,active,suspended,resuming,terminated"`
	Agent        string  `json:"agent"`
	AgentVersion *string `json:"agent_version,omitempty"`
	// AgentConfigID is the stored credential set this session runs with.
	// Empty for BYOK sessions.
	AgentConfigID *string `json:"agent_config_id,omitempty"`
	IsBYOK        bool    `json:"is_byok"`
	RunnerID      *string `json:"runner_id,omitempty"`
	// PreviousRunnerID is the runner this session ran on before the current
	// one, kept so a resume can prefer the runner that already has the
	// workspace warm.
	PreviousRunnerID *string `json:"previous_runner_id,omitempty"`
	WorkspaceID      string  `json:"workspace_id"`
	ProfileID        *string `json:"profile_id,omitempty"`

	NetworkPolicy string   `json:"network_policy" enum:"none,allow_list,proxy,air_gapped"`
	AllowedHosts  []string `json:"allowed_hosts"`

	LifecycleMode      string  `json:"lifecycle_mode" enum:"on_demand,always_on,scheduled"`
	IdleTimeoutSeconds *int    `json:"idle_timeout_seconds,omitempty"`
	MaxLifetimeSeconds *int    `json:"max_lifetime_seconds,omitempty"`
	SuspendStrategy    *string `json:"suspend_strategy,omitempty"`

	ScheduleCron     *string    `json:"schedule_cron,omitempty"`
	ScheduleTimezone *string    `json:"schedule_timezone,omitempty"`
	NextScheduledAt  *time.Time `json:"next_scheduled_at,omitempty"`

	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`

	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
	SuspendedAt    *time.Time `json:"suspended_at,omitempty"`
	ResumedAt      *time.Time `json:"resumed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Task is a unit of work executed inside a session.
type Task struct {
	ID             string            `json:"id"`
	SessionID      string            `json:"session_id"`
	Prompt         string            `json:"prompt"`
	Status         string            `json:"status" enum:"pending,running,completed,failed,canceled"`
	MaxRetries     int               `json:"max_retries"`
	RetryCount     int               `json:"retry_count"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Labels         map[string]string `json:"labels"`
	Annotations    map[string]string `json:"annotations"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// TaskRun is one execution attempt of a task.
type TaskRun struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	Attempt      int        `json:"attempt"`
	RunnerID     *string    `json:"runner_id,omitempty"`
	Status       string     `json:"status" enum:"pending,assigned,running,completed,failed,timeout,canceled"`
	Error        *string    `json:"error,omitempty"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	TokensInput  *int       `json:"tokens_input,omitempty"`
	TokensOutput *int       `json:"tokens_output,omitempty"`
	QueuedAt     time.Time  `json:"queued_at"`
	AssignedAt   *time.Time `json:"assigned_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Runner is an execution environment a session can attach to.
//
// Provider identifiers are deliberately absent: which docker daemon or
// Kubernetes namespace backs a runner is an operator concern served by the
// admin API, not part of the public contract.
type Runner struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Hostname     string            `json:"hostname"`
	Status       string            `json:"status" enum:"offline,idle,busy,paused"`
	Tainted      bool              `json:"tainted"`
	TaintReason  *string           `json:"taint_reason,omitempty"`
	SandboxMode  string            `json:"sandbox_mode" enum:"runner-is-sandbox,runner-creates-sandbox,none"`
	SandboxTypes []string          `json:"sandbox_types"`
	PoolName     *string           `json:"pool_name,omitempty"`
	ProfileID    *string           `json:"profile_id,omitempty"`
	Capabilities []string          `json:"capabilities"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	LastSeenAt   *time.Time        `json:"last_seen_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// PermissionRequest is an agent's request to perform a gated action.
type PermissionRequest struct {
	ID string `json:"id"`
	// OriginalRequestID is the agent-side identifier of the request, e.g. the
	// tool_use_id Claude Code sent.
	OriginalRequestID string  `json:"original_request_id"`
	SessionID         string  `json:"session_id"`
	TaskID            string  `json:"task_id"`
	RunID             string  `json:"run_id"`
	Tool              string  `json:"tool"`
	Action            string  `json:"action"`
	Context           *string `json:"context,omitempty"`
	RiskLevel         string  `json:"risk_level" enum:"low,medium,high,critical"`
	Status            string  `json:"status" enum:"pending,approved,denied,expired,canceled"`
	// SuspendAfterSeconds is how long the session waits for a response before
	// suspending. The request itself never expires.
	SuspendAfterSeconds int        `json:"suspend_after_seconds"`
	RespondedBy         *string    `json:"responded_by,omitempty"`
	ResponseReason      *string    `json:"response_reason,omitempty"`
	RespondedAt         *time.Time `json:"responded_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// Workspace is the persistent working directory behind a session.
//
// Where the bytes actually live (storage domain, key, backend config) is not
// part of the contract; size and quota are.
type Workspace struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Persist          bool              `json:"persist"`
	StorageType      string            `json:"storage_type"`
	Mobility         string            `json:"mobility" enum:"local,shared,object_sync"`
	StorageSizeBytes *int64            `json:"storage_size_bytes,omitempty"`
	DiskQuotaMB      *int              `json:"disk_quota_mb,omitempty"`
	LastSyncedAt     *time.Time        `json:"last_synced_at,omitempty"`
	Labels           map[string]string `json:"labels"`
	Annotations      map[string]string `json:"annotations"`
	ExpiresAt        *time.Time        `json:"expires_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	DeletedAt        *time.Time        `json:"deleted_at,omitempty"`
}

// ScheduledTask creates tasks on a cron schedule.
type ScheduledTask struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"session_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
	CronExpression string  `json:"cron_expression"`
	Timezone       string  `json:"timezone"`
	PromptTemplate string  `json:"prompt_template"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	MaxRetries     int     `json:"max_retries"`
	Status         string  `json:"status" enum:"active,paused,disabled"`

	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastTaskID *string    `json:"last_task_id,omitempty"`
	RunCount   int        `json:"run_count"`

	OnFailure              string `json:"on_failure" enum:"continue,pause_on_failure,disable_on_failure"`
	FailureCount           int    `json:"failure_count"`
	ConsecutiveFailures    int    `json:"consecutive_failures"`
	MaxConsecutiveFailures *int   `json:"max_consecutive_failures,omitempty"`

	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Tunnel forwards a port inside a runner out through the API server.
type Tunnel struct {
	ID        string  `json:"id"`
	SessionID string  `json:"session_id"`
	RunnerID  *string `json:"runner_id,omitempty"`
	Type      string  `json:"type" enum:"http,tcp,desktop,browser,ios,android"`
	Direction string  `json:"direction" enum:"inbound,outbound"`
	LocalPort int     `json:"local_port"`
	PublicURL *string `json:"public_url,omitempty"`
	// IsPublic reports whether the tunnel can be reached without a token.
	IsPublic bool `json:"is_public"`
	// Token is the tunnel's bearer token. It is returned exactly once, by the
	// create call, and is never readable again.
	Token     string     `json:"token,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// Log is one line of task output.
type Log struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	TaskID    string            `json:"task_id"`
	RunID     string            `json:"run_id"`
	RunnerID  string            `json:"runner_id"`
	Stream    string            `json:"stream" enum:"stdout,stderr,system"`
	Level     string            `json:"level" enum:"debug,info,warn,error"`
	Content   string            `json:"content"`
	Sequence  int64             `json:"sequence"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
}

// TaskExecutionAccepted is returned by the task execute endpoint.
type TaskExecutionAccepted struct {
	Status string `json:"status"`
}

// HealthStatus is returned by the unauthenticated health endpoints.
type HealthStatus struct {
	Status string `json:"status"`
}
