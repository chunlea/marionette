// Package provider defines interfaces for managing runner lifecycle across
// different infrastructure backends (Docker, Kubernetes, E2B, etc.).
package provider

import (
	"context"
	"time"
)

// ProviderType represents the provider mode.
type ProviderType string

const (
	// ProviderTypeManaged indicates the server controls full runner lifecycle.
	// Examples: Docker, Kubernetes, E2B, Firecracker
	ProviderTypeManaged ProviderType = "managed"

	// ProviderTypePool indicates runners join a pool and server assigns work.
	// Examples: macOS Pool, Windows Pool, GPU Pool, Bare Metal
	ProviderTypePool ProviderType = "pool"

	// ProviderTypeExternal indicates manual one-off runner registration.
	// Examples: Self-hosted runners
	ProviderTypeExternal ProviderType = "external"
)

// SuspendStrategy defines how a session is suspended.
type SuspendStrategy string

const (
	// SuspendStrategyPause preserves memory, instant resume.
	// Supported by: Docker (pause), Firecracker, E2B
	// Cost: Still consumes memory while paused
	SuspendStrategyPause SuspendStrategy = "pause"

	// SuspendStrategySnapshot creates full VM/container snapshot.
	// Supported by: Firecracker, QEMU, Docker (CRIU)
	// Cost: Storage for snapshot, no compute
	SuspendStrategySnapshot SuspendStrategy = "snapshot"

	// SuspendStrategyTerminatePreserveStorage terminates but keeps storage.
	// Supported by: Kubernetes (PV), Docker (volume)
	// Cost: Only storage (PV/volume)
	SuspendStrategyTerminatePreserveStorage SuspendStrategy = "terminate_preserve_storage"

	// SuspendStrategyReleaseToPool releases runner back to pool.
	// Supported by: Pool providers (macOS, Windows, bare metal)
	// Cost: Only object storage (workspace synced)
	SuspendStrategyReleaseToPool SuspendStrategy = "release_to_pool"

	// SuspendStrategyTerminate is full termination (no resume possible).
	// Fallback when no other strategy available.
	SuspendStrategyTerminate SuspendStrategy = "terminate"
)

// InstanceStatus represents infrastructure/provider state.
type InstanceStatus string

const (
	// InstanceStatusPending indicates the runner is being provisioned.
	InstanceStatusPending InstanceStatus = "pending"

	// InstanceStatusRunning indicates the infrastructure is running.
	InstanceStatusRunning InstanceStatus = "running"

	// InstanceStatusPaused indicates the runner is suspended (memory preserved).
	InstanceStatusPaused InstanceStatus = "paused"

	// InstanceStatusStopped indicates the runner was intentionally stopped.
	InstanceStatusStopped InstanceStatus = "stopped"

	// InstanceStatusFailed indicates an error state.
	InstanceStatusFailed InstanceStatus = "failed"
)

// Provider manages Runner lifecycle on a specific infrastructure.
type Provider interface {
	// Name returns the provider config name (unique identifier).
	Name() string

	// Type returns the provider mode: managed, pool, or external.
	Type() ProviderType

	// Spawn creates a new Runner instance.
	Spawn(ctx context.Context, opts SpawnOptions) (*RunnerInstance, error)

	// Destroy terminates a Runner and cleans up resources.
	Destroy(ctx context.Context, runnerID string) error

	// Status returns the current status of a Runner.
	Status(ctx context.Context, runnerID string) (*RunnerStatus, error)

	// List returns all Runners managed by this provider.
	List(ctx context.Context) ([]*RunnerInstance, error)

	// Capabilities returns what this provider supports.
	Capabilities() ProviderCapabilities
}

// PausableProvider extends Provider with pause/resume capabilities.
// Pause preserves memory state and allows instant resume.
type PausableProvider interface {
	Provider

	// Pause suspends the runner, preserving memory state.
	Pause(ctx context.Context, runnerID string) error

	// Unpause resumes a paused runner.
	Unpause(ctx context.Context, runnerID string) error
}

// SuspendableProvider extends Provider with suspend/resume using configured strategy.
// This is the preferred interface for session suspend/resume operations.
type SuspendableProvider interface {
	Provider

	// Suspend suspends the runner using the configured or specified strategy.
	// Returns the actual strategy used (may differ from requested if fallback).
	Suspend(ctx context.Context, runnerID string, opts SuspendOptions) (*SuspendResult, error)

	// Resume restores a suspended runner.
	// For terminate_preserve_storage: spawns new runner with same storage.
	// For release_to_pool: acquires runner from pool and restores workspace.
	Resume(ctx context.Context, sessionID string, opts ResumeOptions) (*RunnerInstance, error)
}

// PoolAcquirer is implemented by pool providers to acquire runners from a pool.
// Unlike managed providers that spawn runners, pool providers acquire idle runners
// from a pre-registered pool of machines.
type PoolAcquirer interface {
	Provider

	// AcquireFromPool acquires an idle runner from the pool.
	// Returns the acquired runner info or an error if no suitable runner is available.
	AcquireFromPool(ctx context.Context, opts PoolAcquireOptions) (RunnerInfo, error)

	// ReleaseToPool releases a runner back to the pool.
	ReleaseToPool(ctx context.Context, runnerID string, tainted bool, taintReason string) error
}

// PoolAcquireOptions contains options for acquiring a runner from a pool.
type PoolAcquireOptions struct {
	// PreferRunnerID prefers a specific runner if available (for resume).
	PreferRunnerID string

	// RequiredLabels are labels the runner must have (from profile selector).
	RequiredLabels map[string]string

	// RequiredCapabilities are capabilities the runner must have.
	RequiredCapabilities []string

	// ExcludeRunnerIDs excludes specific runners from selection.
	ExcludeRunnerIDs []string

	// SessionID is the session acquiring the runner.
	SessionID string

	// ProfileID is the profile ID for logging/tracking purposes.
	ProfileID string
}

// RunnerInfo contains basic information about an acquired runner.
type RunnerInfo struct {
	ID   string
	Name string
}

// SnapshotProvider extends Provider with full VM/container snapshot capabilities.
// This is for providers that support creating and restoring named snapshots
// of a runner's complete state (memory, disk, network).
type SnapshotProvider interface {
	Provider

	// CreateSnapshot creates a named snapshot of the runner's current state.
	// The snapshot includes memory state, disk contents, and running processes.
	CreateSnapshot(ctx context.Context, runnerID string, opts CreateSnapshotOptions) (*SnapshotResult, error)

	// RestoreSnapshot creates a new runner from a snapshot.
	// The restored runner will have the same state as when the snapshot was taken.
	RestoreSnapshot(ctx context.Context, snapshotID string, opts RestoreSnapshotOptions) (*RunnerInstance, error)

	// ListSnapshots returns all snapshots for a runner.
	ListSnapshots(ctx context.Context, runnerID string) ([]*SnapshotInfo, error)

	// DeleteSnapshot removes a snapshot and frees associated storage.
	DeleteSnapshot(ctx context.Context, snapshotID string) error
}

// CreateSnapshotOptions configures snapshot creation.
type CreateSnapshotOptions struct {
	// Name is the human-readable snapshot name.
	Name string

	// Description provides additional context about the snapshot.
	Description string

	// Labels are key-value pairs for filtering and organization.
	Labels map[string]string

	// ExpiresAt sets when the snapshot should be automatically deleted.
	// Zero value means no automatic expiration.
	ExpiresAt time.Time

	// Timeout is the maximum time to wait for snapshot creation.
	Timeout time.Duration
}

// SnapshotResult contains the result of a snapshot creation.
type SnapshotResult struct {
	// ID is the snapshot ID (snap_xxx) for database reference.
	ID string

	// ProviderSnapshotID is the provider-specific snapshot identifier.
	ProviderSnapshotID string

	// Name is the snapshot name.
	Name string

	// SizeBytes is the size of the snapshot in bytes.
	SizeBytes int64

	// CreatedAt is when the snapshot was created.
	CreatedAt time.Time
}

// SnapshotInfo contains snapshot metadata for listing.
type SnapshotInfo struct {
	// ID is the snapshot ID (snap_xxx).
	ID string

	// ProviderSnapshotID is the provider-specific snapshot identifier.
	ProviderSnapshotID string

	// Name is the snapshot name.
	Name string

	// Description provides additional context about the snapshot.
	Description string

	// SizeBytes is the size of the snapshot in bytes.
	SizeBytes int64

	// CreatedAt is when the snapshot was created.
	CreatedAt time.Time

	// ExpiresAt is when the snapshot will be automatically deleted.
	ExpiresAt *time.Time

	// Labels are key-value pairs for filtering and organization.
	Labels map[string]string
}

// RestoreSnapshotOptions configures snapshot restoration.
type RestoreSnapshotOptions struct {
	// RunnerID specifies the ID for the restored runner.
	// If empty, a new ID will be generated.
	RunnerID string

	// Name is the human-readable name for the restored runner.
	Name string

	// Labels are key-value pairs for the restored runner.
	Labels map[string]string

	// Timeout is the maximum time to wait for restoration.
	Timeout time.Duration
}

// SuspendOptions contains parameters for suspend operation.
type SuspendOptions struct {
	// Strategy is the requested suspend strategy (uses provider default if empty).
	Strategy SuspendStrategy

	// SaveSnapshot creates a snapshot before suspend (if provider supports).
	SaveSnapshot bool

	// SyncWorkspace syncs workspace to object storage before suspend.
	SyncWorkspace bool

	// Timeout is the maximum time to wait for suspend to complete.
	Timeout time.Duration
}

// SuspendResult contains the result of a suspend operation.
type SuspendResult struct {
	// Strategy is the actual strategy used (may differ if fallback was needed).
	Strategy SuspendStrategy

	// SnapshotID is the ID of the snapshot created (if SaveSnapshot was true).
	SnapshotID string

	// WorkspaceSynced indicates if workspace was synced to object storage.
	WorkspaceSynced bool

	// SuspendedAt is when the suspend completed.
	SuspendedAt time.Time
}

// ResumeOptions contains parameters for resume operation.
type ResumeOptions struct {
	// RunnerID is the preferred runner ID (for pool, may get different one).
	RunnerID string

	// SnapshotID is the snapshot to restore from (if available).
	SnapshotID string

	// Timeout is the maximum time to wait for resume to complete.
	Timeout time.Duration

	// SpawnOptions contains options for spawning a new runner if needed.
	SpawnOpts *SpawnOptions
}

// ProviderCapabilities describes what the provider supports.
type ProviderCapabilities struct {
	// Pause indicates if the provider can pause/resume runners.
	Pause bool

	// Snapshot indicates if the provider can create/restore snapshots.
	Snapshot bool

	// Suspend describes suspend strategy support.
	Suspend SuspendCapability
}

// SuspendCapability describes supported suspend strategies.
type SuspendCapability struct {
	// Strategies lists supported strategies in order of preference.
	Strategies []SuspendStrategy

	// Default is the default strategy for this provider.
	Default SuspendStrategy
}

// SpawnOptions contains parameters for spawning a runner.
type SpawnOptions struct {
	// RunnerID is the pre-generated runner ID (run_xxx).
	RunnerID string

	// Name is the human-readable runner name.
	Name string

	// ServerURL is the Marionette server gRPC URL for the agent to connect to.
	ServerURL string

	// RunnerToken is the authentication token for the runner.
	RunnerToken string

	// Environment contains additional environment variables.
	Environment map[string]string

	// Labels are key-value pairs for filtering and organization.
	Labels map[string]string

	// Annotations are key-value pairs for storing metadata.
	Annotations map[string]string

	// SandboxMode specifies the sandbox configuration.
	// Values: "runner-is-sandbox", "runner-creates-sandbox", "none"
	SandboxMode string

	// SandboxTypes lists available sandbox types.
	SandboxTypes []string

	// MemoryMB is the memory limit in megabytes.
	MemoryMB int

	// CPUs is the CPU limit (e.g., 2.0 for 2 cores).
	CPUs float64

	// DiskMB is the disk quota in megabytes.
	DiskMB int

	// WorkspaceMount is the host path to mount as /workspace.
	WorkspaceMount string

	// TenantID is the tenant identifier for isolation.
	TenantID string

	// NetworkPolicy specifies the network isolation level.
	// Values: "none", "allow_list", "proxy", "air_gapped"
	NetworkPolicy string

	// AllowedHosts contains allowed host patterns for allow_list mode.
	// Supports wildcards: *.github.com, api.*.example.com
	AllowedHosts []string
}

// RunnerInstance represents a spawned runner.
type RunnerInstance struct {
	// ID is the runner ID (run_xxx).
	ID string

	// ProviderID is the provider-specific instance ID (e.g., container ID).
	ProviderID string

	// Name is the human-readable runner name.
	Name string

	// Status is the current infrastructure state.
	Status InstanceStatus

	// SandboxMode is the sandbox configuration used.
	SandboxMode string

	// CreatedAt is when the runner was created.
	CreatedAt time.Time

	// Labels are key-value pairs from spawn options.
	Labels map[string]string

	// Annotations are key-value pairs from spawn options.
	Annotations map[string]string

	// Metadata contains provider-specific information.
	Metadata map[string]string
}

// RunnerStatus contains current runner state.
type RunnerStatus struct {
	// Status is the current infrastructure state.
	Status InstanceStatus

	// TaskID is the current task ID if the runner is busy.
	TaskID string

	// UpdatedAt is when the status was last updated.
	UpdatedAt time.Time
}

// SuspendConfig configures suspend behavior for a provider.
type SuspendConfig struct {
	// Strategy is the suspend strategy to use.
	Strategy SuspendStrategy `json:"strategy"`

	// MinDuration prevents rapid suspend/resume cycles.
	MinDuration time.Duration `json:"min_duration,omitempty"`

	// MaxDuration auto-terminates after this time suspended.
	MaxDuration time.Duration `json:"max_duration,omitempty"`

	// Fallback is the strategy to use if primary fails.
	Fallback SuspendStrategy `json:"fallback,omitempty"`

	// SaveSnapshot creates a snapshot before suspend (if supported).
	SaveSnapshot bool `json:"save_snapshot,omitempty"`

	// SyncWorkspace forces workspace sync before suspend.
	SyncWorkspace bool `json:"sync_workspace,omitempty"`
}

// DefaultSuspendConfig returns the default suspend configuration.
func DefaultSuspendConfig() SuspendConfig {
	return SuspendConfig{
		Strategy:    SuspendStrategyPause,
		MinDuration: 60 * time.Second,
		MaxDuration: 24 * time.Hour,
		Fallback:    SuspendStrategyTerminatePreserveStorage,
	}
}
