# Provider Interface

## Provider interface

```go
// Provider manages Runner lifecycle on a specific infrastructure
type Provider interface {
    // Name returns the provider type (e.g., "docker", "kubernetes", "e2b")
    Name() string
    
    // Type returns the provider mode: "managed", "pool", or "external"
    Type() ProviderType
    
    // Spawn creates a new Runner instance (managed providers only)
    Spawn(ctx context.Context, opts SpawnOptions) (*RunnerInstance, error)
    
    // Destroy terminates a Runner and cleans up resources
    Destroy(ctx context.Context, runnerID string) error
    
    // Status returns the current status of a Runner
    Status(ctx context.Context, runnerID string) (*RunnerStatus, error)
    
    // List returns all Runners managed by this provider
    List(ctx context.Context) ([]*RunnerInstance, error)
    
    // Capabilities returns what this provider supports
    Capabilities() ProviderCapabilities
}

type ProviderType string

const (
    ProviderTypeManaged  ProviderType = "managed"  // Server controls lifecycle
    ProviderTypePool     ProviderType = "pool"     // Runners join pool, server assigns
    ProviderTypeExternal ProviderType = "external" // Manual registration
)

type ProviderCapabilities struct {
    Pause    bool // Can pause/resume runners (memory preserved)
    Snapshot bool // Can create/restore snapshots
    Suspend  SuspendCapability // Suspend strategy support
}

// SuspendCapability describes what suspend strategies this provider supports
type SuspendCapability struct {
    Strategies []SuspendStrategy // Supported strategies, in order of preference
    Default    SuspendStrategy   // Default strategy for this provider
}

// SuspendStrategy defines how a session is suspended
type SuspendStrategy string

const (
    // SuspendStrategyPause: Memory preserved, instant resume
    // - Supported by: Docker (pause), Firecracker, E2B
    // - Cost: Still consumes memory while paused
    // - Resume: Instant (<1s)
    SuspendStrategyPause SuspendStrategy = "pause"

    // SuspendStrategySnapshot: Full VM/container snapshot
    // - Supported by: Firecracker, QEMU, Docker (CRIU)
    // - Cost: Storage for snapshot, no compute
    // - Resume: Fast (seconds to restore snapshot)
    SuspendStrategySnapshot SuspendStrategy = "snapshot"

    // SuspendStrategyTerminatePreserveStorage: Terminate but keep storage
    // - Supported by: Kubernetes (PV), Docker (volume)
    // - Cost: Only storage (PV/volume)
    // - Resume: Cold start (need new Pod/container)
    // - State: Workspace preserved, context from DB, memory lost
    SuspendStrategyTerminatePreserveStorage SuspendStrategy = "terminate_preserve_storage"

    // SuspendStrategyReleaseToPool: Release runner back to pool
    // - Supported by: Pool providers (macOS, Windows, bare metal)
    // - Cost: Only object storage (workspace synced)
    // - Resume: Need to acquire new runner from pool
    // - State: Workspace synced to CAS, context from DB, memory lost
    SuspendStrategyReleaseToPool SuspendStrategy = "release_to_pool"

    // SuspendStrategyTerminate: Full termination (no resume possible)
    // - Fallback when no other strategy available
    // - Cost: None
    // - Resume: Not possible, session terminated
    SuspendStrategyTerminate SuspendStrategy = "terminate"
)

// SuspendConfig configures suspend behavior for a provider
type SuspendConfig struct {
    // Strategy to use (must be in provider's supported strategies)
    Strategy SuspendStrategy `json:"strategy"`

    // MinDuration prevents rapid suspend/resume cycles
    // Some providers charge for suspend operations
    MinDuration time.Duration `json:"min_duration,omitempty"` // default: 60s

    // MaxDuration auto-terminates after this time suspended
    // Prevents indefinite resource holding
    MaxDuration time.Duration `json:"max_duration,omitempty"` // default: 24h, 0 = unlimited

    // Fallback strategy if primary fails
    Fallback SuspendStrategy `json:"fallback,omitempty"` // default: terminate

    // SaveSnapshot creates a snapshot before suspend (if provider supports)
    // Useful for terminate_preserve_storage to also save memory state
    SaveSnapshot bool `json:"save_snapshot,omitempty"`

    // SyncWorkspace forces workspace sync before suspend
    // Required for release_to_pool, optional for others
    SyncWorkspace bool `json:"sync_workspace,omitempty"`
}

// SuspendableProvider extends Provider with suspend/resume using configured strategy
type SuspendableProvider interface {
    Provider

    // Suspend suspends the runner using the configured strategy
    // Returns the actual strategy used (may differ from requested if fallback)
    Suspend(ctx context.Context, runnerID string, opts SuspendOptions) (*SuspendResult, error)

    // Resume restores a suspended runner
    // For terminate_preserve_storage: spawns new runner with same storage
    // For release_to_pool: acquires runner from pool and restores workspace
    Resume(ctx context.Context, sessionID string, opts ResumeOptions) (*RunnerInstance, error)
}

type SuspendOptions struct {
    Strategy      SuspendStrategy // Requested strategy (uses provider default if empty)
    SaveSnapshot  bool            // Create snapshot before suspend
    SyncWorkspace bool            // Sync workspace to object storage
    Timeout       time.Duration   // Max time to wait for suspend
}

type SuspendResult struct {
    Strategy       SuspendStrategy // Actual strategy used
    SnapshotID     string          // If snapshot was created
    WorkspaceSynced bool           // If workspace was synced
    SuspendedAt    time.Time
}

type ResumeOptions struct {
    RunnerID   string // Preferred runner ID (for pool, may get different one)
    SnapshotID string // Restore from specific snapshot
    Timeout    time.Duration
}

// PausableProvider extends Provider with pause/resume (memory preserved)
// Deprecated: Use SuspendableProvider with SuspendStrategyPause instead
type PausableProvider interface {
    Provider

    // Pause suspends the runner (preserves memory state)
    Pause(ctx context.Context, runnerID string) error

    // Resume restores a paused runner
    Resume(ctx context.Context, runnerID string) error
}

// SnapshotProvider extends Provider with snapshot/restore
type SnapshotProvider interface {
    Provider
    
    // Snapshot creates a point-in-time snapshot
    Snapshot(ctx context.Context, runnerID string, name string) (*Snapshot, error)
    
    // Restore creates a new runner from a snapshot
    Restore(ctx context.Context, snapshotID string, opts SpawnOptions) (*RunnerInstance, error)
    
    // ListSnapshots lists available snapshots for a runner
    ListSnapshots(ctx context.Context, runnerID string) ([]*Snapshot, error)
    
    // DeleteSnapshot removes a snapshot
    DeleteSnapshot(ctx context.Context, snapshotID string) error
}

// PoolProvider handles runners that connect to a pool
type PoolProvider interface {
    Provider
    
    // ValidateToken checks if a token is valid for this pool
    ValidateToken(ctx context.Context, token string) (bool, error)
    
    // RunInitScript executes init script when runner claims a task
    RunInitScript(ctx context.Context, runnerID string, task *Task) error
    
    // RunCleanupScript executes cleanup after task completes
    RunCleanupScript(ctx context.Context, runnerID string, task *Task) error
    
    // HealthCheck runs periodic health check on pool runner
    HealthCheck(ctx context.Context, runnerID string) (*HealthStatus, error)
}

type SpawnOptions struct {
    Name        string            // Runner name
    ServerURL   string            // Marionette server URL
    RunnerToken string            // Token for runner authentication
    Environment map[string]string // Additional env vars
    Labels      map[string]string // Labels for the instance
    Annotations map[string]string // Annotations for the instance
    
    // Sandbox configuration
    SandboxMode  string   // "runner-is-sandbox" | "runner-creates-sandbox"
    SandboxTypes []string // ["docker", "gvisor", "namespace", "sandbox-exec", "none"]
    
    // Profile (optional, overrides below settings)
    Profile     string
    
    // Isolation settings
    Isolation   IsolationConfig
}

type IsolationConfig struct {
    // Resource limits
    MemoryMB    int           // Memory limit in MB (default: 2048)
    CPUs        float64       // CPU limit (default: 2.0)
    DiskMB      int           // Disk quota in MB (default: 10240)
    Timeout     time.Duration // Max task duration (default: 1h)
    
    // Network policy
    NetworkLevel    string   // "none", "allow_list", "proxy", "air_gapped"
    AllowedHosts    []string // Hosts allowed when level is "allow_list"
    
    // Workspace
    WorkspaceMount  string   // Host path to mount as workspace (optional)
}

type RunnerInstance struct {
    ID          string            // Provider-specific instance ID
    Name        string            // Runner name
    Status      InstanceStatus    // Infrastructure state (see below)
    SandboxMode string            // "runner-is-sandbox" | "runner-creates-sandbox"
    CreatedAt   time.Time
    Labels      map[string]string
    Annotations map[string]string
    Metadata    map[string]string // Provider-specific metadata
}

// InstanceStatus represents infrastructure/provider state (managed by provider)
// Different from ConnectionStatus which tracks agent connection to server
type InstanceStatus string

const (
    // Infrastructure states (provider-managed)
    InstanceStatusPending  InstanceStatus = "pending"  // Being provisioned
    InstanceStatusRunning  InstanceStatus = "running"  // Infrastructure running
    InstanceStatusPaused   InstanceStatus = "paused"   // Suspended (memory preserved)
    InstanceStatusStopped  InstanceStatus = "stopped"  // Intentionally stopped
    InstanceStatusFailed   InstanceStatus = "failed"   // Error state
)

// ConnectionStatus represents agent connection state (tracked in DB)
// See runners.status in schema.sql
type ConnectionStatus string

const (
    // Connection states (server-tracked)
    ConnectionStatusOffline ConnectionStatus = "offline" // Not connected
    ConnectionStatusIdle    ConnectionStatus = "idle"    // Connected, waiting for work
    ConnectionStatusBusy    ConnectionStatus = "busy"    // Executing a task
    ConnectionStatusPaused  ConnectionStatus = "paused"  // Paused by provider
)

type RunnerStatus struct {
    Status      InstanceStatus // Infrastructure state
    TaskID      string         // Current task ID (if busy)
    UpdatedAt   time.Time
}

type Snapshot struct {
    ID          string
    RunnerID    string
    Name        string
    Size        int64
    CreatedAt   time.Time
    Labels      map[string]string
}

type HealthStatus struct {
    Healthy     bool
    Message     string
    CheckedAt   time.Time
}
```

### Provider config examples

```json
// Docker provider config
{
  "host": "unix:///var/run/docker.sock",
  "image": "marionette/agent:latest",
  "network": "marionette-net",
  "resources": {
    "memory": "2g",
    "cpus": "2"
  },
  "suspend": {
    "strategy": "pause",
    "min_duration": "60s",
    "max_duration": "24h",
    "fallback": "terminate_preserve_storage"
  }
}

// Kubernetes provider config
{
  "kubeconfig": "/path/to/kubeconfig",
  "namespace": "marionette",
  "image": "marionette/agent:latest",
  "resources": {
    "requests": {"memory": "1Gi", "cpu": "500m"},
    "limits": {"memory": "4Gi", "cpu": "2"}
  },
  "serviceAccount": "marionette-agent",
  "networkPolicy": {
    "enabled": true,
    "allowEgress": ["api.anthropic.com", "api.openai.com"]
  },
  "suspend": {
    "strategy": "terminate_preserve_storage",
    "pvc_retain": true,
    "min_duration": "60s",
    "max_duration": "24h",
    "sync_workspace": false
  }
}

// E2B provider config
{
  "api_key_encrypted": "...",
  "template": "base",
  "timeout": "1h",
  "suspend": {
    "strategy": "pause",
    "max_duration": "1h"
  }
}

// Pool provider config (macOS)
{
  "auth_mode": "token",
  "required_labels": {"os": "darwin"},
  "suspend": {
    "strategy": "release_to_pool",
    "sync_workspace": true,
    "min_duration": "0s"
  }
}
```

---

## Suspend Strategies

Suspend allows sessions to be paused when waiting for user input (e.g., permission approval) to save compute resources.

### Strategy Comparison

| Strategy | Memory | Workspace | Resume Speed | Cost While Suspended |
|----------|--------|-----------|--------------|---------------------|
| `pause` | Preserved | In-place | Instant | Memory only |
| `snapshot` | Snapshot | Snapshot | Seconds | Storage only |
| `terminate_preserve_storage` | Lost | PV/Volume | Cold start | Storage only |
| `release_to_pool` | Lost | CAS sync | Pool acquire | Object storage |
| `terminate` | Lost | Lost | N/A | None |

### Provider Default Strategies

| Provider | Default | Fallback | Notes |
|----------|---------|----------|-------|
| Docker | `pause` | `terminate_preserve_storage` | Uses `docker pause`, volume preserved |
| Kubernetes | `terminate_preserve_storage` | `terminate` | Delete Pod, keep PVC |
| E2B | `pause` | `terminate` | Native suspend API |
| Firecracker | `snapshot` | `terminate` | microVM snapshot |
| Pool (macOS/Windows) | `release_to_pool` | N/A | Workspace synced to CAS |

### Suspend Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Suspend Flow                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Permission Request                                                 │
│       │                                                             │
│       ▼                                                             │
│  Wait for approval...                                               │
│       │                                                             │
│       │ (suspend_after_seconds elapsed, e.g., 30 min)               │
│       ▼                                                             │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                   Server: Suspend Session                    │   │
│  │                                                              │   │
│  │  1. Get provider's suspend config                            │   │
│  │  2. Save agent context_snapshot to DB                        │   │
│  │  3. Execute strategy:                                        │   │
│  │                                                              │   │
│  │     ┌─────────────────┐  ┌─────────────────┐                 │   │
│  │     │ pause           │  │ snapshot        │                 │   │
│  │     │                 │  │                 │                 │   │
│  │     │ docker pause    │  │ Create VM       │                 │   │
│  │     │ (memory held)   │  │ snapshot        │                 │   │
│  │     └─────────────────┘  └─────────────────┘                 │   │
│  │                                                              │   │
│  │     ┌─────────────────┐  ┌─────────────────┐                 │   │
│  │     │ terminate_      │  │ release_to_pool │                 │   │
│  │     │ preserve_storage│  │                 │                 │   │
│  │     │                 │  │ Sync workspace  │                 │   │
│  │     │ Delete Pod      │  │ to CAS          │                 │   │
│  │     │ Keep PVC        │  │ Release runner  │                 │   │
│  │     └─────────────────┘  └─────────────────┘                 │   │
│  │                                                              │   │
│  │  4. Update session.status = 'suspended'                      │   │
│  │  5. Permission request stays pending in DB                   │   │
│  │                                                              │   │
│  └──────────────────────────────────────────────────────────────┘   │
│       │                                                             │
│       │ (User approves permission via UI/API)                       │
│       ▼                                                             │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                   Server: Resume Session                     │   │
│  │                                                              │   │
│  │  1. Check permission response in DB                          │   │
│  │  2. Execute resume based on strategy:                        │   │
│  │                                                              │   │
│  │     pause:                                                   │   │
│  │       └─ docker unpause (instant)                            │   │
│  │                                                              │   │
│  │     snapshot:                                                │   │
│  │       └─ Restore from snapshot                               │   │
│  │                                                              │   │
│  │     terminate_preserve_storage:                              │   │
│  │       ├─ Spawn new Pod with same PVC                         │   │
│  │       └─ Restore context_snapshot                            │   │
│  │                                                              │   │
│  │     release_to_pool:                                         │   │
│  │       ├─ Acquire runner from pool                            │   │
│  │       ├─ Restore workspace from CAS                          │   │
│  │       └─ Restore context_snapshot                            │   │
│  │                                                              │   │
│  │  3. Send AttachSession with pending_permissions              │   │
│  │  4. Update session.status = 'active'                         │   │
│  │                                                              │   │
│  └──────────────────────────────────────────────────────────────┘   │
│       │                                                             │
│       ▼                                                             │
│  Runner receives ApprovePermission, continues task                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Kubernetes PV Suspend Example

```yaml
# Session suspend with Kubernetes: terminate Pod, preserve PVC
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: session-sess_xxx-workspace
  labels:
    marionette.dev/session: sess_xxx
    marionette.dev/tenant: tenant_yyy
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 50Gi
  storageClassName: fast-ssd
---
# Pod deleted on suspend, PVC retained
# On resume, new Pod created with same PVC
apiVersion: v1
kind: Pod
metadata:
  name: runner-run_xxx
  labels:
    marionette.dev/session: sess_xxx
spec:
  containers:
  - name: agent
    image: marionette/agent:latest
    volumeMounts:
    - name: workspace
      mountPath: /workspace
  volumes:
  - name: workspace
    persistentVolumeClaim:
      claimName: session-sess_xxx-workspace
```

### State Preservation Matrix

| Component | pause | snapshot | terminate_preserve_storage | release_to_pool |
|-----------|-------|----------|---------------------------|-----------------|
| Running processes | ✓ | ✓ | ✗ | ✗ |
| Memory state | ✓ | ✓ | ✗ | ✗ |
| Filesystem | ✓ | ✓ | ✓ (PV) | ✓ (CAS) |
| Agent context | DB | DB | DB | DB |
| Network connections | ✓ | ✗ | ✗ | ✗ |
| Environment vars | ✓ | ✓ | ✓ (from config) | ✓ (from config) |

### Isolation defaults per provider

| Setting | Docker | Kubernetes | E2B |
|---------|--------|------------|-----|
| Memory | 2GB | 2Gi | Preset |
| CPU | 2 cores | 2 cores | Preset |
| Disk | 10GB | 10Gi PVC | 10GB |
| Network | Bridge (isolated) | NetworkPolicy | Isolated VPC |
| Timeout | 1 hour | 1 hour | 1 hour |
