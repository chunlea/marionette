# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

```bash
# Install dependencies and setup
make deps
createdb marionette
make migrate

# Build (always use make, not bare `go build`)
make build

# Run tests
make test

# Run linter
make lint

# Generate protobuf
make proto

# Build Docker images
make docker-build

# Hot reload development
make dev
```

### Running Locally

```bash
# Terminal 1: Start server
./bin/server --config configs/local.yaml

# Terminal 2: Start Docker runner
./bin/agent --server localhost:9090 --token dev-token

# Terminal 3: CLI
./bin/mctl sessions create --agent claude --api-key $ANTHROPIC_API_KEY
./bin/mctl tasks create --session $SESSION_ID --prompt "Build a REST API"
./bin/mctl tasks logs --follow $TASK_ID
```

## Documentation

Core reference (always loaded):
- @docs/schema.sql - Database schema (source of truth for data models)

Read on-demand when working on specific features:
- `docs/id.md` - ID generation (Stripe-style prefixed IDs)
- `docs/auth.md` - Authentication and token design
- `docs/runner.proto` - gRPC protocol definitions
- `docs/providers.md` - Provider interface and suspend strategies
- `docs/storage.md` - CAS storage, encryption, log archiving
- `docs/pool.md` - Pool provider and agent lifecycle
- `docs/network.md` - Network isolation levels
- `docs/profiles.md` - Profile definitions
- `docs/integration.md` - Webhooks, metrics
- `docs/roadmap.md` - Implementation phases

---

# Marionette

A remote agent orchestration and observability platform for controlling coding agents (like Claude Code) running in isolated environments.

## Terminology

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Core Concepts                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  marionette-agent   Our control binary, runs inside Runner          │
│  Runner             Execution environment (VM/Container/Machine)    │
│  Agent              AI coding agent (Claude Code, Codex, etc.)      │
│  AgentConfig        Agent credentials (API key, model, base_url)    │
│  Session            Long-lived work context, binds Runner+Workspace │
│  Task               Unit of work (prompt), belongs to a Session     │
│  Workspace          Persistent working directory (/workspace)       │
│  Sandbox            Task isolation environment within Runner        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Key relationships**:
```
Session
  ├── Workspace (persistent storage, survives runner changes)
  ├── Runner (attached/detached dynamically)
  └── Tasks (sequential execution within session)
        ├── Task 1: "Build API" [completed]
        ├── Task 2: "Add auth" [completed]
        └── Task 3: "Fix bug" [running]
```

**Key distinction**:
- **Session** = persistent work context (outlives individual tasks and runners)
- **Runner** = where things run (infrastructure, can be swapped)
- **Agent** = what does the work (AI, like Claude Code)
- **marionette-agent** = our software that bridges them

## Project Overview

Marionette enables users to:
1. Deploy Runners in controlled environments via pluggable providers (Docker, K8s, E2B, etc.)
2. Execute AI coding agents (Claude Code, Codex, etc.) in isolated sandboxes
3. Stream real-time logs and handle permission requests
4. Forward ports, browser/desktop UIs, and mobile emulator screens

### Two Usage Modes

1. **As a component**: Users integrate Marionette via API into their own systems
2. **Standalone**: Out-of-the-box deployment with built-in Admin WebUI (Basic Auth)

### Provider System

Three provider modes for managing Runner lifecycle:

| Mode | Description | Examples |
|------|-------------|----------|
| **Managed** | Server controls full lifecycle (spawn/destroy) | Docker, Kubernetes, E2B, Fly.io, Firecracker |
| **Pool** | Runners join a pool, server assigns work | macOS Pool, Windows Pool, GPU Pool, Bare Metal |
| **External** | Manual one-off runner registration | Self-hosted runners |

See `docs/providers.md` for provider interface and configuration.

### Sandbox Modes

Two independent configuration axes:

1. **Runner Lifecycle**: `per-session` (default, fast) vs `per-task` (high isolation)
2. **Sandbox Mode**: `runner-is-sandbox` vs `runner-creates-sandbox`

| Configuration | Use Case |
|---------------|----------|
| per-session + runner-is-sandbox | Docker/E2B (default for cloud) |
| per-session + runner-creates-sandbox | macOS/GPU pools |
| per-task + runner-is-sandbox | Maximum isolation |

See `docs/pool.md` for agent lifecycle and sandbox types.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Server (Go)                                    │
│  ┌────────────────────────────────────────────────────────────────────┐     │
│  │                            Core                                    │     │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐       │     │
│  │  │ SessionMgr │ │  TaskMgr   │ │ RunnerMgr  │ │ TunnelMgr  │       │     │
│  │  └────────────┘ └────────────┘ └────────────┘ └────────────┘       │     │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐                      │     │
│  │  │ ProviderMgr│ │WorkspaceMgr│ │ SandboxMgr │                      │     │
│  │  └─────┬──────┘ └─────┬──────┘ └────────────┘                      │     │
│  └────────┼──────────────┼────────────────────────────────────────────┘     │
│           │              │                                                  │
│  ┌────────▼──────────────▼────────────────────────────────────────────┐     │
│  │                    Provider Registry                               │     │
│  │  ┌────────┐ ┌────────────┐ ┌─────┐ ┌───────────┐ ┌──────┐          │     │
│  │  │ Docker │ │ Kubernetes │ │ E2B │ │Firecracker│ │ Pool │          │     │
│  │  └────────┘ └────────────┘ └─────┘ └───────────┘ └──────┘          │     │
│  └────────────────────────────────────────────────────────────────────┘     │
│           │                                                                 │
│  ┌────────▼───────────────────────────────────────────────────────────┐     │
│  │  :9090 gRPC   |<---- marionette-agent (mTLS)                       │     │
│  │  :8080 Public |<---- CLI / External Apps (API Key)                 │     │
│  │  :8081 Admin  |<---- Admin WebUI (Basic Auth)                      │     │
│  └────────────────────────────────────────────────────────────────────┘     │
│           │                                                                 │
│  ┌────────▼──────┐                                                          │
│  │    Store      │                                                          │
│  │  PostgreSQL   │                                                          │
│  └───────────────┘                                                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Security Model

### Credential Handling (two modes)

| Mode | Description | Use Case |
|------|-------------|----------|
| **Managed** | API keys stored encrypted in DB | Operator provides keys |
| **BYOK** | Keys in memory only, never stored | Users bring own keys |

### Transport Security

| Connection | Security |
|------------|----------|
| marionette-agent ↔ Server | mTLS (required in production) |
| CLI/API ↔ Server | TLS + API Key |
| Admin WebUI ↔ Server | TLS + Basic Auth |

### Tenant Isolation

- Labels are NOT a security boundary
- All core tables have `tenant_id` column (nullable for single-tenant deployments)
- Auth middleware injects `tenant_id` from auth context into all queries
- Users cannot set/modify `tenant_id` directly
- For multi-tenant SaaS: enforce non-null `tenant_id` at application layer

## Session Lifecycle

Sessions are long-lived work contexts that outlive individual tasks and runners.

**States**: `pending` → `active` ↔ `suspended` ↔ `resuming` → `terminated`

- **pending**: Waiting for runner assignment
- **active**: Runner attached, can execute tasks
- **suspended**: Runner released, state preserved (workspace + context)
- **resuming**: Acquiring new runner and restoring state
- **terminated**: Ended, resources cleaned up

### Session-Runner Relationship

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Session-Runner Lifecycle                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Key Principle: Sessions OUTLIVE Runners                            │
│  - A session can have 0 or 1 runner at any time                     │
│  - Runners can be attached/detached without losing session state    │
│  - Workspace persists independently of runner                       │
│                                                                     │
│  Session States          Runner States                              │
│  ──────────────          ─────────────                              │
│                                                                     │
│  ┌─────────┐             ┌─────────┐                                │
│  │ pending │────────────►│ offline │ (no runner yet)                │
│  └────┬────┘             └────┬────┘                                │
│       │ assign runner         │ connect                             │
│       ▼                       ▼                                     │
│  ┌─────────┐             ┌─────────┐                                │
│  │ active  │◄───────────►│  idle   │ (runner attached)              │
│  └────┬────┘             └────┬────┘                                │
│       │                       │ execute task                        │
│       │                       ▼                                     │
│       │                  ┌─────────┐                                │
│       │                  │  busy   │ (running task)                 │
│       │                  └────┬────┘                                │
│       │                       │                                     │
│       │ suspend               │ task done                           │
│       ▼                       ▼                                     │
│  ┌───────────┐           ┌─────────┐                                │
│  │ suspended │           │  idle   │                                │
│  └─────┬─────┘           └─────────┘                                │
│        │                      │                                     │
│        │ (runner released)    │ detach (suspend strategy)           │
│        │                      ▼                                     │
│        │                 Runner may be:                             │
│        │                 - paused (memory preserved)                │
│        │                 - terminated (PV preserved)                │
│        │                 - released to pool                         │
│        │                                                            │
│        │ resume                                                     │
│        ▼                                                            │
│  ┌───────────┐                                                      │
│  │ resuming  │──────────► Acquire new/same runner                   │
│  └─────┬─────┘            Restore workspace (if needed)             │
│        │                  Restore context_snapshot                  │
│        │                  Deliver pending permissions               │
│        ▼                                                            │
│  ┌─────────┐                                                        │
│  │ active  │ (ready for next task)                                  │
│  └─────────┘                                                        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### State Transitions

| From | To | Trigger | Actions |
|------|------|---------|---------|
| pending | active | Runner assigned | AttachSession to runner |
| active | suspended | Suspend (idle/permission timeout) | Save context, execute suspend strategy |
| active | terminated | User terminates | Cleanup resources |
| suspended | resuming | User resumes / Permission responded | Request runner |
| resuming | active | Runner attached | Restore context, deliver pending permissions |
| resuming | suspended | Runner unavailable | Back to suspended, retry later |

### What Persists Across Runner Changes

| Component | Storage | Survives Runner Change |
|-----------|---------|----------------------|
| Workspace files | CAS / PV / Volume | ✓ Yes |
| Agent context | DB (context_snapshot) | ✓ Yes |
| Session metadata | DB | ✓ Yes |
| Pending permissions | DB | ✓ Yes |
| Running processes | Memory | ✗ No (except pause strategy) |
| Network connections | Memory | ✗ No |
| Environment vars | Config | ✓ Yes (restored from config) |

### Runner Swapping Scenarios

1. **Permission Timeout Suspend**
   ```
   Session active → Permission requested → 30 min timeout →
   Session suspended (runner released) → User approves →
   Session resuming → New runner attached → Continue task
   ```

2. **Idle Timeout Suspend**
   ```
   Session active → No tasks for idle_timeout →
   Session suspended → User creates new task →
   Session resuming → Runner attached → Execute task
   ```

3. **Runner Failure**
   ```
   Session active → Runner crashes/disconnects →
   Session suspended (workspace synced if possible) →
   Auto-resume with new runner
   ```

4. **Pool Runner Rotation**
   ```
   Session active on Runner-A → Task completes →
   Session suspended (release to pool) → Runner-A returns to pool →
   New task → Session resuming → Runner-B assigned → Continue
   ```

### Session Lifecycle Modes

Sessions can operate in different lifecycle modes depending on use case:

| Mode | Description | Suspend Behavior | Use Case |
|------|-------------|------------------|----------|
| `on_demand` | Default, cost-efficient | Auto-suspend after idle_timeout | Most development tasks |
| `always_on` | 7x24 persistent session | Never auto-suspend | Cloud assistant, monitoring |
| `scheduled` | Activated by cron schedule | Suspend between scheduled runs | Daily reports, periodic tasks |

**on_demand** (default):
```
Session created → Runner attached → Task executed →
Idle for idle_timeout → Auto-suspend (save cost) →
User sends new task → Resume → Execute
```

**always_on**:
```
Session created → Runner attached → Ready 24/7 →
Tasks can arrive anytime → Execute immediately →
Never auto-suspends (unless user explicitly suspends)
```

**scheduled**:
```
Session created (schedule: "0 9 * * 1-5") →
Session suspended (waiting for schedule) →
Mon-Fri 9am: Auto-resume → Execute scheduled tasks →
Tasks complete → Auto-suspend until next schedule
```

### Scheduled Tasks

For recurring work, sessions support scheduled tasks with cron expressions:

```yaml
# Example: Daily standup summary
scheduled_tasks:
  - name: daily-standup
    cron: "0 9 * * 1-5"           # Weekdays at 9am
    timezone: "America/Los_Angeles"
    prompt_template: |
      Generate a standup summary for {{.Date}}.
      Check git logs and summarize yesterday's progress.
    on_failure: pause_on_failure  # Stop if it fails
```

**Scheduled task states**:
- `active`: Will run at next scheduled time
- `paused`: Temporarily disabled (can be resumed)
- `disabled`: Permanently disabled (after max failures)

**Error handling options**:
- `continue`: Keep running even if last run failed
- `pause_on_failure`: Pause until manually resumed
- `disable_on_failure`: Disable after N consecutive failures

### CLI Examples

```bash
# Create always-on cloud assistant session
mctl sessions create \
  --agent claude \
  --lifecycle always_on \
  --name "my-assistant"

# Create scheduled session for daily reports
mctl sessions create \
  --agent claude \
  --lifecycle scheduled \
  --schedule "0 9 * * 1-5" \
  --schedule-tz "America/Los_Angeles" \
  --name "daily-reporter"

# Add scheduled task to a session
mctl scheduled-tasks create \
  --session $SESSION_ID \
  --name "daily-summary" \
  --cron "0 9 * * *" \
  --prompt "Summarize yesterday's git commits"

# List scheduled tasks
mctl scheduled-tasks list --session $SESSION_ID

# Pause a scheduled task
mctl scheduled-tasks pause $SCHEDULED_TASK_ID

# Manually trigger a scheduled task now
mctl scheduled-tasks trigger $SCHEDULED_TASK_ID
```

## Task Lifecycle

Tasks are logical units of work. Each task can have multiple runs (execution attempts).

**Task states**: `pending` | `running` | `completed` | `failed` | `canceled`

**Run states**: `pending` → `assigned` → `running` → `completed`/`failed`/`timeout`/`canceled`

## Permission Request Lifecycle

When an agent needs approval for a potentially dangerous action (e.g., executing a shell command), it sends a `PermissionRequest`. The system supports async approval with session suspend/resume:

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Permission Request Flow                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Agent requests permission                                          │
│       │                                                             │
│       ▼                                                             │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  PermissionRequest created (status: pending)                │    │
│  │  Runner blocks, waiting for approval                        │    │
│  └──────────────────────────┬──────────────────────────────────┘    │
│                             │                                       │
│         ┌───────────────────┼───────────────────┐                   │
│         ▼                   ▼                   ▼                   │
│   User approves       User denies        No response                │
│   within timeout      within timeout     (suspend_after)            │
│         │                   │                   │                   │
│         ▼                   ▼                   ▼                   │
│   Agent continues     Agent receives     Session suspended          │
│   with action         denial, handles    Permission stays pending   │
│                       gracefully         (no timeout, waits forever)│
│                                                │                    │
│                                                ▼                    │
│                                          User resumes session       │
│                                                │                    │
│                                    ┌───────────┴───────────┐        │
│                                    ▼                       ▼        │
│                              Already responded?      Still pending? │
│                              Deliver cached          Wait for       │
│                              response                response       │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**States**: `pending` → `approved` | `denied` | `expired` | `canceled`

**Timing defaults**:
- `suspend_after_seconds`: 1800 (30 min) - auto-suspend session if no response

**Key behaviors**:
1. No auto-deny: permission requests stay pending until explicit user response
2. User can respond to pending permission requests even while session is suspended
3. When session resumes, any pending responses are delivered to runner
4. Session suspend does NOT fail the task - it pauses execution
5. Permission requests are persisted to database for durability
6. Only explicit actions end a pending request: approve, deny, cancel task, or terminate session

## Tech Stack

| Component | Technology |
|-----------|------------|
| Server | Go |
| Agent | Go |
| CLI (mctl) | Go |
| WebUI | React + TypeScript + Vite + TanStack Router + TanStack Query |
| Communication | gRPC (Agent↔Server), WebSocket (Dashboard↔Server) |
| Database | PostgreSQL |
| Desktop streaming | Selkies GStreamer (WebRTC) |
| Android streaming | scrcpy |

## Project Structure

```
marionette/
├── cmd/
│   ├── server/main.go          # Server binary
│   ├── agent/main.go           # Agent binary
│   └── mctl/                   # CLI tool
├── api/proto/                  # gRPC proto definitions
├── gen/proto/                  # Generated protobuf code
├── pkg/                        # Public packages
│   ├── server/                 # Server implementation
│   │   ├── grpc/               # gRPC handlers (:9090)
│   │   ├── public/             # Public HTTP API (:8080)
│   │   ├── admin/              # Admin HTTP API (:8081)
│   │   └── core/               # Business logic
│   ├── agent/                  # Agent implementation
│   ├── provider/               # Provider implementations (Docker, K8s, E2B)
│   ├── store/                  # Database layer (PostgreSQL)
│   ├── client/                 # Go SDK
│   └── crypto/                 # Encryption utilities
├── internal/                   # Private packages
├── web/                        # React WebUI
├── configs/                    # Example config files
└── deploy/docker/              # Dockerfiles + compose
```

## Configuration

### Port Summary

| Port | Purpose | Authentication |
|------|---------|----------------|
| 8080 | Public API | API Key |
| 8081 | Admin API + WebUI | Master Key / Basic Auth |
| 9090 | gRPC (agents) | Agent Token |

### Config Files

Config file contains non-sensitive settings only. Sensitive values loaded from environment.

```yaml
# config.yaml - non-sensitive only
server:
  api:
    port: 8080
  admin:
    port: 8081
  grpc:
    port: 9090

providers:
  default: docker
  docker:
    image: "ghcr.io/chunlea/marionette-runner:latest"

storage:
  provider: local
  local:
    path: /var/marionette/storage
```

```bash
# .env - sensitive values (all prefixed with MARIONETTE_)
MARIONETTE_DATABASE_URL=postgres://localhost/marionette?sslmode=disable
MARIONETTE_MASTER_KEY=your-master-key
MARIONETTE_ENCRYPTION_KEY=your-encryption-key
```

## CLI Examples

```bash
# Session management
mctl sessions create --agent claude --api-key $KEY --name "my-project"
mctl sessions list
mctl sessions suspend $SESSION_ID
mctl sessions attach $SESSION_ID

# Task execution
mctl tasks create --session $SID --prompt "Build a REST API"
mctl tasks logs --follow $TASK_ID
mctl tasks cancel $TASK_ID

# Continue from previous task
mctl tasks create --continue $TASK_ID --prompt "Add authentication"

# Admin operations
mctl admin keys create --name "ci-key" --scopes "tasks:*"
mctl admin agent-configs create --name "claude-prod" --agent claude --api-key $KEY
mctl admin providers create docker --name "docker-local" --image "marionette/runner:latest"
mctl admin runners spawn --provider docker-local --name "runner-1"
```

## Environment Variables

All environment variables are prefixed with `MARIONETTE_` for consistency.

### Server

| Variable | Description |
|----------|-------------|
| `MARIONETTE_DATABASE_URL` | PostgreSQL connection string |
| `MARIONETTE_MASTER_KEY` | Master key for admin operations |
| `MARIONETTE_ENCRYPTION_KEY` | Key for encrypting agent credentials |
| `MARIONETTE_UI_USERNAME` / `MARIONETTE_UI_PASSWORD` | WebUI Basic Auth credentials |

### marionette-agent

| Variable | Description |
|----------|-------------|
| `MARIONETTE_SERVER` | Server gRPC URL |
| `MARIONETTE_RUNNER_TOKEN` | Token for server authentication |
| `MARIONETTE_SANDBOX_MODE` | "runner-is-sandbox" or "runner-creates-sandbox" |
| `MARIONETTE_POOL_NAME` | Pool name for pool runners |

### CLI (mctl)

| Variable | Description |
|----------|-------------|
| `MARIONETTE_API_URL` | Public API URL |
| `MARIONETTE_API_KEY` | API key for operations |

## Key Design Decisions

1. **Runner initiates connection** - Avoids firewall issues (like GitHub Actions)
2. **Separate control and log streams** - Control: low-latency; Logs: high-throughput
3. **No Redis required (MVP)** - In-memory for real-time, DB for persistence
4. **LiteLLM-inspired auth** - Master key + API keys with labels, no users table
5. **Interface-based store** - PostgreSQL default, extensible
6. **Managed + BYOK credentials** - Flexibility for different deployment models
7. **Pluggable providers** - Docker, K8s, E2B, Pool, custom
8. **Session-based execution** - Long-lived contexts, runner can be swapped
9. **Task isolation** - Each task runs in sandbox within session
10. **Labels for organization** - Kubernetes-style selectors, not security boundary

## Implementation Notes

1. Use structured logging (zap) from the start
2. Use prefixed IDs via `pkg/id` (see `docs/id.md` for format)
3. Pass `context.Context` through all layers
4. Define clear error types in `store/errors.go`
5. Handle SIGTERM properly for graceful shutdown
6. Use AES-GCM for credential encryption at rest

## Testing

### Coverage Requirements

- Minimum test coverage: **90%** for new code
- Run coverage report: `make test-coverage` (runs in Docker)
- Coverage report output: `coverage.html`

### Test Environments

| Environment | Use Case | Command |
|-------------|----------|---------|
| Docker (Linux) | Server, Linux agent, integration tests, coverage | `make test-linux` |
| Docker (root) | Tests requiring root privileges | `make test-linux-root` |
| Docker (coverage) | Coverage report generation | `make test-coverage` |
| Local (macOS) | macOS agent testing only | `make test` |

**Why Docker for most tests:**
- Consistent Linux environment matching production
- Test Linux-specific features (sandbox, cgroups, etc.)
- Avoid macOS/Linux behavioral differences
- Coverage reports reflect production environment

**macOS native tests:**
- macOS agent-specific functionality only

### Temporary Files Directory

Use `.claude/tmp/` for temporary test files and scripts:

```
.claude/
└── tmp/           # Temporary files (gitignored)
```

**Why `.claude/tmp/`:**
- Writable under Claude Code's sandbox mode (within project root)
- Automatically gitignored (won't be committed)
- Easy to clean up (just delete the directory)
- Avoids cluttering system `/tmp`

### Running Tests

```bash
# Full test suite in Docker (recommended)
make test-linux

# Coverage report in Docker
make test-coverage

# macOS agent tests only
make test

# Specific package (local, for quick iteration)
go test -v ./pkg/store/...

# Specific test
go test -v ./pkg/store -run TestSessionCreate
```

## Git Workflow

### Branch Naming Convention

Format: `{developer}/{phase}-{feature}`

Examples:
- `alice/g1-grpc-server`
- `bob/g2-control-runner`
- `carol/g3-http-public`

### Creating Pull Requests

Use `gh` CLI to create pull requests with proper formatting:

```bash
# Create PR with multiline body using HEREDOC
# Use single-quoted 'EOF' to prevent variable expansion issues
gh pr create --title "feat: add new feature" --body "$(cat <<'EOF'
## Summary
- Brief description of changes

## Test Plan
- [ ] Unit tests added
- [ ] Manual testing done

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"

# For complex descriptions, use --body-file
echo "## Summary
..." > /tmp/pr-body.md
gh pr create --title "feat: add new feature" --body-file /tmp/pr-body.md
```

**Important:** Always use single-quoted `'EOF'` (not `EOF`) to prevent shell variable expansion issues in the PR body.

### Merge Workflow (Stacked PRs)

We use **squash merge** via `gh` CLI. When working with stacked PRs, follow this sequence to prevent dependent PRs from being auto-closed:

```bash
# 1. Squash merge the PR (do NOT delete remote branch yet)
gh pr merge <PR_NUMBER> --squash

# 2. Update dependent PRs to point to main
gh pr edit <DEPENDENT_PR_NUMBER> --base main

# 3. Delete the remote branch (after dependent PRs are rebased)
git push origin --delete <branch-name>

# 4. Delete local branch (use -D since squash merge is not detected)
git branch -D <branch-name>

# 5. Rebase dependent branches onto main
git checkout <dependent-branch>
git fetch origin main
git rebase origin/main
git push --force-with-lease
```

### Cleaning Up Merged Branches

After PRs are merged, remote branches may still exist. To clean up:

```bash
# Prune remote tracking branches
git fetch --prune

# Find local branches with deleted remotes (shows "[gone]")
git branch -vv | grep ': gone]'

# Delete local branches (use -D for squash-merged branches)
git branch -D <branch-name>
```
