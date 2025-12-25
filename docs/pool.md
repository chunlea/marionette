# Pool Provider & Agent Lifecycle

## Pool provider

Pool providers allow existing machines (macOS, Windows, bare metal) to join as runners.

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Pool Provider Architecture                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  macOS Machine 1              macOS Machine 2                       │
│  ┌────────────────────┐      ┌────────────────────┐                 │
│  │  marionette-agent  │      │  marionette-agent  │                 │
│  │  --pool macos-pool │      │  --pool macos-pool │                 │
│  │  --labels os=darwin│      │  --labels os=darwin│                 │
│  │           xcode=15 │      │           xcode=16 │                 │
│  └─────────┬──────────┘      └─────────┬──────────┘                 │
│            │                           │                            │
│            └───────────┬───────────────┘                            │
│                        │ gRPC Connect                               │
│                        ▼                                            │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                    Marionette Server                        │    │
│  │                                                             │    │
│  │  Pool: macos-pool                                           │    │
│  │  ┌─────────────────────────────────────────────────────┐    │    │
│  │  │  agent-mac-1: idle,   labels={os=darwin, xcode=15}  │    │    │
│  │  │  agent-mac-2: busy,   labels={os=darwin, xcode=16}  │    │    │
│  │  │               task=task-123                         │    │    │
│  │  └─────────────────────────────────────────────────────┘    │    │
│  │                                                             │    │
│  │  Task Request: {profile: ios-dev, need: xcode>=15}          │    │
│  │       │                                                     │    │
│  │       ▼                                                     │    │
│  │  Scheduler: assign to agent-mac-1 (matches labels)          │    │
│  │                                                             │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Pool provider configuration

```yaml
# configs/providers.yaml

providers:
  # macOS pool for iOS development
  - name: macos-pool
    type: pool
    config:
      # Authentication for pool agents
      # Option 1: Pre-shared tokens (generate with: openssl rand -hex 32)
      # Each runner gets a unique token registered in the database
      auth_mode: token  # "token" or "mtls"

      # Option 2: mTLS (recommended for production)
      # mtls:
      #   ca_cert: /etc/marionette/ca.crt
      #   verify_cn: true  # Verify Common Name matches runner name

      # Labels required to join this pool
      required_labels:
        os: darwin

      # Init script run when agent claims a task
      # SECURITY: Scripts run with elevated privileges on the pool machine.
      # NEVER interpolate user-controlled values directly into shell commands.
      # Use environment variables with validation instead.
      init_script: |
        #!/bin/bash
        set -euo pipefail

        # Clean workspace
        rm -rf ~/workspace/*

        # SAFE: Use environment variable with allowlist validation
        # Server validates XCODE_VERSION against allowlist before setting env var
        if [[ -n "${XCODE_VERSION:-}" ]]; then
          case "$XCODE_VERSION" in
            14.3|15.0|15.1|15.2|15.3|15.4|16.0)
              sudo xcode-select -s "/Applications/Xcode_${XCODE_VERSION}.app"
              ;;
            *)
              echo "ERROR: Invalid Xcode version: $XCODE_VERSION" >&2
              exit 1
              ;;
          esac
        fi

        # SAFE: Task ID is server-generated (prefixed ID format)
        if [[ -n "${NETWORK_RULES_FILE:-}" && -f "$NETWORK_RULES_FILE" ]]; then
          sudo pfctl -a marionette-agent -f "$NETWORK_RULES_FILE"
        fi

      # Cleanup script after task completes
      cleanup_script: |
        #!/bin/bash
        set -euo pipefail
        # Remove network sandbox
        sudo pfctl -a marionette-agent -F all 2>/dev/null || true
        # Clean workspace
        rm -rf ~/workspace/*
        # Kill any simulators
        killall Simulator 2>/dev/null || true
        # Clear derived data
        rm -rf ~/Library/Developer/Xcode/DerivedData/*

      # Health check (run periodically)
      health_script: |
        #!/bin/bash
        # Check Xcode is available
        xcode-select -p > /dev/null 2>&1
```

### Running a pool agent

```bash
# On macOS machine, install and run agent
curl -fsSL https://get.marionette.dev/agent | sh

# Generate a secure token for this runner (run once, save securely)
# openssl rand -hex 32 > /etc/marionette/runner-token

# Start agent in pool mode
marionette-agent \
  --server grpcs://marionette.example.com:9090 \
  --pool macos-pool \
  --token-file /etc/marionette/runner-token \
  --labels "os=darwin,arch=arm64,xcode=15.2,memory=32GB" \
  --workspace ~/marionette-workspace \
  --log-level info

# Or via systemd/launchd for auto-start
sudo marionette-agent install \
  --server grpcs://marionette.example.com:9090 \
  --pool macos-pool \
  --token-file /etc/marionette/runner-token \
  --labels "os=darwin,arch=arm64,xcode=15.2"
```

### Script Security

**CRITICAL**: Init/cleanup scripts run with elevated privileges on pool machines.
A compromised script can affect ALL tasks on that machine.

#### Label Security Model

| Label Source | User Writable | Allowed in Scripts |
|--------------|---------------|-------------------|
| Runner labels | No (admin) | Yes (trusted) |
| Profile labels | No (admin) | Yes (trusted) |
| Task labels | Yes (user) | **NO** (untrusted) |
| Session labels | Yes (user) | **NO** (untrusted) |

#### Safe Script Patterns

```go
// pkg/pool/script.go

// ScriptContext contains only SAFE values for script execution
// User-controlled values are NEVER included directly
type ScriptContext struct {
    // Server-generated IDs (safe - validated format)
    TaskID    string // task_xxx format
    SessionID string // sess_xxx format
    RunnerID  string // run_xxx format

    // Admin-defined values (safe - from config)
    RunnerLabels map[string]string
    ProfileName  string

    // Validated user selections (safe - validated against allowlist)
    // Server validates these BEFORE setting
    ValidatedSelections map[string]string
}

// PrepareScriptEnv creates environment variables for script execution
func (p *PoolProvider) PrepareScriptEnv(ctx context.Context, task *Task) (map[string]string, error) {
    env := make(map[string]string)

    // Safe: server-generated IDs
    env["TASK_ID"] = task.ID
    env["SESSION_ID"] = task.SessionID
    env["WORKSPACE"] = task.WorkspacePath

    // Safe: admin-defined runner labels
    for k, v := range task.Runner.Labels {
        env["RUNNER_LABEL_"+sanitizeEnvKey(k)] = v
    }

    // Validate user-requested selections against profile allowlist
    profile, _ := p.store.GetProfile(ctx, task.ProfileID)
    for key, value := range task.Labels {
        if allowlist, ok := profile.SelectableOptions[key]; ok {
            if contains(allowlist, value) {
                env[sanitizeEnvKey(key)] = value
            } else {
                return nil, fmt.Errorf("invalid selection for %s: %s", key, value)
            }
        }
        // Non-allowlisted labels are NOT passed to scripts
    }

    return env, nil
}

// sanitizeEnvKey ensures environment variable key is safe
func sanitizeEnvKey(s string) string {
    // Only allow alphanumeric and underscore
    var result strings.Builder
    for _, r := range strings.ToUpper(s) {
        if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
            result.WriteRune(r)
        }
    }
    return result.String()
}
```

#### Profile Allowlists

Profiles define which user selections are valid:

```yaml
# profiles/ios-dev.yaml
name: ios-dev
selectable_options:
  # Users can select from these values only
  xcode_version:
    - "14.3"
    - "15.0"
    - "15.2"
    - "16.0"
  ios_simulator:
    - "iPhone 15"
    - "iPhone 15 Pro"
    - "iPad Pro"
  # Any other task labels are NOT passed to scripts
```

#### What NOT to Do

```yaml
# DANGEROUS - NEVER DO THIS
init_script: |
  # BAD: Direct template interpolation of user-controlled values
  sudo xcode-select -s /Applications/Xcode_{{.Task.Labels.xcode_version}}.app

  # BAD: User labels in shell commands
  echo "Running for team {{.Task.Labels.team_name}}"

  # BAD: Unsanitized path construction
  cd {{.Task.Labels.project_path}}
```

#### What TO Do

```yaml
# SAFE - Use environment variables with server-side validation
init_script: |
  #!/bin/bash
  set -euo pipefail

  # SAFE: Env var validated against allowlist by server
  if [[ -n "${XCODE_VERSION:-}" ]]; then
    sudo xcode-select -s "/Applications/Xcode_${XCODE_VERSION}.app"
  fi

  # SAFE: Server-generated ID (cannot be user-modified)
  echo "Task: $TASK_ID"

  # SAFE: Admin-defined runner label
  echo "Runner: $RUNNER_LABEL_HOSTNAME"
```

### Pool agent lifecycle

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Pool Agent States                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│              connect                                                │
│    ─────────────────►  ┌──────────┐                                 │
│                        │          │  disconnect                     │
│                        │  idle    │  ◄───────────                   │
│                        │ (in pool)│         │                       │
│                        └────┬─────┘         │                       │
│                             │               │                       │
│               claim task    │               │                       │
│               (run init)    │               │                       │
│                             ▼               │                       │
│                        ┌──────────┐         │                       │
│                        │          │         │                       │
│                        │  busy    │─────────┤                       │
│                        │(executing)         │                       │
│                        └────┬─────┘         │                       │
│                             │               │                       │
│               task done     │               │                       │
│               (run cleanup) │               │                       │
│                             ▼               │                       │
│                        ┌──────────┐         │                       │
│                        │ cleaning │─────────┘                       │
│                        │          │  → back to idle                 │
│                        └──────────┘                                 │
│                                                                     │
│  Note: Pool agents don't support pause/snapshot                     │
│        (use managed providers for that)                             │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Agent lifecycle (Pause/Resume/Snapshot)

Managed providers may support pause, resume, and snapshot operations.

### Provider capabilities

| Provider | Pause | Resume | Snapshot | Notes |
|----------|-------|--------|----------|-------|
| Docker | ✓ | ✓ | ✓ | `docker pause`, `docker commit` |
| Kubernetes | ✗ | ✗ | ✗ | Scale to 0 instead |
| E2B | ✓ | ✓ | ✓ | Native suspend |
| Fly.io | ✓ | ✓ | ✗ | Machine stop/start |
| Modal | ✗ | ✗ | ✗ | Ephemeral only |
| Firecracker | ✓ | ✓ | ✓ | microVM snapshot |
| QEMU | ✓ | ✓ | ✓ | VM snapshot |
| Pool | ✗ | ✗ | ✗ | Release to pool instead |

### Lifecycle states

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Agent Lifecycle States                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│                   ┌──────────┐                                      │
│        spawn      │          │  destroy                             │
│    ───────────►   │  idle    │  ◄───────────                        │
│                   │          │         │                            │
│                   └────┬─────┘         │                            │
│                        │               │                            │
│              assign    │               │                            │
│              task      │               │                            │
│                        ▼               │                            │
│                   ┌──────────┐         │                            │
│                   │          │         │                            │
│                   │  busy    │─────────┤                            │
│                   │          │         │                            │
│                   └────┬─────┘         │                            │
│                        │               │                            │
│              pause     │   resume      │                            │
│         (if supported) │               │                            │
│                        ▼               │                            │
│                   ┌──────────┐         │                            │
│                   │          │         │                            │
│                   │  paused  │─────────┘                            │
│                   │          │                                      │
│                   └──────────┘                                      │
│                                                                     │
│  Use cases for pause:                                               │
│  - Cost saving (stop billing while waiting for review)              │
│  - Long-running tasks with idle periods                             │
│  - Preserve state between task phases                               │
│  - Quick resume without cold start overhead                         │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Pause/Resume/Snapshot CLI

```bash
# Pause a runner (if supported by provider)
mctl admin runners pause runner-1

# Resume a paused runner
mctl admin runners resume runner-1

# Create a snapshot
mctl admin runners snapshot runner-1 --name "before-refactor"

# List snapshots
mctl admin runners snapshots runner-1

# Restore from snapshot (creates new runner)
mctl admin runners restore --snapshot snap-123 --name "runner-restored"

# Delete a snapshot
mctl admin runners snapshot-delete snap-123
```

## Watchdog & Taint Mechanism

Pool runners need careful lifecycle management to prevent state pollution between tasks.

### Taint Status

| Tainted | Taint Reason | Description | Action |
|---------|--------------|-------------|--------|
| `false` | - | Runner is clean | Accept tasks |
| `true` | `crash` | Task crashed | Cleanup or destroy |
| `true` | `timeout` | Task timed out | Cleanup or destroy |
| `true` | `state_pollution` | Workspace not cleaned | Cleanup or destroy |
| `true` | `health_check_failed` | Health check failed | Destroy and replace |

### Watchdog Implementation

```go
// pkg/pool/watchdog.go
package pool

// Watchdog monitors pool health and manages runner lifecycle
type Watchdog struct {
    poolMgr    *Manager
    runnerRepo RunnerRepository
    interval   time.Duration
}

// Run starts the watchdog loop
func (w *Watchdog) Run(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.runChecks(ctx)
        }
    }
}

func (w *Watchdog) runChecks(ctx context.Context) {
    pools := w.poolMgr.ListPools()

    for _, pool := range pools {
        // 1. Health check all runners
        w.checkRunnerHealth(ctx, pool)

        // 2. Clean up tainted runners
        w.cleanupTaintedRunners(ctx, pool)

        // 3. Enforce scaling constraints
        w.enforceScaling(ctx, pool)

        // 4. Recycle aged runners
        w.recycleAgedRunners(ctx, pool)
    }
}

// cleanupTaintedRunners handles tainted runner cleanup
func (w *Watchdog) cleanupTaintedRunners(ctx context.Context, pool *Pool) {
    tainted, _ := w.runnerRepo.ListTainted(ctx, pool.Name)

    for _, runner := range tainted {
        // Try to clean the runner
        if err := w.cleanRunner(ctx, runner); err != nil {
            // Can't clean, need to destroy and replace
            w.destroyRunner(ctx, runner)
            w.provisionReplacement(ctx, pool)
        } else {
            // Successfully cleaned, untaint
            w.untaintRunner(ctx, runner.ID)
        }
    }
}
```

### Taint Detection

The runner agent detects conditions that should taint the runner:

```go
// pkg/runner/taint.go
package runner

// TaintDetector monitors for conditions that taint runners
type TaintDetector struct {
    agent      *Agent
    cleanState *StateSnapshot
}

// DetectAfterTask checks for state pollution after task completion
func (d *TaintDetector) DetectAfterTask(ctx context.Context, result TaskResult) *TaintReason {
    // 1. Check for crash
    if result.Crashed {
        return &TaintReason{Reason: "crash", Details: result.Error}
    }

    // 2. Check for timeout
    if result.TimedOut {
        return &TaintReason{Reason: "timeout", Details: "agent may still be running"}
    }

    // 3. Check for leftover processes
    if procs := d.findLeftoverProcesses(); len(procs) > 0 {
        return &TaintReason{
            Reason:  "state_pollution",
            Details: fmt.Sprintf("leftover processes: %v", procs),
        }
    }

    // 4. Check workspace state
    if !d.isWorkspaceClean() {
        return &TaintReason{
            Reason:  "state_pollution",
            Details: "workspace not properly cleaned",
        }
    }

    return nil
}
```

### Runner Cleanup

```go
// pkg/runner/cleanup.go
package runner

// Clean attempts to restore runner to clean state
func (c *Cleaner) Clean(ctx context.Context) error {
    var errs []error

    // 1. Kill any remaining processes
    if err := c.killAllUserProcesses(ctx); err != nil {
        errs = append(errs, fmt.Errorf("killing processes: %w", err))
    }

    // 2. Clean workspace
    if err := c.cleanWorkspace(ctx); err != nil {
        errs = append(errs, fmt.Errorf("cleaning workspace: %w", err))
    }

    // 3. Reset network state
    if err := c.resetNetwork(ctx); err != nil {
        errs = append(errs, fmt.Errorf("resetting network: %w", err))
    }

    // 4. Clear tmp directories
    if err := c.clearTmp(ctx); err != nil {
        errs = append(errs, fmt.Errorf("clearing tmp: %w", err))
    }

    // 5. Run custom cleanup script (from profile)
    if c.runner.Profile.CleanupScript != "" {
        if err := c.runCleanupScript(ctx); err != nil {
            errs = append(errs, fmt.Errorf("cleanup script: %w", err))
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("cleanup failed: %v", errs)
    }
    return nil
}
```

### Pool Scaling Configuration

```yaml
# config.yaml
pools:
  small:
    profile: small-profile
    min_runners: 2
    max_runners: 10
    idle_runners: 3
    scale_up_threshold: 0.8      # Scale up when 80% busy
    scale_down_delay: 300s       # Wait 5 min before scaling down
    max_tasks_per_runner: 100    # Recycle after 100 tasks
    max_runner_age: 24h          # Recycle after 24 hours
    health_check_interval: 30s
```

### Database Queries

```sql
-- List tainted runners in a pool
SELECT id, name, status, taint_reason, last_seen_at
FROM runners
WHERE pool_name = $1 AND tainted = TRUE
ORDER BY updated_at;

-- Get pool statistics
SELECT
    pool_name,
    COUNT(*) as total_runners,
    COUNT(*) FILTER (WHERE status = 'idle' AND NOT tainted) as idle_runners,
    COUNT(*) FILTER (WHERE status = 'busy') as busy_runners,
    COUNT(*) FILTER (WHERE tainted) as tainted_runners
FROM runners
WHERE pool_name = $1
GROUP BY pool_name;

-- Mark runner as tainted
UPDATE runners
SET tainted = TRUE,
    taint_reason = $2,
    updated_at = NOW()
WHERE id = $1;

-- Clear runner taint
UPDATE runners
SET tainted = FALSE,
    taint_reason = NULL,
    updated_at = NOW()
WHERE id = $1;
```

### Monitoring Metrics

| Metric | Description |
|--------|-------------|
| `pool_runners_total{pool}` | Total runners per pool |
| `pool_runners_idle{pool}` | Idle runners per pool |
| `pool_runners_tainted{pool}` | Tainted runners per pool |
| `pool_taint_events{pool,reason}` | Taint events by reason |
| `pool_cleanup_success{pool}` | Successful cleanups |
| `pool_cleanup_failures{pool}` | Failed cleanups requiring destroy |
| `pool_scale_up_events{pool}` | Scale up events |
| `pool_scale_down_events{pool}` | Scale down events |
