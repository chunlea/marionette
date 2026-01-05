# Implementation Roadmap

## Phase Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Implementation Phases                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Phase 1: Foundation                                                │
│  └── Project setup, Store, Auth, Crypto, Docker provider            │
│                                                                     │
│  Phase 2: Core Runtime                                              │
│  └── gRPC, Control channel, Runner lifecycle, Workspace             │
│                                                                     │
│  Phase 3: Session & Task                                            │
│  └── Session state machine, Task execution, Logs, Permissions       │
│                                                                     │
│  Phase 4: HTTP API & CLI                                            │
│  └── REST API, WebSocket, mctl CLI                                  │
│                                                                     │
│  Phase 5: Suspend/Resume                                            │
│  └── Suspend strategies, CAS storage, Context restore               │
│                                                                     │
│  Phase 6: Security Hardening                                        │
│  └── mTLS, Sandbox verification, Audit logs, Network isolation      │
│                                                                     │
│  Phase 7: WebUI                                                     │
│  └── Admin UI, Dashboard, React components                          │
│                                                                     │
│  Phase 8: Production Ready                                          │
│  └── Docker/K8s deployment, Docs, Observability, Performance        │
│                                                                     │
│  Phase 9: Advanced Providers                                        │
│  └── Kubernetes, E2B, Pool provider, Profiles                       │
│                                                                     │
│  Phase 10: Advanced Features                                        │
│  └── Tunneling, Scheduled tasks, Webhooks, Metrics                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Foundation

### 1.1 Project Setup ✓

- [x] Initialize Go module (`go mod init github.com/chunlea/marionette`)
- [x] Create directory structure:
  ```
  marionette/
  ├── cmd/
  │   ├── server/main.go
  │   ├── agent/main.go
  │   └── mctl/main.go
  ├── api/proto/
  ├── gen/proto/
  ├── pkg/
  │   ├── server/
  │   ├── agent/
  │   ├── store/
  │   ├── crypto/
  │   ├── id/
  │   └── client/
  ├── internal/
  ├── configs/
  └── deploy/
  ```
- [x] Create Makefile with targets:
  - [x] `make deps` - install dependencies
  - [x] `make build` - build all binaries
  - [x] `make test` - run tests
  - [x] `make lint` - run linter
  - [x] `make proto` - generate protobuf
  - [x] `make migrate` - run database migrations
  - [x] `make dev` - hot reload development
- [x] Set up protobuf generation:
  - [x] Install buf or protoc
  - [x] Create buf.yaml / buf.gen.yaml
  - [x] Generate Go code from runner.proto
- [x] Set up linting (golangci-lint v2)
- [x] Set up pre-commit hooks
- [x] Set up LSP/editor settings (VS Code, gopls, .editorconfig)

### 1.2 Server API Foundation ✓

- [x] Set up Public API server (`pkg/server/api/`):
  - [x] Chi router with CORS middleware
  - [x] `GET /health` - Health check endpoint
  - [x] `GET /healthz` - Kubernetes probe endpoint
- [x] Set up Admin API server (`pkg/server/admin/`):
  - [x] `GET /health` - Admin health check
  - [x] `GET /healthz` - Kubernetes probe
  - [x] `GET /api/status` - Service registry status
  - [x] Embedded static file serving (for future WebUI)
- [x] Set up gRPC server (`pkg/server/grpc/`):
  - [x] RunnerService stub implementation
  - [x] gRPC reflection enabled
- [x] Server binary wiring (`cmd/server/main.go`):
  - [x] Start all 3 servers (public :8080, admin :8081, gRPC :9090)
  - [x] Graceful shutdown handling
  - [x] Service registry for status reporting
- [x] Unit tests for all handlers and services

### 1.3 ID Generation ✓

- [x] Implement `pkg/id/id.go`:
  - [x] `New(prefix string) string` - generate prefixed ID
  - [x] `encodeTimestamp(n int64) string` - fixed-width base52 (letters only)
  - [x] Convenience functions: `Session()`, `Task()`, `Runner()`, etc. (20 total)
- [x] Implement `pkg/id/validate.go`:
  - [x] `Parse(id string) (prefix, value string, err error)`
  - [x] `ExtractTime(id string) time.Time`
  - [x] Type checking: `IsSession()`, `IsTask()`, `IsRunner()`, etc. (20 total)
- [x] Write unit tests:
  - [x] Test time ordering (lexicographic = chronological)
  - [x] Test prefix extraction
  - [x] Test time extraction accuracy

### 1.4 Configuration ✓

- [x] Create `pkg/config/config.go`:
  - [x] Define config structs (Server, Agent, Database, etc.)
  - [x] Viper integration for YAML + env vars
  - [x] Environment variable prefix: `MARIONETTE_`
- [x] Create example config files:
  - [x] `configs/local.yaml` - local development
  - [x] `configs/production.yaml.example` - production template
- [x] Sensitive values from environment only:
  - [x] `MARIONETTE_DATABASE_URL`
  - [x] `MARIONETTE_MASTER_KEY`
  - [x] `MARIONETTE_ENCRYPTION_KEY`

### 1.5 Store Layer ✓

- [x] Define store interfaces (`pkg/store/store.go`):
  ```go
  type Store interface {
      // Runners
      CreateRunner(ctx, *Runner) error
      GetRunner(ctx, id string) (*Runner, error)
      ListRunners(ctx, opts ListOptions) ([]*Runner, error)
      UpdateRunner(ctx, id string, updates map[string]any) error
      DeleteRunner(ctx, id string) error

      // Sessions
      CreateSession(ctx, *Session) error
      GetSession(ctx, id string) (*Session, error)
      // ... etc

      // Transactions
      BeginTx(ctx) (Tx, error)
  }
  ```
- [x] Define data models (`pkg/store/models.go`):
  - [x] Runner, Session, Task, TaskRun
  - [x] Workspace, PermissionRequest
  - [x] APIKey, RunnerToken, AgentConfig
  - [x] ProviderConfig, Profile
  - [x] Snapshot, Tunnel, DataKey
  - [x] Log, LogArchive, ActionLog
  - [x] Chunk, Manifest
- [x] Implement PostgreSQL store (`pkg/store/postgres/`):
  - [x] `store.go` - connection pool setup
  - [x] `tx.go` - transaction wrapper
  - [x] `helpers.go` - SQL helpers
  - [x] `runners.go` - runner CRUD
  - [x] `sessions.go` - session CRUD
  - [x] `tasks.go` - task/task_run/scheduled_task CRUD
  - [x] `workspaces.go` - workspace CRUD
  - [x] `permissions.go` - permission_request CRUD
  - [x] `auth.go` - api_keys, runner_tokens
  - [x] `configs.go` - agent_configs, provider_configs, profiles
  - [x] `storage.go` - snapshots, tunnels, data_keys
  - [x] `logs.go` - logs, log_archives, action_logs
  - [x] `cas.go` - chunks, manifests
- [x] Create migration system (golang-migrate):
  - [x] `migrations/001_initial.up.sql` - from schema.sql
  - [x] `migrations/001_initial.down.sql`
  - [x] Migration runner in Makefile
- [x] Wire store into server:
  - [x] Initialize store when DATABASE_URL is set
  - [x] Register database health status
  - [x] Graceful shutdown
- [ ] Implement tenant isolation (deferred to later phase):
  - [ ] `TenantContext` wrapper for queries
  - [ ] Automatic `tenant_id` injection
  - [ ] Cross-tenant validation in store methods
- [x] Write store tests:
  - [x] Use testcontainers for PostgreSQL
  - [x] Test CRUD operations (runners, workspaces, sessions, tasks, API keys, configs)
  - [x] Test transactions (commit/rollback)
  - [x] Test unique constraints and error handling

### 1.6 Token & Authentication ✓

- [x] Implement token generation (`pkg/crypto/token.go`):
  ```go
  func GenerateToken(prefix string) (token, displayPrefix, hash string, version int, err error)
  func VerifyToken(token, storedHash string, version int, hmacKey []byte) bool
  ```
- [x] Token prefixes:
  - [x] `mk_` - API keys
  - [x] `rtok_` - Runner tokens
  - [x] `ttok_` - Tunnel tokens
- [x] SHA-256 hashing with hash_version support:
  - [x] Version 1: SHA-256 (current)
  - [x] Version 2: HMAC-SHA256 (reserved)
- [x] Implement API key service (`pkg/auth/apikey.go`):
  - [x] `Create(name string, scopes []string) (*APIKey, plainToken string, error)`
  - [x] `Validate(token string) (*APIKeyInfo, error)`
  - [x] `Revoke(id string, reason string) error`
  - [x] `List(opts ListOptions) ([]*APIKey, error)`
- [x] Implement runner token service (`pkg/auth/runnertoken.go`):
  - [x] `Create(poolName string) (*RunnerToken, plainToken string, error)`
  - [x] `Validate(token string) (*RunnerTokenInfo, error)`
  - [x] `Rotate(id string) (newToken string, error)` - with grace period
  - [x] `Revoke(id string, reason string) error`
- [x] Write auth tests:
  - [x] Token generation uniqueness
  - [x] Hash verification
  - [x] Token rotation grace period

### 1.7 Encryption ✓

- [x] Implement envelope encryption (`pkg/crypto/envelope.go`):
  - [x] KEK loading from environment (`MARIONETTE_ENCRYPTION_KEY`)
  - [x] DEK generation per resource
  - [x] `Encrypt(tenantID string, plaintext []byte) (ciphertext []byte, error)`
  - [x] `Decrypt(tenantID string, ciphertext []byte) (plaintext []byte, error)`
- [x] AES-256-GCM implementation:
  - [x] Nonce generation (12 bytes random)
  - [x] Authenticated encryption
  - [x] Ciphertext format: `nonce || ciphertext || tag`
- [x] DEK management:
  - [x] Store encrypted DEKs in `data_keys` table
  - [x] DEK rotation support
  - [x] Per-tenant DEK isolation
- [x] Write crypto tests:
  - [x] Encryption roundtrip
  - [x] Different tenants have different DEKs
  - [x] Tampering detection

### 1.8 Docker Provider (Basic) ✓

- [x] Define provider interface (`pkg/provider/provider.go`):
  ```go
  type Provider interface {
      Name() string
      Type() ProviderType  // managed, pool, external
      Spawn(ctx, SpawnOptions) (*RunnerInstance, error)
      Destroy(ctx, runnerID string) error
      Status(ctx, runnerID string) (*RunnerStatus, error)
      List(ctx) ([]*RunnerInstance, error)
      Capabilities() ProviderCapabilities
  }
  ```
- [x] Implement Docker provider (`pkg/provider/docker/`):
  - [x] `docker.go` - provider implementation
  - [x] Docker client setup (docker.sock or TCP)
  - [x] Container creation with labels
  - [x] Volume mounts for workspace
  - [x] Network configuration (bridge)
- [x] Spawn options:
  - [x] Image name (default: `marionette/agent:latest`)
  - [x] Environment variables
  - [x] Resource limits (memory, CPU)
  - [x] Labels and annotations
- [x] Container lifecycle:
  - [x] `Spawn()` - create and start container
  - [x] `Destroy()` - stop and remove container
  - [x] `Status()` - inspect container state
- [x] Provider registry:
  - [x] Register providers by name
  - [x] Default provider configuration
  - [x] Provider config from database
- [x] Write provider tests:
  - [x] Use testcontainers or mock Docker client
  - [x] Test spawn/destroy lifecycle
  - [x] Test status reporting

---

## Phase 2: Core Runtime

### 2.1 gRPC Server ✓

- [x] Set up gRPC server (`pkg/server/grpc/server.go`):
  - [x] TLS configuration (required)
  - [x] Port 9090 for agent connections
  - [x] Interceptors for logging, recovery
- [x] Implement RunnerService (stubs):
  - [x] `RegisterRunner(RegisterRunnerRequest) returns (RegisterRunnerResponse)`
  - [x] `Connect(stream RunnerMessage) returns (stream ServerCommand)`
  - [x] `StreamLogs(stream StreamLogsMessage) returns (StreamLogsResponse)`
  - [x] `GetRunnerStatus(GetRunnerStatusRequest) returns (RunnerStatus)`
- [x] TLS setup:
  - [x] Load server certificate and key
  - [ ] CA certificate for client verification (optional in dev)
  - [ ] Certificate generation script for development
- [x] Connection management:
  - [x] Track connected runners in memory
  - [x] Handle connection/disconnection events
  - [x] Update runner status in database

### 2.2 Marionette Agent (Basic) ✓

- [x] Create agent binary (`cmd/agent/main.go`):
  - [x] Config loading (server URL, token, labels)
  - [x] Graceful shutdown handling
- [x] Implement gRPC client (`pkg/agent/client.go`):
  - [x] TLS connection to server
  - [x] Runner token in metadata
  - [x] Automatic reconnection with backoff
- [x] Registration flow:
  - [x] Call `RegisterRunner` on startup
  - [x] Send runner capabilities and labels
  - [x] Receive runner ID from server
- [x] Heartbeat loop:
  - [x] Send `Heartbeat` messages periodically (30s)
  - [x] Include status, uptime, resource usage
  - [x] Detect server disconnection
- [x] Sandbox mode configuration:
  - [x] `runner-is-sandbox` - agent runs in isolated container
  - [x] `runner-creates-sandbox` - agent creates sandbox per task
  - [x] Detect available sandbox types

### 2.3 Control Channel (Server Side) ✓

- [x] Implement bidirectional streaming:
  - [x] Server: send `ServerCommand` messages
  - [x] Agent: send `RunnerMessage` responses
  - [x] Keep-alive pings (via heartbeat)
  - [x] Command handler dispatching (`pkg/agent/command_handler.go`)
  - [x] Session attach/detach handling
- [x] Message routing:
  - [x] Route commands to specific runner by ID
  - [x] Queue commands if runner temporarily disconnected
  - [x] Per-connection bounded channel (100 commands)
- [x] Connection state management:
  - [x] Track runner state: offline, idle, busy, paused
  - [x] Update database on state changes
  - [ ] Emit events for state transitions (Phase 4)
- [x] Error handling:
  - [x] Graceful handling of stream errors
  - [x] Reconnection without losing state (Agent)
  - [ ] Dead letter queue for failed deliveries (Phase 3)
- [x] Server wiring:
  - [x] gRPC server accepts store in config
  - [x] Auto-wire RunnerTokenService, RunnerRegistry, RunnerManager
  - [x] Auto-wire MessageRouter for message handling
  - [x] cmd/server passes database store to gRPC server

### 2.4 Runner Lifecycle ✓

- [x] Runner registration:
  - [x] Validate runner token
  - [x] Create or update runner record
  - [x] Assign runner ID (if new)
  - [x] Set initial status to `idle`
- [x] Runner status transitions:
  ```
  offline → idle (on connect)
  idle → busy (on task assignment, Phase 3)
  busy → idle (on task completion, Phase 3)
  idle/busy → offline (on disconnect)
  * → paused (on pause command)
  ```
- [x] Heartbeat handling:
  - [x] Update `last_seen_at` timestamp
  - [ ] Update resource usage metrics (Phase 3)
  - [x] Detect stale runners (configurable threshold, default 90s)
- [x] Runner disconnection:
  - [x] Mark runner as offline
  - [x] Handle in-flight tasks stub (full impl Phase 3)
  - [x] Detach from sessions stub (full impl Phase 3)

### 2.5 Workspace (Basic) ✓

- [x] Workspace creation (Server side):
  - [x] Generate workspace ID (`pkg/id/id.go`)
  - [x] Create workspace record in database
  - [x] Create workspace directory on runner (`pkg/agent/workspace.go`)
- [x] Workspace Manager (`pkg/server/core/workspace_manager.go`):
  - [x] `WorkspaceManagerInterface` for dependency injection
  - [x] `GetHostPath` - resolve host path based on sandbox mode
  - [x] `EnsureHostDirectory` - create workspace directory on host
  - [x] `CleanupHostDirectory` - remove workspace directory
  - [x] Sandbox mode support: `runner-is-sandbox`, `runner-creates-sandbox`, `none`
- [x] Workspace Adapter (`pkg/server/api/workspace_adapter.go`):
  - [x] API layer adapter for workspace service
  - [x] CRUD operations via WorkspaceManager
- [x] Local volume storage:
  - [x] Mount host directory to container
  - [x] Workspace path: `/workspace` (default)
  - [x] Path resolution by sandbox mode
- [x] Workspace lifecycle:
  - [x] Create on session creation (Server)
  - [x] Ensure workspace exists on attach (Agent)
  - [x] Track workspace state (Agent)
  - [ ] Delete on session termination (configurable) - deferred
- [x] Basic workspace operations (Agent side):
  - [x] List files (for debugging)
  - [x] Get workspace size and info
  - [x] Check workspace exists
  - [x] Clean workspace contents
- [x] Test coverage improvements:
  - [x] `SessionManagerInterface` for testability
  - [x] `PermissionManagerInterface` for testability
  - [x] API package coverage: 86.4%

---

## Phase 3: Session & Task

### 3.1 Session State Machine ✓

- [x] Session states:
  ```
  pending → active → suspended → terminated
                  ↘ resuming ↗
  ```
- [x] State transitions:
  - [x] `pending → active`: Runner assigned
  - [x] `active → suspended`: Idle timeout or permission timeout
  - [x] `suspended → resuming`: User resumes or responds to permission
  - [x] `resuming → active`: Runner attached, context restored
  - [x] `active → terminated`: User terminates or max lifetime reached
- [x] Session creation:
  - [x] Create workspace (validation)
  - [x] Create session record (status: pending)
  - [ ] Request runner from provider (deferred to integration)
  - [x] Attach runner when available (AttachRunner method)
- [x] Session service (`pkg/server/core/session_manager.go`):
  - [x] `Create(opts CreateSessionOptions) (*Session, error)`
  - [x] `Get(id string) (*Session, error)`
  - [x] `List(opts ListOptions) ([]*Session, error)`
  - [x] `Suspend(id string, strategy string) error`
  - [x] `Resume(id string) error`
  - [x] `Terminate(id string) error`

### 3.2 Runner-Session Binding ✓

- [x] Attach runner to session:
  - [x] Send `AttachSession` command to runner (Server: PR #28, Agent: PR #13)
  - [x] Include workspace path
  - [x] Include agent config (API key, model)
  - [x] Include pending permission responses (if resuming)
  - [x] Auto-detach existing sessions from runner before attach (PR #39)
- [x] Detach runner from session:
  - [x] Send `DetachSession` command (G2 - PR #13)
  - [ ] Save context snapshot (G5)
  - [ ] Sync workspace (if configured) (G5)
  - [x] Update session state
- [x] Runner assignment:
  - [x] Validate runner is idle before attach
  - [ ] Label-based matching (profile selector) (deferred)
  - [x] Set runner status (via RunnerManager)
  - [x] Handle no available runners (ErrRunnerNotIdle)

### 3.3 Task Management ✓

- [x] Task creation:
  - [x] Validate session exists
  - [x] Create task record (status: pending)
  - [x] Create first task_run (attempt: 1)
- [x] Task service (`pkg/server/core/task_manager.go`):
  - [x] `Create(sessionID, prompt string, opts TaskOptions) (*Task, error)`
  - [x] `Get(id string) (*Task, error)`
  - [x] `List(sessionID string, opts ListOptions) ([]*Task, error)`
  - [x] `Cancel(id string) error`
  - [x] `Retry(id string) error`
- [x] Task status aggregation:
  - [x] Get latest task_run status
  - [x] Update task status from run status
  - [x] Handle terminal states

### 3.4 Task Run Management ✓

- [x] Run state machine:
  ```
  pending → assigned → running → completed/failed/timeout/canceled
  ```
- [x] Run lifecycle:
  - [x] `pending`: Waiting for runner
  - [x] `assigned`: Runner selected, command sent
  - [x] `running`: Runner acknowledged start
  - [x] Terminal: completed, failed, timeout, canceled
- [x] Task execution flow:
  - [x] Send `ExecuteTask` command to runner
  - [x] Receive `TaskAccepted` ack (OnTaskAccepted)
  - [x] Receive `TaskStarted` ack (OnTaskStarted)
  - [x] Receive `TaskProgress` updates (OnTaskProgress)
  - [x] Receive `TaskCompleted` with result (OnTaskCompleted)
- [x] Timeout enforcement:
  - [x] Per-task timeout (default: 1 hour)
  - [x] Start timer on `assigned` (TaskTimeoutEnforcer)
  - [x] Send `KillTask` on timeout
  - [x] Mark run as `timeout`
- [x] Retry logic:
  - [x] Check `max_retries` and `retry_count` (ShouldRetry)
  - [x] Create new task_run with incremented attempt
  - [ ] Exponential backoff between retries (deferred)

### 3.5 Agent Execution ✓

- [x] Agent interface (`pkg/agent/executor/executor.go`):
  - [x] `Executor` interface with `Execute`, `Kill`, `Name` methods
  - [x] `StreamExecutor` interface for bidirectional communication
  - [x] `OutputHandler` interface for output capture
  - [x] `OutputWriter` adapter implementing `io.Writer`
- [x] Data types (`pkg/agent/executor/executor.go`):
  - [x] `Task` struct with ID, SessionID, RunID, Prompt, etc.
  - [x] `AgentConfig` struct with Agent, APIKey, Model, etc.
  - [x] `Result` struct with Success, ExitCode, Error, TokensIn/Out
  - [x] `PermissionRequest` struct with Tool, Action, Context, RiskLevel
- [x] Event system (`pkg/agent/executor/event.go`):
  - [x] `AgentEvent` struct with unified event format
  - [x] Event types: text, thinking, tool_use, tool_result, error, system, usage
  - [x] Event constructors: `NewTextEvent`, `NewThinkingEvent`, etc.
  - [x] `ToolUseEvent`, `ToolResultEvent`, `UsageEvent` sub-types
- [x] Parser system (`pkg/agent/executor/parser.go`):
  - [x] `AgentEventParser` interface for parsing agent output
  - [x] `ParserFactory` function type
  - [x] `ParserRegistry` for agent-specific parser registration
  - [x] Default global registry with thread-safe access
- [x] Claude Code executor (`pkg/agent/executor/claude/`) ✓
  - [x] `ClaudeExecutor` implementing `Executor` and `StreamExecutor`
  - [x] Stream-JSON message types (`messages.go`)
  - [x] Output parser for Claude stream-json format (`parser.go`)
  - [x] Session ID tracking for resume support
  - [x] Parser registered with default registry
- [x] Environment setup:
  - [x] Set `ANTHROPIC_API_KEY` from AgentConfig
  - [x] Set working directory to workspace
  - [x] Set extra environment from config
- [x] Process management:
  - [x] Process spawning with `exec.Command`
  - [x] Signal handling (SIGTERM, SIGKILL)
  - [x] Graceful termination with timeout
  - [x] Resource cleanup on context cancel

### 3.6 Log Streaming ✓

- [x] Log stream implementation (Agent-side):
  - [x] `LogStreamer` interface for log streaming abstraction (`pkg/agent/log_streamer.go`)
  - [x] `GRPCLogStreamer` implementing gRPC client streaming
  - [x] Thread-safe with `sync.Mutex` and atomic operations
  - [x] Automatic sequence numbering and timestamps
  - [x] Graceful handling of stream lifecycle (Start/Send/Close)
- [x] TaskRunner integration:
  - [x] Stream logs in real-time during task execution via `HandleOutput`
  - [x] Graceful degradation - log streaming failures don't block task execution
  - [x] Proper cleanup with deferred `Close()`
  - [x] Log level mapping (stdout→info, stderr→error, system→info)
- [x] Log storage (`migrations/001_initial.up.sql`):
  - [x] `logs` partitioned table with TEXT content
  - [x] `log_` prefixed IDs (`pkg/id/id.go`)
  - [x] `Log` model (`pkg/store/models.go`)
  - [x] Partition management functions
- [x] Log entry structure:
  - [x] task_id, run_id, session_id
  - [x] stream (stdout, stderr, system)
  - [x] level (debug, info, warn, error)
  - [x] content, sequence number, timestamp
- [x] Server-side handling:
  - [x] Validate `StreamLogsInit` message
  - [x] Verify runner authorization
  - [x] Persist logs to database (never drop)
  - [x] Forward to real-time subscribers (stub - can drop under pressure)
- [x] Batching:
  - [ ] Buffer logs on agent side (100ms or 100 entries) - deferred
  - [x] Batch insert on server (100 entries)
  - [x] Flush on stream close
- [x] Sequence tracking:
  - [x] Monotonic sequence per run
  - [x] Detect gaps for debugging (in log entry metadata)
  - [x] Store sequence in database
- [x] Tests:
  - [x] `GRPCLogStreamer` lifecycle tests (Start, Send, Close)
  - [x] `TaskRunner` log streaming integration tests
  - [x] Error handling tests (start error, send error)

### 3.7 Permission Handling ✓

- [x] Permission request detection (Agent-side):
  - [x] Parse Claude Code output for tool_use events
  - [x] Check `IsPermissionRequired()` for tool name
  - [x] Extract tool name, action from event
  - [x] Call `handler.HandlePermissionRequest()` and block
- [x] Permission request flow:
  - [x] Agent sends `PermissionRequest` message
  - [x] Server creates permission_request record (PermissionManager)
  - [x] MessageRouter routes to PermissionManager
  - [ ] Server notifies subscribers (WebSocket, webhook) - Phase 4
- [x] Permission response flow:
  - [x] User approves/denies via API (PermissionManager.Respond)
  - [x] Server sends `ApprovePermission` command with original request ID (PR #39)
  - [x] Session resumed if suspended
  - [x] Agent receives and unblocks executor (`TaskRunner.HandlePermissionResponse`)
- [x] Timeout handling:
  - [x] `suspend_after_seconds`: auto-suspend session (PermissionTimeoutEnforcer)
  - [x] Permission stays pending (no auto-deny)
  - [x] On resume: deliver cached response to agent via `pending_permissions`
- [x] Permission caching:
  - [x] Store response in database
  - [x] Include `responded_by`, `response_reason`, `responded_at`
  - [x] Deliver on session resume via `pending_permissions` in `AttachSession`

---

## Phase 4: HTTP API & CLI

### 4.1 CLI Framework ✓

- [x] Set up Cobra CLI framework (`cmd/mctl/`):
  - [x] Root command with global persistent flags
  - [x] `--server` / `-s` (API URL)
  - [x] `--api-key` / `-k` (API Key)
  - [x] `--output` / `-o` (table|json|yaml)
  - [x] `--config` (config file path)
  - [x] `--context` (context name)
- [x] Config management commands:
  - [x] `set-context` - Create/update context
  - [x] `use-context` - Switch active context
  - [x] `view` - Show config
  - [x] `delete-context` - Remove context
  - [x] `get-contexts` - List all contexts
- [x] Config file at `~/.config/marionette/config.yaml`
- [x] Environment overrides: `MARIONETTE_API_URL`, `MARIONETTE_API_KEY`, `MARIONETTE_CONTEXT`
- [x] Define Client interface (`pkg/client/interface.go`)
- [x] Implement mock client for testing (`pkg/client/mock.go`)
- [x] Session commands (with mock):
  - [x] `mctl sessions create`
  - [x] `mctl sessions list`
  - [x] `mctl sessions get`
  - [x] `mctl sessions suspend`
  - [x] `mctl sessions resume`
  - [x] `mctl sessions terminate`
- [x] Task commands (with mock):
  - [x] `mctl tasks create`
  - [x] `mctl tasks list`
  - [x] `mctl tasks get`
  - [x] `mctl tasks logs`
  - [x] `mctl tasks cancel`
- [x] Output formatting:
  - [x] Table format using `text/tabwriter`
  - [x] JSON format with `encoding/json`
  - [x] YAML format with `gopkg.in/yaml.v3`
- [x] CLI command tests with mock client

### 4.2 Service Interfaces ✓

- [x] Define service interfaces (`pkg/server/public/`):
  - [x] `SessionService` - CRUD + lifecycle (suspend, resume, terminate)
  - [x] `TaskService` - CRUD + cancel/retry + log streaming
  - [x] `RunnerService` - Read-only (Get, List)
  - [x] `PermissionService` - Get, List, Approve, Deny
  - [x] `LogStream` interface for real-time log delivery
- [x] Implement mock services for testing HTTP handlers:
  - [x] `MockSessionService` with function stubs
  - [x] `MockTaskService` with log management
  - [x] `MockRunnerService` with label matching
  - [x] `MockPermissionService` with state validation
  - [x] `MockLogStream` for testing log streaming
- [x] Custom error types:
  - [x] `InvalidStateError` - Invalid state transition
  - [x] `MaxRetriesExceededError` - Retry limit reached
  - [x] `ValidationError` - Input validation failure
  - [x] `NotAuthorizedError` - Permission denied
- [x] Comprehensive mock service tests

### 4.3 HTTP Server Setup ✓

- [x] Set up HTTP servers:
  - [x] Port 8080: Public API (API key auth)
  - [ ] Port 8081: Admin API (Basic Auth) - deferred to Phase 4.5
- [x] Middleware stack:
  - [x] Request ID injection
  - [x] Structured logging
  - [x] Panic recovery
  - [x] CORS (configurable)
  - [ ] Rate limiting (optional) - deferred
- [x] Authentication middleware:
  - [x] Extract `Authorization: Bearer <token>` header
  - [x] Validate API key
  - [ ] Inject tenant_id into context - deferred to tenant isolation
  - [x] Scope checking

### 4.4 Public API Endpoints ✓

- [x] Sessions API (`/api/v1/sessions`):
  - [x] `POST /` - Create session
  - [x] `GET /` - List sessions
  - [x] `GET /:id` - Get session
  - [x] `POST /:id/suspend` - Suspend session
  - [x] `POST /:id/resume` - Resume session
  - [x] `DELETE /:id` - Terminate session
- [x] Tasks API (`/api/v1/tasks`):
  - [x] `POST /` - Create task (with session_id)
  - [x] `GET /` - List tasks (filter by session)
  - [x] `GET /:id` - Get task
  - [x] `POST /:id/cancel` - Cancel task
  - [x] `POST /:id/retry` - Retry task
  - [x] `GET /:id/logs` - Get task logs
- [x] Runners API (`/api/v1/runners`):
  - [x] `GET /` - List runners
  - [x] `GET /:id` - Get runner
- [x] Permissions API (`/api/v1/permissions`):
  - [x] `GET /` - List pending permissions
  - [x] `GET /:id` - Get permission details
  - [x] `POST /:id/approve` - Approve permission
  - [x] `POST /:id/deny` - Deny permission
- [x] Logs API (`/api/v1/logs`):
  - [x] `GET /:task_id/stream` - WebSocket log stream (implemented in Phase 4.6)

### 4.5 Admin API Endpoints ✓

- [x] API Keys (`/admin/api/v1/keys`):
  - [x] `POST /` - Create API key
  - [x] `GET /` - List API keys
  - [x] `GET /:id` - Get API key
  - [x] `POST /:id/revoke` - Revoke API key
- [x] Agent Configs (`/admin/api/v1/agent-configs`):
  - [x] `POST /` - Create agent config
  - [x] `GET /` - List agent configs
  - [x] `GET /:id` - Get agent config
  - [x] `PATCH /:id` - Update agent config
  - [x] `DELETE /:id` - Delete agent config
- [x] Provider Configs (`/admin/api/v1/provider-configs`):
  - [x] `POST /` - Create provider config
  - [x] `GET /` - List provider configs
  - [x] `GET /:id` - Get provider config
  - [x] `PATCH /:id` - Update provider config
  - [x] `DELETE /:id` - Delete provider config
- [x] Runners (`/admin/api/v1/runners`):
  - [x] `POST /spawn` - Spawn new runner
  - [x] `DELETE /:id` - Destroy runner
- [x] Basic Auth middleware for Admin API
- [x] Admin API tests (79% coverage)

### 4.6 WebSocket Support ✓

- [x] Log streaming WebSocket:
  - [x] Endpoint: `/api/v1/logs/:task_id/stream`
  - [x] Authentication via Authorization header
  - [x] Real-time log delivery
  - [x] Backpressure handling (drop if channel full)
  - [x] Ping/pong keep-alive (30s interval)
- [x] Event streaming WebSocket:
  - [x] Endpoint: `/api/v1/events`
  - [x] Subscribe to event types via query params
  - [x] Filter by labels (JSON format)
  - [x] Ping/pong keep-alive
- [x] Mock implementations for testing:
  - [x] `MockLogStreamService` with subscribe/publish
  - [x] `MockEventStreamService` with subscribe/publish
- [x] WebSocket tests (89.8% coverage for public package)

### 4.7 OpenAPI Documentation ✓

- [x] Generate OpenAPI spec:
  - [x] Manual OpenAPI 3.1.0 spec at `api/openapi.yaml`
  - [x] Document all endpoints (Public API + Admin API)
  - [x] Include request/response schemas
  - [x] Include authentication requirements (Bearer token, Basic Auth)
- [x] Serve documentation:
  - [x] Swagger UI at `/docs`
  - [x] OpenAPI YAML at `/openapi.yaml`

### 4.8 CLI Advanced Commands ✓

- [ ] Interactive session commands:
  - [ ] `mctl sessions attach <id>` - interactive mode with WebSocket (deferred)
- [ ] Task retry command:
  - [ ] `mctl tasks retry <id>` (deferred)
- [x] Runner commands:
  - [x] `mctl runners list`
  - [x] `mctl runners get <id>`
- [x] Permission commands:
  - [x] `mctl permissions list [--pending]`
  - [x] `mctl permissions approve <id> [--reason <reason>]`
  - [x] `mctl permissions deny <id> [--reason <reason>]`
- [x] Admin commands:
  - [x] `mctl admin keys create --name <name> --scopes <scopes>`
  - [x] `mctl admin keys list`
  - [x] `mctl admin keys revoke <id>`
  - [x] `mctl admin agent-configs create --agent claude --api-key <key>`
  - [x] `mctl admin agent-configs list`
  - [x] `mctl admin runners spawn --provider <provider>`
  - [x] `mctl admin runners destroy <id>`
- [x] Real HTTP client implementation:
  - [x] `pkg/client/http.go` - HTTPClient for public API
  - [x] `pkg/client/admin.go` - HTTPAdminClient for admin API
  - [x] CLI wiring with PersistentPreRunE hook
  - [x] Client tests with httptest

---

## Phase 5: Suspend/Resume

### 5.1 Suspend Strategy Framework ✓

- [x] Define suspend strategies:
  ```go
  type SuspendStrategy string
  const (
      SuspendStrategyPause                    // Memory preserved, instant resume
      SuspendStrategySnapshot                 // Full VM/container snapshot
      SuspendStrategyTerminatePreserveStorage // Terminate but keep PV/volume
      SuspendStrategyReleaseToPool            // Sync workspace to CAS, release runner
      SuspendStrategyTerminate                // Full termination
  )
  ```
- [x] SuspendableProvider interface:
  ```go
  type SuspendableProvider interface {
      Provider
      Suspend(ctx, runnerID string, opts SuspendOptions) (*SuspendResult, error)
      Resume(ctx, sessionID string, opts ResumeOptions) (*RunnerInstance, error)
  }
  ```
- [x] Suspend configuration per provider:
  - [x] Default strategy
  - [x] Fallback strategy
  - [x] Min/max suspend duration
  - [x] Workspace sync options

### 5.2 Docker Pause/Resume ✓

- [x] Implement pause strategy for Docker:
  - [x] `docker pause <container>` on suspend
  - [x] `docker unpause <container>` on resume
  - [x] Memory preserved, instant resume
- [x] Fallback to terminate_preserve_storage:
  - [x] Stop container but keep volume
  - [x] Create new container with same volume on resume

### 5.3 Context Snapshot ✓

- [x] Define context snapshot schema:
  ```go
  type ContextSnapshot struct {
      WorkingDirectory string
      Environment      map[string]string
      ConversationID   string  // For agents that support it
      AgentState       json.RawMessage
      LastActivity     time.Time
  }
  ```
- [x] Save context on suspend:
  - [x] Store in session.context_snapshot (JSONB)
  - [x] Store agent_version for compatibility check
  - [ ] Request context from agent via gRPC (Agent-side)
- [x] Restore context on resume:
  - [x] Parse context snapshot on resume
  - [x] Return context in ResumeResult
  - [ ] Send context in AttachSession command (Agent-side)
  - [ ] Agent restores working directory, environment (Agent-side)

### 5.4 CAS Storage Implementation ✓

- [x] Implement content-defined chunking (`pkg/storage/cas/`):
  - [x] Use restic chunker (Rabin fingerprinting)
  - [x] Chunk size: 512KB min, 1MB target, 8MB max
  - [x] SHA-256 hash per chunk
- [x] Chunk storage:
  - [x] Local storage provider (development)
  - [x] S3 storage provider (production)
  - [ ] GCS storage provider (production) - deferred
  - [x] Tenant-scoped paths: `chunks/{tenant_id}/{hash[:2]}/{hash}.blob.enc`
- [x] Chunk encryption:
  - [x] Per-tenant DEK from envelope encryption
  - [x] zstd compression before encryption
  - [x] AES-256-GCM encryption
- [x] Manifest management:
  - [x] JSONL format for streaming
  - [x] Header line with metadata
  - [x] One file entry per line
  - [x] Store in `manifests/{tenant_id}/{workspace_id}/{manifest_id}.jsonl.zst.enc`
- [x] Single chunk mode:
  - [x] For workspaces < 100MB
  - [x] Create tar.zst archive
  - [x] Store as single encrypted chunk

### 5.5 Workspace Sync ✓

- [x] Sync service (`pkg/storage/cas/sync.go`):
  - [x] `Sync(ctx, workspaceID, tenantID, srcDir string) error`
  - [x] `Restore(ctx, workspaceID, tenantID, dstDir string) error`
  - [x] `ValidateManifest(ctx, manifest *Manifest) error`
- [x] Incremental sync:
  - [x] Get previous manifest chunk hashes
  - [x] Only upload new/modified chunks
  - [x] Diff functionality (Added/Modified/Deleted/Unchanged)
  - [x] ParentID tracking for manifest chains
- [x] Memory-efficient implementation:
  - [x] StreamingProcessor for file chunking via channels
  - [x] ChunkUploader for concurrent uploads with progress
  - [x] StreamingFileReader for restore
  - [x] Parallel upload with bounded concurrency

### 5.6 Garbage Collection ✓

- [x] Mark-and-sweep GC (`pkg/storage/cas/gc.go`, `pkg/jobs/chunk_gc.go`):
  - [x] Phase 1: Mark orphaned chunks (ref_count = 0)
  - [x] Set deleted_at timestamp (soft delete)
  - [x] Phase 2: Sweep after grace period (7 days)
  - [x] Re-verify ref_count before physical delete
- [x] Chunk resurrection:
  - [x] Clear deleted_at if chunk referenced during grace period
  - [x] Atomic ref_count update via ResurrectIfNeeded
- [x] GC job scheduling:
  - [x] ChunkGCJob for per-tenant periodic GC
  - [x] ChunkGCScheduler for multi-tenant management
  - [x] Configurable interval (default: 24h)

### 5.7 Permission Timeout Suspend

- [ ] Implement auto-suspend on permission timeout:
  - [ ] Start timer when permission request created
  - [ ] `suspend_after_seconds` default: 1800 (30 min)
  - [ ] Trigger session suspend on timeout
- [ ] Session suspend flow:
  - [ ] Save context snapshot
  - [ ] Sync workspace (if configured)
  - [ ] Execute provider suspend strategy
  - [ ] Update session status to `suspended`
  - [ ] Permission request stays `pending`
- [ ] Resume on permission response:
  - [ ] User responds to pending permission
  - [ ] Trigger session resume
  - [ ] Acquire runner (same or new)
  - [ ] Restore workspace and context
  - [ ] Deliver permission response to agent

---

## Phase 6: Security Hardening

### 6.1 mTLS Implementation ✓

- [x] Upgrade from TLS to mTLS:
  - [x] Server presents certificate (existing)
  - [x] Server verifies client certificate (existing)
  - [x] Client presents certificate (existing)
- [x] Certificate management:
  - [x] CA certificate generation script (`scripts/certs/generate-ca.sh`)
  - [x] Server certificate generation (`scripts/certs/generate-server.sh`)
  - [x] Agent certificate generation (`scripts/certs/generate-client.sh`)
  - [x] Unified Makefile (`scripts/certs/Makefile`, `make certs`)
- [x] Certificate configuration:
  ```yaml
  tls:
    ca_cert: /etc/marionette/ca.crt
    server_cert: /etc/marionette/server.crt
    server_key: /etc/marionette/server.key
    client_cert: /etc/marionette/client.crt  # Agent
    client_key: /etc/marionette/client.key
    verify_client: true  # Require client cert
  ```
- [x] Certificate hot-reload (`pkg/crypto/certreloader/`):
  - [x] Reload certificates without restart
  - [x] File watcher for cert changes (fsnotify)
  - [x] Atomic pointer for lock-free access
  - [x] Debounce support for simultaneous updates
  - [x] Graceful degradation on reload failure
- [x] mTLS integration tests (`pkg/server/grpc/mtls_integration_test.go`):
  - [x] Successful mTLS connection
  - [x] Rejection without client certificate
  - [x] Rejection with wrong CA
  - [x] Rejection with expired certificate

### 6.2 Sandbox Verification ✓

- [x] Sandbox escape testing (`pkg/sandbox/verify.go`):
  - [x] Verify container isolation
  - [x] Test filesystem access restrictions
  - [x] Test network isolation (metadata endpoint, privileged ports)
  - [x] Test process isolation (ptrace, host process visibility)
- [x] Resource limit enforcement (`pkg/sandbox/limits.go`):
  - [x] Memory limits (cgroup v1/v2)
  - [x] CPU limits (cgroup v1/v2)
  - [x] Disk quota detection
  - [x] Process count limits (pids cgroup)
  - [x] Open file limits
- [x] Sandbox type detection (`pkg/sandbox/detect.go`):
  - [x] Detect Docker, gVisor, Firecracker, Kata
  - [x] Detect VM environment
  - [x] Report capabilities to server

### 6.3 Audit Logging

- [x] Audit logging package (`pkg/audit/`):
  - [x] Core types: `Event`, `Actor`, `Filter`, `QueryResult`, `StoredEvent`
  - [x] `Logger` interface with `Log()` and `Query()` methods
  - [x] `Store` interface for pluggable backends
  - [x] `EventBuilder` fluent API for constructing events
  - [x] Memory store for testing
  - [x] Store adapter for postgres
- [x] Action constants and helper functions:
  - [x] Permission actions (approved/denied/canceled)
  - [x] Session actions (created/resumed/suspended/terminated)
  - [x] Task actions (created/started/completed/failed/canceled)
  - [x] Runner actions (connected/disconnected/assigned/released)
  - [x] Config actions (created/updated/deleted)
  - [x] API key actions (created/revoked/used)
- [x] Action log content:
  - [x] Actor (api_key, system, runner, user)
  - [x] Action type
  - [x] Resource type and ID
  - [x] Details (JSON)
  - [x] IP address, user agent
  - [x] Success/failure
- [ ] Admin API for audit logs:
  - [ ] `GET /admin/api/v1/action-logs`
  - [ ] Filter by actor, action, resource, time range
  - [ ] Pagination support
- [ ] Integration with existing services (deferred)

### 6.4 Network Isolation (Basic)

- [ ] Implement allow_list network policy:
  - [ ] DNS pinning at task start
  - [ ] Resolve allowed hosts to IPs
  - [ ] Configure iptables/pf rules
- [ ] Metadata endpoint blocking:
  - [ ] Always block 169.254.169.254
  - [ ] Block link-local addresses
  - [ ] Block localhost (SSRF prevention)
- [ ] Docker network isolation:
  - [ ] Create isolated bridge network
  - [ ] Apply iptables rules to container
  - [ ] Block inter-container communication
- [ ] Policy configuration:
  ```yaml
  network:
    level: allow_list
    allowed_hosts:
      - "*.github.com"
      - "api.anthropic.com"
      - "pypi.org"
  ```

---

## Phase 7: WebUI

### 7.1 Project Setup ✓

- [x] Initialize React project:
  - [x] Vite + React + TypeScript
  - [x] TanStack Router for routing
  - [x] TanStack Query for data fetching
  - [x] Tailwind CSS for styling
- [x] Directory structure:
  ```
  web/
  ├── src/
  │   ├── components/
  │   ├── routes/
  │   ├── hooks/
  │   ├── api/
  │   └── lib/
  ├── package.json
  └── vite.config.ts
  ```
- [x] API client setup:
  - [x] Axios wrapper with interceptors
  - [x] Authentication handling (API Key + Basic Auth)
  - [x] Error handling
  - [x] TypeScript types from OpenAPI

### 7.2 Admin UI ✓

- [x] Login page:
  - [x] Basic Auth login form
  - [x] Session management (localStorage)
  - [x] Redirect to dashboard on success
- [x] API Key management:
  - [x] List API keys (masked)
  - [x] Create new key (show once)
  - [x] Revoke key
  - [x] Show last used time
- [x] Agent Config management:
  - [x] List agent configs
  - [x] Create/edit form
  - [x] API key input (masked)
  - [ ] Test connection button
- [x] Provider Config management:
  - [x] List provider configs
  - [x] Create/edit form
  - [x] JSON editor for config

### 7.3 Dashboard ✓

- [x] Overview page:
  - [x] Active sessions count
  - [x] Running tasks count
  - [x] Online runners count
  - [x] Recent activity feed
- [x] Sessions page:
  - [x] Session list with status
  - [ ] Create session form
  - [x] Session detail view
  - [ ] Suspend/resume/terminate buttons (UI only)
- [x] Task execution page:
  - [ ] Prompt input
  - [x] Real-time log viewer (WebSocket)
  - [x] Task status display
  - [ ] Cancel button (UI only)
- [x] Permission approval:
  - [x] Pending permissions list
  - [x] Permission detail (tool, action, context)
  - [x] Approve/deny buttons
  - [ ] Timeout countdown

### 7.4 Real-time Log Viewer ✓

- [x] WebSocket connection:
  - [x] Connect to log stream endpoint
  - [x] Reconnection on disconnect
  - [x] Connection status indicator
- [x] Log display:
  - [x] ANSI color support
  - [x] Auto-scroll to bottom
  - [x] Pause auto-scroll on scroll up
  - [ ] Search/filter logs
- [x] Log buffering:
  - [ ] Virtual scrolling for large logs
  - [x] Limit displayed lines (configurable)
  - [x] Download full logs button

### 7.5 Embeddable React Components

- [ ] Package setup:
  - [ ] Separate npm package: `@marionette/react`
  - [ ] Rollup/Vite for library build
  - [ ] TypeScript declarations
- [ ] Components:
  - [ ] `<MarionetteProvider>` - Context with API key
  - [ ] `<SessionList>` - List sessions
  - [ ] `<TaskViewer>` - View task with logs
  - [ ] `<LogStream>` - Real-time log component
  - [ ] `<PermissionDialog>` - Approve/deny modal
  - [ ] `<RunnerStatus>` - Runner status badge
- [ ] Storybook documentation:
  - [ ] Component stories
  - [ ] Usage examples
  - [ ] Props documentation

---

## Phase 8: Production Ready

### 8.1 Docker & Deployment ✓

- [x] Server Dockerfile (`deploy/docker/Dockerfile.server`):
  - [x] Multi-stage build (golang:1.25-bookworm → distroless)
  - [x] Minimal runtime image (gcr.io/distroless/static-debian12:nonroot)
  - [x] Non-root user
  - [x] Protobuf generation in build
- [x] Agent Dockerfile (`deploy/docker/Dockerfile.agent`):
  - [x] Include common tools (git, curl, jq, openssh-client)
  - [x] Non-root user (UID 1000)
  - [x] Workspace directory setup
- [x] CLI Dockerfile (`deploy/docker/Dockerfile.mctl`):
  - [x] Minimal distroless image
- [x] docker-compose.yaml (`deploy/docker/`):
  - [x] Server + PostgreSQL + Agent (agent via profile)
  - [x] Volume mounts for persistence
  - [x] Auto-initialize database schema
  - [x] Environment variables
  - [x] Health checks
- [x] Kubernetes manifests (`deploy/kubernetes/`):
  - [x] Kustomize base + overlays (dev/prod)
  - [x] Deployment for server and agent
  - [x] Service and Ingress
  - [x] ConfigMap and Secrets
  - [x] PersistentVolumeClaim for workspaces
  - [x] NetworkPolicy
  - [x] HorizontalPodAutoscaler
- [x] Helm chart (`deploy/helm/marionette/`):
  - [x] Chart with PostgreSQL subchart dependency
  - [x] Configurable values.yaml
  - [x] All templates (deployments, services, ingress, etc.)

### 8.2 Documentation

- [ ] README.md:
  - [ ] Project overview
  - [ ] Quick start guide
  - [ ] Architecture diagram
  - [ ] Links to detailed docs
- [ ] Installation guide:
  - [ ] Docker Compose setup
  - [ ] Kubernetes setup
  - [ ] Configuration reference
- [ ] API documentation:
  - [ ] OpenAPI spec
  - [ ] Endpoint descriptions
  - [ ] Authentication guide
- [ ] CLI reference:
  - [ ] Command list
  - [ ] Options and flags
  - [ ] Examples
- [ ] Integration guide:
  - [ ] Embedding in applications
  - [ ] Webhook setup
  - [ ] Custom providers
- [ ] Security best practices:
  - [ ] mTLS setup
  - [ ] API key management
  - [ ] Network isolation
  - [ ] Audit logging

### 8.3 Observability

- [ ] Structured logging:
  - [ ] JSON format in production
  - [ ] Request ID correlation
  - [ ] Log levels (debug, info, warn, error)
  - [ ] zap or zerolog
- [ ] Prometheus metrics:
  - [ ] `/metrics` endpoint
  - [ ] Request duration histograms
  - [ ] Active connections gauge
  - [ ] Task counts by status
  - [ ] Error rates
- [ ] Health endpoints:
  - [ ] `/health/live` - Liveness probe
  - [ ] `/health/ready` - Readiness probe
  - [ ] Database connectivity check
- [ ] OpenTelemetry traces (optional):
  - [ ] Trace context propagation
  - [ ] Span creation for key operations
  - [ ] Export to Jaeger/Zipkin

### 8.4 Performance Testing

- [ ] Define performance targets:
  - [ ] Task throughput: 100+ concurrent tasks per server
  - [ ] Log latency: <100ms from agent to subscriber
  - [ ] API latency: p99 <50ms for CRUD operations
  - [ ] Workspace sync: 100MB in <30s
- [ ] Load testing infrastructure:
  - [ ] k6 or vegeta scripts for API
  - [ ] Mock agent for concurrent task simulation
  - [ ] Log flooding test (10K logs/sec per agent)
- [ ] Benchmark suite:
  - [ ] CAS chunking performance (MB/s)
  - [ ] Encryption overhead measurement
  - [ ] Database query profiling (EXPLAIN ANALYZE)
- [ ] Performance regression CI:
  - [ ] Baseline metrics capture
  - [ ] Automated comparison on PRs
  - [ ] Alert on >10% regression
- [ ] Optimization:
  - [ ] Connection pooling tuning
  - [ ] Query optimization
  - [ ] Memory profiling (pprof)
  - [ ] GC tuning if needed

---

## Phase 9: Advanced Providers

### 9.1 Provider Interface Extensions

- [ ] SuspendableProvider interface:
  - [ ] All providers implement Suspend/Resume
  - [ ] Fallback strategy handling
  - [ ] Capability declaration
- [ ] SnapshotProvider interface:
  - [ ] Snapshot creation
  - [ ] Snapshot restore
  - [ ] Snapshot listing and deletion
- [ ] Provider config from database:
  - [ ] Load provider_configs table
  - [ ] Hot-reload on config change
  - [ ] Default provider per environment

### 9.2 Kubernetes Provider

- [ ] Pod lifecycle management:
  - [ ] Create Pod from template
  - [ ] Wait for Pod ready
  - [ ] Delete Pod on destroy
- [ ] Configuration:
  - [ ] Namespace
  - [ ] Service account
  - [ ] Resource limits/requests
  - [ ] Node selectors
  - [ ] Tolerations
- [ ] Storage:
  - [ ] PersistentVolumeClaim for workspace
  - [ ] Storage class configuration
  - [ ] Volume expansion
- [ ] Suspend strategy:
  - [ ] `terminate_preserve_storage`: Delete Pod, keep PVC
  - [ ] Resume: Create new Pod with same PVC
- [ ] NetworkPolicy integration:
  - [ ] Create NetworkPolicy per session
  - [ ] Egress rules from allowed_hosts
  - [ ] Delete on session termination

### 9.3 E2B Provider

- [ ] E2B API integration:
  - [ ] API client setup
  - [ ] Authentication
  - [ ] Sandbox creation
- [ ] Sandbox lifecycle:
  - [ ] Create sandbox from template
  - [ ] Destroy sandbox
  - [ ] Get sandbox status
- [ ] Pause/Resume:
  - [ ] Use E2B native suspend
  - [ ] Instant resume
- [ ] Snapshot support:
  - [ ] Create sandbox snapshot
  - [ ] Restore from snapshot

### 9.4 Pool Provider

- [ ] Pool registration:
  - [ ] Runners connect to pool by name
  - [ ] Validate runner token
  - [ ] Check required labels
- [ ] Runner matching:
  - [ ] Label selector matching
  - [ ] Capability requirements
  - [ ] Prefer least recently used
- [ ] Init/cleanup scripts:
  - [ ] Execute init script on task start
  - [ ] Execute cleanup script on task end
  - [ ] Script timeout handling
  - [ ] Safe environment variable passing
- [ ] Watchdog implementation (`pkg/pool/watchdog.go`):
  - [ ] Health check runners periodically
  - [ ] Detect tainted runners
  - [ ] Clean or destroy tainted runners
  - [ ] Enforce scaling constraints
- [ ] Taint mechanism:
  - [ ] Detect crash, timeout, state pollution
  - [ ] Mark runner as tainted
  - [ ] Cleanup attempt
  - [ ] Destroy if cleanup fails
- [ ] Pool scaling:
  - [ ] min_runners, max_runners configuration
  - [ ] Scale up threshold
  - [ ] Scale down delay
  - [ ] Max tasks per runner (recycle)

### 9.5 Profiles

- [ ] Profile schema:
  ```go
  type Profile struct {
      ID          string
      Name        string
      Description string
      ProviderConfigID string
      Resources   ResourceConfig
      Network     NetworkConfig
      InitScript  string
      CleanupScript string
      Tunnels     []TunnelConfig
      Selector    map[string]string
  }
  ```
- [ ] Built-in profiles:
  - [ ] `dev-small`: 2 CPU, 4GB RAM
  - [ ] `dev-medium`: 4 CPU, 8GB RAM
  - [ ] `dev-large`: 8 CPU, 16GB RAM
  - [ ] `ios-dev`: macOS pool, Xcode
  - [ ] `android-dev`: Android emulator
- [ ] Profile CRUD:
  - [ ] Create custom profiles
  - [ ] Update/delete profiles
  - [ ] List profiles
- [ ] Profile selection:
  - [ ] `--profile` flag in session creation
  - [ ] Profile applied to runner selection
  - [ ] Profile settings override defaults

---

## Phase 10: Advanced Features

### 10.1 HTTP/TCP Tunneling

- [ ] Tunnel relay implementation:
  - [ ] WebSocket-based tunnel
  - [ ] Proxy HTTP requests to runner port
  - [ ] Proxy TCP connections
- [ ] Dynamic subdomain assignment:
  - [ ] Generate unique subdomain per tunnel
  - [ ] DNS or wildcard certificate
  - [ ] Route based on subdomain
- [ ] Tunnel token authentication:
  - [ ] Generate `ttok_` token per tunnel
  - [ ] Validate on connection
  - [ ] Token expiration
- [ ] Security:
  - [ ] Rate limiting per tunnel
  - [ ] Connection limits
  - [ ] SSRF protection (block internal IPs)

### 10.2 Desktop/Browser Streaming

- [ ] Desktop streaming (Linux/macOS):
  - [ ] Selkies GStreamer integration
  - [ ] WebRTC signaling server
  - [ ] Browser viewer component
  - [ ] Keyboard/mouse input forwarding
  - [ ] Clipboard sync
- [ ] Browser streaming (headless Chrome):
  - [ ] Chrome DevTools Protocol integration
  - [ ] Page capture to video stream
  - [ ] Input forwarding
- [ ] Android emulator streaming:
  - [ ] scrcpy integration
  - [ ] H.264 to WebRTC transcoding
  - [ ] Touch input forwarding via ADB
- [ ] iOS Simulator streaming:
  - [ ] macOS screen capture (simctl)
  - [ ] Selkies for video encoding
  - [ ] Touch input via simctl

### 10.3 Advanced Network Isolation

- [ ] Proxy mode:
  - [ ] Egress proxy deployment
  - [ ] TLS termination (MITM)
  - [ ] Request logging
  - [ ] Content filtering
- [ ] Air-gapped mode:
  - [ ] Block all outbound except control plane
  - [ ] Inbound tunnels only (for streaming)
  - [ ] Strict firewall rules
- [ ] DNS rebinding prevention:
  - [ ] Pin DNS at task start
  - [ ] Firewall rules use IPs not hostnames
  - [ ] Refresh interval for long tasks

### 10.4 Scheduled Tasks

- [ ] Scheduled task schema:
  - [ ] Cron expression
  - [ ] Timezone
  - [ ] Prompt template
  - [ ] Error handling policy
- [ ] Scheduler implementation:
  - [ ] Parse cron expressions
  - [ ] Calculate next run time
  - [ ] Trigger task creation
- [ ] Session lifecycle modes:
  - [ ] `on_demand`: Suspend when idle
  - [ ] `always_on`: Never auto-suspend
  - [ ] `scheduled`: Activate on schedule
- [ ] Error handling:
  - [ ] `continue`: Keep running
  - [ ] `pause_on_failure`: Pause until manual resume
  - [ ] `disable_on_failure`: Disable after N failures
- [ ] CLI commands:
  - [ ] `mctl scheduled-tasks create`
  - [ ] `mctl scheduled-tasks list`
  - [ ] `mctl scheduled-tasks pause/resume`
  - [ ] `mctl scheduled-tasks trigger`

### 10.5 Integration Hooks

- [ ] Webhooks:
  - [ ] Webhook configuration storage
  - [ ] Event dispatcher
  - [ ] Event types: session.*, task.*, runner.*, permission.*
  - [ ] Retry with exponential backoff
  - [ ] Webhook signature (HMAC)
- [ ] Prometheus metrics:
  - [ ] Usage metrics by label
  - [ ] Token counting per agent
  - [ ] Resource consumption tracking
  - [ ] Custom metric registration
- [ ] Usage API:
  - [ ] `/api/v1/usage` endpoint
  - [ ] Aggregation by label
  - [ ] Time range filtering
  - [ ] Group-by support
  - [ ] Export to CSV/JSON
- [ ] Event streaming:
  - [ ] WebSocket event stream
  - [ ] Server-Sent Events (SSE) alternative
  - [ ] Selector-based filtering
  - [ ] Reconnection support

### 10.6 Log Archiving

- [ ] Log archiver job (`pkg/jobs/log_archiver.go`):
  - [ ] Find sessions for archival (terminated + retention passed)
  - [ ] Stream logs to object storage (JSONL.zst)
  - [ ] Create log_archive record
  - [ ] Delete logs from PostgreSQL
- [ ] Log retrieval:
  - [ ] Check PostgreSQL first (hot)
  - [ ] Fall back to object storage (cold)
  - [ ] Stream from archive
- [ ] Partition management:
  - [ ] Create daily partitions ahead
  - [ ] Drop old partitions after archival
  - [ ] pg_cron integration
