package store

import (
	"encoding/json"
	"time"
)

// Runner represents an execution environment.
type Runner struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Hostname           string          `json:"hostname"`
	Status             string          `json:"status"` // offline, idle, busy, paused
	Tainted            bool            `json:"tainted"`
	TaintReason        *string         `json:"taint_reason,omitempty"`
	SandboxMode        string          `json:"sandbox_mode"` // runner-is-sandbox, runner-creates-sandbox, none
	SandboxTypes       []string        `json:"sandbox_types"`
	ProviderConfigID   *string         `json:"provider_config_id,omitempty"`
	ProviderInstanceID *string         `json:"provider_instance_id,omitempty"`
	PoolName           *string         `json:"pool_name,omitempty"`
	ProfileID          *string         `json:"profile_id,omitempty"`
	Capabilities       []string        `json:"capabilities"`
	TenantID           *string         `json:"tenant_id,omitempty"`
	Labels             json.RawMessage `json:"labels"`
	Annotations        json.RawMessage `json:"annotations"`
	LastSeenAt         *time.Time      `json:"last_seen_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// RunnerUpdates contains fields that can be updated on a runner.
type RunnerUpdates struct {
	Name               *string
	Hostname           *string
	Status             *string
	Tainted            *bool
	TaintReason        *string
	SandboxMode        *string
	SandboxTypes       []string
	ProviderConfigID   *string
	ProviderInstanceID *string
	PoolName           *string
	ProfileID          *string
	Capabilities       []string
	Labels             json.RawMessage
	Annotations        json.RawMessage
	LastSeenAt         *time.Time
}

// Workspace represents persistent storage for a session.
type Workspace struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Persist          bool            `json:"persist"`
	StorageType      string          `json:"storage_type"` // volume
	StorageConfig    json.RawMessage `json:"storage_config"`
	Mobility         string          `json:"mobility"` // local, shared, object_sync
	StorageDomain    *string         `json:"storage_domain,omitempty"`
	StorageKey       *string         `json:"storage_key,omitempty"`
	StorageSizeBytes *int64          `json:"storage_size_bytes,omitempty"`
	LastSyncedAt     *time.Time      `json:"last_synced_at,omitempty"`
	DiskQuotaMB      *int            `json:"disk_quota_mb,omitempty"`
	TenantID         *string         `json:"tenant_id,omitempty"`
	Labels           json.RawMessage `json:"labels"`
	Annotations      json.RawMessage `json:"annotations"`
	ExpiresAt        *time.Time      `json:"expires_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        *time.Time      `json:"deleted_at,omitempty"`
}

// WorkspaceUpdates contains fields that can be updated on a workspace.
type WorkspaceUpdates struct {
	Name             *string
	Persist          *bool
	StorageConfig    json.RawMessage
	Mobility         *string
	StorageDomain    *string
	StorageKey       *string
	StorageSizeBytes *int64
	LastSyncedAt     *time.Time
	DiskQuotaMB      *int
	Labels           json.RawMessage
	Annotations      json.RawMessage
	ExpiresAt        *time.Time
	DeletedAt        *time.Time
}

// Session represents a long-lived work context.
type Session struct {
	ID                     string          `json:"id"`
	Name                   *string         `json:"name,omitempty"`
	Status                 string          `json:"status"` // pending, active, suspended, resuming, terminated
	RunnerID               *string         `json:"runner_id,omitempty"`
	WorkspaceID            string          `json:"workspace_id"`
	Agent                  string          `json:"agent"`
	IsBYOK                 bool            `json:"is_byok"`
	AgentConfigID          *string         `json:"agent_config_id,omitempty"`
	AgentConfigMetadata    json.RawMessage `json:"agent_config_metadata,omitempty"`
	ContextSnapshot        json.RawMessage `json:"context_snapshot,omitempty"`
	AgentVersion           *string         `json:"agent_version,omitempty"`
	SuspendStrategy        *string         `json:"suspend_strategy,omitempty"`
	SuspendSnapshotID      *string         `json:"suspend_snapshot_id,omitempty"`
	SuspendWorkspaceSynced *bool           `json:"suspend_workspace_synced,omitempty"`
	PreviousRunnerID       *string         `json:"previous_runner_id,omitempty"`
	NetworkPolicy          string          `json:"network_policy"` // none, allow_list, proxy, air_gapped
	AllowedHosts           []string        `json:"allowed_hosts"`
	LifecycleMode          string          `json:"lifecycle_mode"` // on_demand, always_on, scheduled
	IdleTimeoutSeconds     *int            `json:"idle_timeout_seconds,omitempty"`
	MaxLifetimeSeconds     *int            `json:"max_lifetime_seconds,omitempty"`
	ScheduleCron           *string         `json:"schedule_cron,omitempty"`
	ScheduleTimezone       *string         `json:"schedule_timezone,omitempty"`
	NextScheduledAt        *time.Time      `json:"next_scheduled_at,omitempty"`
	TenantID               *string         `json:"tenant_id,omitempty"`
	Labels                 json.RawMessage `json:"labels"`
	Annotations            json.RawMessage `json:"annotations"`
	LastActivityAt         *time.Time      `json:"last_activity_at,omitempty"`
	SuspendedAt            *time.Time      `json:"suspended_at,omitempty"`
	ResumedAt              *time.Time      `json:"resumed_at,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// SessionUpdates contains fields that can be updated on a session.
type SessionUpdates struct {
	Name                   *string
	Status                 *string
	RunnerID               *string
	AgentConfigID          *string
	AgentConfigMetadata    json.RawMessage
	ContextSnapshot        json.RawMessage
	AgentVersion           *string
	SuspendStrategy        *string
	SuspendSnapshotID      *string
	SuspendWorkspaceSynced *bool
	PreviousRunnerID       *string
	NetworkPolicy          *string
	AllowedHosts           []string
	LifecycleMode          *string
	IdleTimeoutSeconds     *int
	MaxLifetimeSeconds     *int
	ScheduleCron           *string
	ScheduleTimezone       *string
	NextScheduledAt        *time.Time
	Labels                 json.RawMessage
	Annotations            json.RawMessage
	LastActivityAt         *time.Time
	SuspendedAt            *time.Time
	ResumedAt              *time.Time
}

// Task represents a unit of work.
type Task struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"session_id"`
	Prompt         string          `json:"prompt"`
	Status         string          `json:"status"` // pending, running, completed, failed, canceled
	MaxRetries     int             `json:"max_retries"`
	RetryCount     int             `json:"retry_count"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	TenantID       *string         `json:"tenant_id,omitempty"`
	Labels         json.RawMessage `json:"labels"`
	Annotations    json.RawMessage `json:"annotations"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// TaskUpdates contains fields that can be updated on a task.
type TaskUpdates struct {
	Status      *string
	RetryCount  *int
	Labels      json.RawMessage
	Annotations json.RawMessage
}

// TaskRun represents an execution attempt of a task.
type TaskRun struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	Attempt      int        `json:"attempt"`
	RunnerID     *string    `json:"runner_id,omitempty"`
	Status       string     `json:"status"` // pending, assigned, running, completed, failed, timeout, canceled
	Error        *string    `json:"error,omitempty"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	TokensInput  *int       `json:"tokens_input,omitempty"`
	TokensOutput *int       `json:"tokens_output,omitempty"`
	TenantID     *string    `json:"tenant_id,omitempty"`
	QueuedAt     time.Time  `json:"queued_at"`
	AssignedAt   *time.Time `json:"assigned_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TaskRunUpdates contains fields that can be updated on a task run.
type TaskRunUpdates struct {
	RunnerID     *string
	Status       *string
	Error        *string
	ExitCode     *int
	TokensInput  *int
	TokensOutput *int
	AssignedAt   *time.Time
	StartedAt    *time.Time
	EndedAt      *time.Time
}

// ScheduledTask represents a recurring task with cron schedule.
type ScheduledTask struct {
	ID                     string          `json:"id"`
	SessionID              string          `json:"session_id"`
	Name                   string          `json:"name"`
	Description            *string         `json:"description,omitempty"`
	CronExpression         string          `json:"cron_expression"`
	Timezone               string          `json:"timezone"`
	PromptTemplate         string          `json:"prompt_template"`
	TimeoutSeconds         int             `json:"timeout_seconds"`
	MaxRetries             int             `json:"max_retries"`
	Status                 string          `json:"status"` // active, paused, disabled
	NextRunAt              *time.Time      `json:"next_run_at,omitempty"`
	LastRunAt              *time.Time      `json:"last_run_at,omitempty"`
	LastTaskID             *string         `json:"last_task_id,omitempty"`
	RunCount               int             `json:"run_count"`
	FailureCount           int             `json:"failure_count"`
	OnFailure              string          `json:"on_failure"` // continue, pause_on_failure, disable_on_failure
	MaxConsecutiveFailures *int            `json:"max_consecutive_failures,omitempty"`
	ConsecutiveFailures    int             `json:"consecutive_failures"`
	TenantID               *string         `json:"tenant_id,omitempty"`
	Labels                 json.RawMessage `json:"labels"`
	Annotations            json.RawMessage `json:"annotations"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// ScheduledTaskUpdates contains fields that can be updated on a scheduled task.
type ScheduledTaskUpdates struct {
	Name                   *string
	Description            *string
	CronExpression         *string
	Timezone               *string
	PromptTemplate         *string
	TimeoutSeconds         *int
	MaxRetries             *int
	Status                 *string
	NextRunAt              *time.Time
	LastRunAt              *time.Time
	LastTaskID             *string
	RunCount               *int
	FailureCount           *int
	OnFailure              *string
	MaxConsecutiveFailures *int
	ConsecutiveFailures    *int
	Labels                 json.RawMessage
	Annotations            json.RawMessage
}

// PermissionRequest represents an async permission approval request.
type PermissionRequest struct {
	ID                  string     `json:"id"`
	SessionID           string     `json:"session_id"`
	TaskID              string     `json:"task_id"`
	RunID               string     `json:"run_id"`
	Tool                string     `json:"tool"`
	Action              string     `json:"action"`
	Context             *string    `json:"context,omitempty"`
	RiskLevel           string     `json:"risk_level"` // low, medium, high, critical
	Status              string     `json:"status"`     // pending, approved, denied, canceled
	SuspendAfterSeconds int        `json:"suspend_after_seconds"`
	RespondedBy         *string    `json:"responded_by,omitempty"`
	ResponseReason      *string    `json:"response_reason,omitempty"`
	RespondedAt         *time.Time `json:"responded_at,omitempty"`
	TenantID            *string    `json:"tenant_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// PermissionRequestUpdates contains fields that can be updated on a permission request.
type PermissionRequestUpdates struct {
	Status         *string
	RespondedBy    *string
	ResponseReason *string
	RespondedAt    *time.Time
}

// APIKey represents an API authentication key.
type APIKey struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	KeyHash      string          `json:"-"` // Never expose in JSON
	KeyPrefix    string          `json:"key_prefix"`
	HashVersion  int             `json:"hash_version"`
	Scopes       []string        `json:"scopes"`
	TenantID     *string         `json:"tenant_id,omitempty"`
	Labels       json.RawMessage `json:"labels"`
	Annotations  json.RawMessage `json:"annotations"`
	CreatedAt    time.Time       `json:"created_at"`
	CreatedBy    *string         `json:"created_by,omitempty"`
	LastUsedAt   *time.Time      `json:"last_used_at,omitempty"`
	ExpiresAt    *time.Time      `json:"expires_at,omitempty"`
	RevokedAt    *time.Time      `json:"revoked_at,omitempty"`
	RevokeReason *string         `json:"revoke_reason,omitempty"`
}

// APIKeyUpdates contains fields that can be updated on an API key.
type APIKeyUpdates struct {
	Name         *string
	Scopes       []string
	Labels       json.RawMessage
	Annotations  json.RawMessage
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
	RevokeReason *string
}

// RunnerToken represents authentication for pool runners.
type RunnerToken struct {
	ID                string          `json:"id"`
	TokenHash         string          `json:"-"` // Never expose in JSON
	TokenPrefix       string          `json:"token_prefix"`
	HashVersion       int             `json:"hash_version"`
	RunnerID          *string         `json:"runner_id,omitempty"`
	PoolName          string          `json:"pool_name"`
	Status            string          `json:"status"` // active, rotating, revoked, expired
	PreviousTokenHash *string         `json:"-"`      // Never expose in JSON
	RotationDeadline  *time.Time      `json:"rotation_deadline,omitempty"`
	TenantID          *string         `json:"tenant_id,omitempty"`
	Labels            json.RawMessage `json:"labels"`
	CreatedAt         time.Time       `json:"created_at"`
	CreatedBy         *string         `json:"created_by,omitempty"`
	LastUsedAt        *time.Time      `json:"last_used_at,omitempty"`
	ExpiresAt         *time.Time      `json:"expires_at,omitempty"`
	RevokedAt         *time.Time      `json:"revoked_at,omitempty"`
	RevokeReason      *string         `json:"revoke_reason,omitempty"`
}

// RunnerTokenUpdates contains fields that can be updated on a runner token.
type RunnerTokenUpdates struct {
	RunnerID          *string
	Status            *string
	PreviousTokenHash *string
	RotationDeadline  *time.Time
	Labels            json.RawMessage
	LastUsedAt        *time.Time
	RevokedAt         *time.Time
	RevokeReason      *string
}

// AgentConfig represents configuration for an AI agent.
type AgentConfig struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Agent           string          `json:"agent"`
	APIKeyEncrypted string          `json:"-"` // Never expose in JSON
	Model           *string         `json:"model,omitempty"`
	BaseURL         *string         `json:"base_url,omitempty"`
	Extra           json.RawMessage `json:"extra"`
	IsDefault       bool            `json:"is_default"`
	TenantID        *string         `json:"tenant_id,omitempty"`
	Labels          json.RawMessage `json:"labels"`
	Annotations     json.RawMessage `json:"annotations"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// AgentConfigUpdates contains fields that can be updated on an agent config.
type AgentConfigUpdates struct {
	Name            *string
	APIKeyEncrypted *string
	Model           *string
	BaseURL         *string
	Extra           json.RawMessage
	IsDefault       *bool
	Labels          json.RawMessage
	Annotations     json.RawMessage
}

// ProviderConfig represents configuration for a runner provider.
type ProviderConfig struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Provider      string          `json:"provider"`
	Config        json.RawMessage `json:"config"`
	SuspendConfig json.RawMessage `json:"suspend_config"`
	IsDefault     bool            `json:"is_default"`
	TenantID      *string         `json:"tenant_id,omitempty"`
	Labels        json.RawMessage `json:"labels"`
	Annotations   json.RawMessage `json:"annotations"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ProviderConfigUpdates contains fields that can be updated on a provider config.
type ProviderConfigUpdates struct {
	Name          *string
	Config        json.RawMessage
	SuspendConfig json.RawMessage
	IsDefault     *bool
	Labels        json.RawMessage
	Annotations   json.RawMessage
}

// Profile represents a resource and network configuration template.
type Profile struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	ProviderConfigID *string         `json:"provider_config_id,omitempty"`
	TenantID         *string         `json:"tenant_id,omitempty"`
	Resources        json.RawMessage `json:"resources"`
	Network          json.RawMessage `json:"network"`
	InitScript       *string         `json:"init_script,omitempty"`
	CleanupScript    *string         `json:"cleanup_script,omitempty"`
	Tunnels          json.RawMessage `json:"tunnels"`
	Selector         json.RawMessage `json:"selector"`
	Labels           json.RawMessage `json:"labels"`
	Annotations      json.RawMessage `json:"annotations"`
	IsBuiltin        bool            `json:"is_builtin"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// ProfileUpdates contains fields that can be updated on a profile.
type ProfileUpdates struct {
	Name             *string
	Description      *string
	ProviderConfigID *string
	Resources        json.RawMessage
	Network          json.RawMessage
	InitScript       *string
	CleanupScript    *string
	Tunnels          json.RawMessage
	Selector         json.RawMessage
	Labels           json.RawMessage
	Annotations      json.RawMessage
}

// Snapshot represents a runner state snapshot.
type Snapshot struct {
	ID                 string          `json:"id"`
	RunnerID           string          `json:"runner_id"`
	Name               string          `json:"name"`
	ProviderSnapshotID string          `json:"provider_snapshot_id"`
	StorageKey         *string         `json:"storage_key,omitempty"`
	TenantID           *string         `json:"tenant_id,omitempty"`
	SizeBytes          *int64          `json:"size_bytes,omitempty"`
	Labels             json.RawMessage `json:"labels"`
	Annotations        json.RawMessage `json:"annotations"`
	CreatedAt          time.Time       `json:"created_at"`
	ExpiresAt          *time.Time      `json:"expires_at,omitempty"`
}

// SnapshotUpdates contains fields that can be updated on a snapshot.
type SnapshotUpdates struct {
	Name        *string
	StorageKey  *string
	SizeBytes   *int64
	Labels      json.RawMessage
	Annotations json.RawMessage
	ExpiresAt   *time.Time
}

// Tunnel represents a network tunnel for port forwarding or streaming.
type Tunnel struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	RunnerID    *string    `json:"runner_id,omitempty"`
	Type        string     `json:"type"`      // http, tcp, desktop, browser, ios, android
	Direction   string     `json:"direction"` // inbound, outbound
	LocalPort   int        `json:"local_port"`
	PublicURL   *string    `json:"public_url,omitempty"`
	TokenHash   string     `json:"-"` // Never expose in JSON
	TokenPrefix string     `json:"token_prefix"`
	HashVersion int        `json:"hash_version"`
	TenantID    *string    `json:"tenant_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
}

// TunnelUpdates contains fields that can be updated on a tunnel.
type TunnelUpdates struct {
	RunnerID  *string
	PublicURL *string
	ClosedAt  *time.Time
}

// ActionLog represents an audit log entry.
type ActionLog struct {
	ID           string          `json:"id"`
	ActorType    string          `json:"actor_type"` // user, api_key, system, runner
	ActorID      *string         `json:"actor_id,omitempty"`
	ActorName    *string         `json:"actor_name,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	SessionID    *string         `json:"session_id,omitempty"`
	TaskID       *string         `json:"task_id,omitempty"`
	Details      json.RawMessage `json:"details"`
	IPAddress    *string         `json:"ip_address,omitempty"`
	UserAgent    *string         `json:"user_agent,omitempty"`
	Success      bool            `json:"success"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	TenantID     *string         `json:"tenant_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// Log represents a task execution log entry.
type Log struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	TaskID    string          `json:"task_id"`
	RunID     string          `json:"run_id"`
	RunnerID  string          `json:"runner_id"`
	Stream    string          `json:"stream"` // stdout, stderr, system
	Level     string          `json:"level"`  // debug, info, warn, error
	Content   string          `json:"content"`
	Sequence  int64           `json:"sequence"`
	TenantID  *string         `json:"tenant_id,omitempty"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

// LogArchive represents an archived log storage record.
type LogArchive struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"session_id"`
	TenantID         *string    `json:"tenant_id,omitempty"`
	StorageKey       string     `json:"storage_key"`
	StorageSizeBytes *int64     `json:"storage_size_bytes,omitempty"`
	LogCount         int64      `json:"log_count"`
	FirstLogAt       *time.Time `json:"first_log_at,omitempty"`
	LastLogAt        *time.Time `json:"last_log_at,omitempty"`
	ArchivedAt       time.Time  `json:"archived_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

// LogArchiveUpdates contains fields that can be updated on a log archive.
type LogArchiveUpdates struct {
	DeletedAt *time.Time
}

// DataKey represents an encryption key record.
type DataKey struct {
	ID           string     `json:"id"`
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	DEKEncrypted string     `json:"-"` // Never expose in JSON
	Algorithm    string     `json:"algorithm"`
	KEKID        *string    `json:"kek_id,omitempty"`
	TenantID     *string    `json:"tenant_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	RotatedAt    *time.Time `json:"rotated_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// DataKeyUpdates contains fields that can be updated on a data key.
type DataKeyUpdates struct {
	DEKEncrypted *string
	RotatedAt    *time.Time
}

// Chunk represents a content-addressed storage chunk.
type Chunk struct {
	Hash      string     `json:"hash"`
	TenantID  string     `json:"tenant_id"`
	Size      int64      `json:"size"`
	RefCount  int        `json:"ref_count"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ChunkUpdates contains fields that can be updated on a chunk.
type ChunkUpdates struct {
	RefCount  *int
	DeletedAt *time.Time
}

// Manifest represents a workspace snapshot manifest.
type Manifest struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	ParentID    *string         `json:"parent_id,omitempty"`
	TotalSize   int64           `json:"total_size"`
	SingleChunk bool            `json:"single_chunk"`
	ChunkHash   *string         `json:"chunk_hash,omitempty"`
	ChunkCount  int             `json:"chunk_count"`
	FilesJSON   json.RawMessage `json:"files_json,omitempty"`
	TenantID    string          `json:"tenant_id"`
	CreatedAt   time.Time       `json:"created_at"`
}
