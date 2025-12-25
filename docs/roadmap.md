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

### 1.5 Store Layer

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
- [ ] Implement PostgreSQL store (`pkg/store/postgres/`):
  - [x] `postgres.go` - connection pool setup
  - [x] `runners.go` - runner CRUD
  - [ ] `sessions.go` - session CRUD
  - [ ] `tasks.go` - task/task_run CRUD
  - [ ] `workspaces.go` - workspace CRUD
  - [ ] `auth.go` - api_keys, runner_tokens
  - [ ] `configs.go` - agent_configs, provider_configs, profiles
- [x] Create migration system (golang-migrate):
  - [x] `migrations/001_initial.up.sql` - from schema.sql
  - [x] `migrations/001_initial.down.sql`
  - [x] Migration runner in Makefile
- [ ] Implement tenant isolation:
  - [ ] `TenantContext` wrapper for queries
  - [ ] Automatic `tenant_id` injection
  - [ ] Cross-tenant validation in store methods
- [ ] Write store tests:
  - [ ] Use testcontainers for PostgreSQL
  - [ ] Test CRUD operations
  - [ ] Test tenant isolation
  - [ ] Test unique constraints

### 1.6 Token & Authentication

- [ ] Implement token generation (`pkg/crypto/token.go`):
  ```go
  func GenerateToken(prefix string) (token, displayPrefix, hash string, version int, err error)
  func VerifyToken(token, storedHash string, version int, hmacKey []byte) bool
  ```
- [ ] Token prefixes:
  - [ ] `mk_` - API keys
  - [ ] `rtok_` - Runner tokens
  - [ ] `ttok_` - Tunnel tokens
- [ ] SHA-256 hashing with hash_version support:
  - [ ] Version 1: SHA-256 (current)
  - [ ] Version 2: HMAC-SHA256 (reserved)
- [ ] Implement API key service (`pkg/auth/apikey.go`):
  - [ ] `Create(name string, scopes []string) (*APIKey, plainToken string, error)`
  - [ ] `Validate(token string) (*APIKeyInfo, error)`
  - [ ] `Revoke(id string, reason string) error`
  - [ ] `List(opts ListOptions) ([]*APIKey, error)`
- [ ] Implement runner token service (`pkg/auth/runnertoken.go`):
  - [ ] `Create(poolName string) (*RunnerToken, plainToken string, error)`
  - [ ] `Validate(token string) (*RunnerTokenInfo, error)`
  - [ ] `Rotate(id string) (newToken string, error)` - with grace period
  - [ ] `Revoke(id string, reason string) error`
- [ ] Write auth tests:
  - [ ] Token generation uniqueness
  - [ ] Hash verification
  - [ ] Token rotation grace period

### 1.7 Encryption

- [ ] Implement envelope encryption (`pkg/crypto/envelope.go`):
  - [ ] KEK loading from environment (`MARIONETTE_ENCRYPTION_KEY`)
  - [ ] DEK generation per resource
  - [ ] `Encrypt(tenantID string, plaintext []byte) (ciphertext []byte, error)`
  - [ ] `Decrypt(tenantID string, ciphertext []byte) (plaintext []byte, error)`
- [ ] AES-256-GCM implementation:
  - [ ] Nonce generation (12 bytes random)
  - [ ] Authenticated encryption
  - [ ] Ciphertext format: `nonce || ciphertext || tag`
- [ ] DEK management:
  - [ ] Store encrypted DEKs in `data_keys` table
  - [ ] DEK rotation support
  - [ ] Per-tenant DEK isolation
- [ ] Write crypto tests:
  - [ ] Encryption roundtrip
  - [ ] Different tenants have different DEKs
  - [ ] Tampering detection

### 1.8 Docker Provider (Basic)

- [ ] Define provider interface (`pkg/provider/provider.go`):
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
- [ ] Implement Docker provider (`pkg/provider/docker/`):
  - [ ] `docker.go` - provider implementation
  - [ ] Docker client setup (docker.sock or TCP)
  - [ ] Container creation with labels
  - [ ] Volume mounts for workspace
  - [ ] Network configuration (bridge)
- [ ] Spawn options:
  - [ ] Image name (default: `marionette/agent:latest`)
  - [ ] Environment variables
  - [ ] Resource limits (memory, CPU)
  - [ ] Labels and annotations
- [ ] Container lifecycle:
  - [ ] `Spawn()` - create and start container
  - [ ] `Destroy()` - stop and remove container
  - [ ] `Status()` - inspect container state
- [ ] Provider registry:
  - [ ] Register providers by name
  - [ ] Default provider configuration
  - [ ] Provider config from database
- [ ] Write provider tests:
  - [ ] Use testcontainers or mock Docker client
  - [ ] Test spawn/destroy lifecycle
  - [ ] Test status reporting

---

## Phase 2: Core Runtime

### 2.1 gRPC Server

- [ ] Set up gRPC server (`pkg/server/grpc/server.go`):
  - [ ] TLS configuration (required)
  - [ ] Port 9090 for agent connections
  - [ ] Interceptors for logging, recovery
- [ ] Implement RunnerService:
  - [ ] `RegisterRunner(RegisterRunnerRequest) returns (RegisterRunnerResponse)`
  - [ ] `Connect(stream RunnerMessage) returns (stream ServerCommand)`
  - [ ] `StreamLogs(stream StreamLogsMessage) returns (StreamLogsResponse)`
  - [ ] `GetRunnerStatus(GetRunnerStatusRequest) returns (RunnerStatus)`
- [ ] TLS setup:
  - [ ] Load server certificate and key
  - [ ] CA certificate for client verification (optional in dev)
  - [ ] Certificate generation script for development
- [ ] Connection management:
  - [ ] Track connected runners in memory
  - [ ] Handle connection/disconnection events
  - [ ] Update runner status in database

### 2.2 Marionette Agent (Basic)

- [ ] Create agent binary (`cmd/agent/main.go`):
  - [ ] Config loading (server URL, token, labels)
  - [ ] Graceful shutdown handling
- [ ] Implement gRPC client (`pkg/agent/client.go`):
  - [ ] TLS connection to server
  - [ ] Runner token in metadata
  - [ ] Automatic reconnection with backoff
- [ ] Registration flow:
  - [ ] Call `RegisterRunner` on startup
  - [ ] Send runner capabilities and labels
  - [ ] Receive runner ID from server
- [ ] Heartbeat loop:
  - [ ] Send `Heartbeat` messages periodically (30s)
  - [ ] Include status, uptime, resource usage
  - [ ] Detect server disconnection
- [ ] Sandbox mode configuration:
  - [ ] `runner-is-sandbox` - agent runs in isolated container
  - [ ] `runner-creates-sandbox` - agent creates sandbox per task
  - [ ] Detect available sandbox types

### 2.3 Control Channel

- [ ] Implement bidirectional streaming:
  - [ ] Server: send `ServerCommand` messages
  - [ ] Agent: send `RunnerMessage` responses
  - [ ] Keep-alive pings
- [ ] Message routing:
  - [ ] Route commands to specific runner by ID
  - [ ] Queue commands if runner temporarily disconnected
  - [ ] Timeout for pending commands
- [ ] Connection state management:
  - [ ] Track runner state: offline, idle, busy
  - [ ] Update database on state changes
  - [ ] Emit events for state transitions
- [ ] Error handling:
  - [ ] Graceful handling of stream errors
  - [ ] Reconnection without losing state
  - [ ] Dead letter queue for failed deliveries

### 2.4 Runner Lifecycle

- [ ] Runner registration:
  - [ ] Validate runner token
  - [ ] Create or update runner record
  - [ ] Assign runner ID (if new)
  - [ ] Set initial status to `idle`
- [ ] Runner status transitions:
  ```
  offline → idle (on connect)
  idle → busy (on task assignment)
  busy → idle (on task completion)
  idle/busy → offline (on disconnect)
  ```
- [ ] Heartbeat handling:
  - [ ] Update `last_seen_at` timestamp
  - [ ] Update resource usage metrics
  - [ ] Detect stale runners (no heartbeat for 3 intervals)
- [ ] Runner disconnection:
  - [ ] Mark runner as offline
  - [ ] Handle in-flight tasks (mark as failed or retry)
  - [ ] Detach from sessions

### 2.5 Workspace (Basic)

- [ ] Workspace creation:
  - [ ] Generate workspace ID
  - [ ] Create workspace record in database
  - [ ] Create workspace directory on runner
- [ ] Local volume storage:
  - [ ] Mount host directory to container
  - [ ] Workspace path: `/workspace`
  - [ ] Persist across container restarts
- [ ] Workspace lifecycle:
  - [ ] Create on session creation
  - [ ] Persist while session exists
  - [ ] Delete on session termination (configurable)
- [ ] Basic workspace operations:
  - [ ] List files (for debugging)
  - [ ] Get workspace size
  - [ ] Check workspace exists

---

## Phase 3: Session & Task

### 3.1 Session State Machine

- [ ] Session states:
  ```
  pending → active → suspended → terminated
                  ↘ resuming ↗
  ```
- [ ] State transitions:
  - [ ] `pending → active`: Runner assigned
  - [ ] `active → suspended`: Idle timeout or permission timeout
  - [ ] `suspended → resuming`: User resumes or responds to permission
  - [ ] `resuming → active`: Runner attached, context restored
  - [ ] `active → terminated`: User terminates or max lifetime reached
- [ ] Session creation:
  - [ ] Create workspace
  - [ ] Create session record (status: pending)
  - [ ] Request runner from provider
  - [ ] Attach runner when available
- [ ] Session service (`pkg/server/core/session.go`):
  - [ ] `Create(opts CreateSessionOptions) (*Session, error)`
  - [ ] `Get(id string) (*Session, error)`
  - [ ] `List(opts ListOptions) ([]*Session, error)`
  - [ ] `Suspend(id string, reason string) error`
  - [ ] `Resume(id string) error`
  - [ ] `Terminate(id string) error`

### 3.2 Runner-Session Binding

- [ ] Attach runner to session:
  - [ ] Send `AttachSession` command to runner
  - [ ] Include workspace path
  - [ ] Include agent config (API key, model)
  - [ ] Include pending permission responses (if resuming)
- [ ] Detach runner from session:
  - [ ] Send `DetachSession` command
  - [ ] Save context snapshot
  - [ ] Sync workspace (if configured)
  - [ ] Update session state
- [ ] Runner assignment:
  - [ ] Find idle runner matching requirements
  - [ ] Label-based matching (profile selector)
  - [ ] Reserve runner (set status to busy)
  - [ ] Handle no available runners (queue or error)

### 3.3 Task Management

- [ ] Task creation:
  - [ ] Validate session is active
  - [ ] Create task record (status: pending)
  - [ ] Create first task_run (attempt: 1)
- [ ] Task service (`pkg/server/core/task.go`):
  - [ ] `Create(sessionID, prompt string, opts TaskOptions) (*Task, error)`
  - [ ] `Get(id string) (*Task, error)`
  - [ ] `List(sessionID string, opts ListOptions) ([]*Task, error)`
  - [ ] `Cancel(id string) error`
  - [ ] `Retry(id string) error`
- [ ] Task status aggregation:
  - [ ] Get latest task_run status
  - [ ] Update task status from run status
  - [ ] Handle terminal states

### 3.4 Task Run Management

- [ ] Run state machine:
  ```
  pending → assigned → running → completed/failed/timeout/canceled
  ```
- [ ] Run lifecycle:
  - [ ] `pending`: Waiting for runner
  - [ ] `assigned`: Runner selected, command sent
  - [ ] `running`: Runner acknowledged start
  - [ ] Terminal: completed, failed, timeout, canceled
- [ ] Task execution flow:
  - [ ] Send `ExecuteTask` command to runner
  - [ ] Receive `TaskAccepted` ack
  - [ ] Receive `TaskStarted` ack
  - [ ] Receive `TaskProgress` updates
  - [ ] Receive `TaskCompleted` with result
- [ ] Timeout enforcement:
  - [ ] Per-task timeout (default: 1 hour)
  - [ ] Start timer on `assigned`
  - [ ] Send `KillTask` on timeout
  - [ ] Mark run as `timeout`
- [ ] Retry logic:
  - [ ] Check `max_retries` and `retry_count`
  - [ ] Create new task_run with incremented attempt
  - [ ] Exponential backoff between retries

### 3.5 Agent Execution

- [ ] Agent interface (`pkg/agent/executor/executor.go`):
  ```go
  type Executor interface {
      Execute(ctx, task *Task, config *AgentConfig) (*Result, error)
      Kill() error
  }
  ```
- [ ] Claude Code executor (`pkg/agent/executor/claude/`):
  - [ ] Spawn Claude Code process via PTY
  - [ ] Pass API key as environment variable
  - [ ] Send prompt to stdin
  - [ ] Stream stdout/stderr
  - [ ] Handle permission requests
  - [ ] Capture exit code
- [ ] Environment setup:
  - [ ] Set `ANTHROPIC_API_KEY`
  - [ ] Set working directory to workspace
  - [ ] Set any extra environment from config
- [ ] Process management:
  - [ ] PTY-based process spawning
  - [ ] Signal handling (SIGTERM, SIGKILL)
  - [ ] Graceful termination
  - [ ] Resource cleanup

### 3.6 Log Streaming

- [ ] Log stream implementation:
  - [ ] Agent captures stdout/stderr from executor
  - [ ] Wrap in `LogEntry` protobuf messages
  - [ ] Send via `StreamLogs` RPC
- [ ] Log entry structure:
  - [ ] task_id, run_id, session_id
  - [ ] stream (stdout, stderr, system)
  - [ ] level (debug, info, warn, error)
  - [ ] content, sequence number, timestamp
- [ ] Server-side handling:
  - [ ] Validate `StreamLogsInit` message
  - [ ] Verify runner authorization
  - [ ] Persist logs to database (never drop)
  - [ ] Forward to real-time subscribers (can drop under pressure)
- [ ] Batching:
  - [ ] Buffer logs on agent side (100ms or 100 entries)
  - [ ] Send batches to reduce RPC overhead
  - [ ] Flush on task completion
- [ ] Sequence tracking:
  - [ ] Monotonic sequence per run
  - [ ] Detect gaps for debugging
  - [ ] Re-order if needed on server

### 3.7 Permission Handling

- [ ] Permission request detection:
  - [ ] Parse Claude Code output for permission patterns
  - [ ] Extract tool name, action, context
  - [ ] Determine risk level
- [ ] Permission request flow:
  - [ ] Agent sends `PermissionRequest` message
  - [ ] Server creates permission_request record
  - [ ] Server notifies subscribers (WebSocket, webhook)
  - [ ] Agent blocks waiting for response
- [ ] Permission response flow:
  - [ ] User approves/denies via API or WebUI
  - [ ] Server sends `ApprovePermission` command
  - [ ] Agent receives and unblocks executor
  - [ ] Executor continues or handles denial
- [ ] Timeout handling:
  - [ ] `suspend_after_seconds`: auto-suspend session (default: 30 min)
  - [ ] Permission stays pending (no auto-deny)
  - [ ] On resume: deliver cached response to agent
- [ ] Permission caching:
  - [ ] Store response in database
  - [ ] Include `responded_by`, `response_reason`
  - [ ] Deliver on session resume via `pending_permissions`

---

## Phase 4: HTTP API & CLI

### 4.1 HTTP Server Setup

- [ ] Set up HTTP servers:
  - [ ] Port 8080: Public API (API key auth)
  - [ ] Port 8081: Admin API (Basic Auth)
- [ ] Middleware stack:
  - [ ] Request ID injection
  - [ ] Structured logging
  - [ ] Panic recovery
  - [ ] CORS (configurable)
  - [ ] Rate limiting (optional)
- [ ] Authentication middleware:
  - [ ] Extract `Authorization: Bearer <token>` header
  - [ ] Validate API key
  - [ ] Inject tenant_id into context
  - [ ] Scope checking

### 4.2 Public API Endpoints

- [ ] Sessions API (`/api/v1/sessions`):
  - [ ] `POST /` - Create session
  - [ ] `GET /` - List sessions
  - [ ] `GET /:id` - Get session
  - [ ] `POST /:id/suspend` - Suspend session
  - [ ] `POST /:id/resume` - Resume session
  - [ ] `DELETE /:id` - Terminate session
- [ ] Tasks API (`/api/v1/tasks`):
  - [ ] `POST /` - Create task (with session_id)
  - [ ] `GET /` - List tasks (filter by session)
  - [ ] `GET /:id` - Get task
  - [ ] `POST /:id/cancel` - Cancel task
  - [ ] `POST /:id/retry` - Retry task
- [ ] Runners API (`/api/v1/runners`):
  - [ ] `GET /` - List runners
  - [ ] `GET /:id` - Get runner
- [ ] Permissions API (`/api/v1/permissions`):
  - [ ] `GET /` - List pending permissions
  - [ ] `GET /:id` - Get permission details
  - [ ] `POST /:id/approve` - Approve permission
  - [ ] `POST /:id/deny` - Deny permission
- [ ] Logs API (`/api/v1/logs`):
  - [ ] `GET /:task_id` - Get task logs
  - [ ] `GET /:task_id/stream` - WebSocket log stream

### 4.3 Admin API Endpoints

- [ ] API Keys (`/admin/api/v1/keys`):
  - [ ] `POST /` - Create API key
  - [ ] `GET /` - List API keys
  - [ ] `DELETE /:id` - Revoke API key
- [ ] Agent Configs (`/admin/api/v1/agent-configs`):
  - [ ] `POST /` - Create agent config
  - [ ] `GET /` - List agent configs
  - [ ] `GET /:id` - Get agent config
  - [ ] `PUT /:id` - Update agent config
  - [ ] `DELETE /:id` - Delete agent config
- [ ] Provider Configs (`/admin/api/v1/provider-configs`):
  - [ ] `POST /` - Create provider config
  - [ ] `GET /` - List provider configs
  - [ ] `PUT /:id` - Update provider config
  - [ ] `DELETE /:id` - Delete provider config
- [ ] Runners (`/admin/api/v1/runners`):
  - [ ] `POST /spawn` - Spawn new runner
  - [ ] `DELETE /:id` - Destroy runner

### 4.4 WebSocket Support

- [ ] Log streaming WebSocket:
  - [ ] Endpoint: `/api/v1/logs/:task_id/stream`
  - [ ] Authentication via query param or header
  - [ ] Real-time log delivery
  - [ ] Backpressure handling (drop if slow)
- [ ] Event streaming WebSocket:
  - [ ] Endpoint: `/api/v1/events`
  - [ ] Subscribe to event types
  - [ ] Filter by labels/selectors
  - [ ] Session state changes, task updates, permission requests

### 4.5 OpenAPI Documentation

- [ ] Generate OpenAPI spec:
  - [ ] Use swaggo or manual spec
  - [ ] Document all endpoints
  - [ ] Include request/response schemas
  - [ ] Include authentication requirements
- [ ] Serve documentation:
  - [ ] Swagger UI at `/docs`
  - [ ] OpenAPI JSON at `/openapi.json`

### 4.6 CLI (mctl)

- [ ] CLI framework setup (`cmd/mctl/`):
  - [ ] Use cobra for commands
  - [ ] Config file support (`~/.marionette/config.yaml`)
  - [ ] Environment variable support
- [ ] Config commands:
  - [ ] `mctl config set-context <name> --api-url <url> --api-key <key>`
  - [ ] `mctl config use-context <name>`
  - [ ] `mctl config get-contexts`
  - [ ] `mctl config current-context`
- [ ] Session commands:
  - [ ] `mctl sessions create --agent claude --name <name>`
  - [ ] `mctl sessions list`
  - [ ] `mctl sessions get <id>`
  - [ ] `mctl sessions attach <id>` - interactive mode
  - [ ] `mctl sessions suspend <id>`
  - [ ] `mctl sessions resume <id>`
  - [ ] `mctl sessions terminate <id>`
- [ ] Task commands:
  - [ ] `mctl tasks create --session <id> --prompt "<prompt>"`
  - [ ] `mctl tasks create --continue <task_id> --prompt "<prompt>"`
  - [ ] `mctl tasks list --session <id>`
  - [ ] `mctl tasks get <id>`
  - [ ] `mctl tasks logs <id> [--follow]`
  - [ ] `mctl tasks cancel <id>`
  - [ ] `mctl tasks retry <id>`
- [ ] Runner commands:
  - [ ] `mctl runners list`
  - [ ] `mctl runners get <id>`
- [ ] Permission commands:
  - [ ] `mctl permissions list [--pending]`
  - [ ] `mctl permissions approve <id> [--reason <reason>]`
  - [ ] `mctl permissions deny <id> [--reason <reason>]`
- [ ] Admin commands:
  - [ ] `mctl admin keys create --name <name> --scopes <scopes>`
  - [ ] `mctl admin keys list`
  - [ ] `mctl admin keys revoke <id>`
  - [ ] `mctl admin agent-configs create --agent claude --api-key <key>`
  - [ ] `mctl admin agent-configs list`
  - [ ] `mctl admin runners spawn --provider <provider>`
  - [ ] `mctl admin runners destroy <id>`
- [ ] Output formatting:
  - [ ] Table format (default)
  - [ ] JSON format (`--output json`)
  - [ ] YAML format (`--output yaml`)

---

## Phase 5: Suspend/Resume

### 5.1 Suspend Strategy Framework

- [ ] Define suspend strategies:
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
- [ ] SuspendableProvider interface:
  ```go
  type SuspendableProvider interface {
      Provider
      Suspend(ctx, runnerID string, opts SuspendOptions) (*SuspendResult, error)
      Resume(ctx, sessionID string, opts ResumeOptions) (*RunnerInstance, error)
  }
  ```
- [ ] Suspend configuration per provider:
  - [ ] Default strategy
  - [ ] Fallback strategy
  - [ ] Min/max suspend duration
  - [ ] Workspace sync options

### 5.2 Docker Pause/Resume

- [ ] Implement pause strategy for Docker:
  - [ ] `docker pause <container>` on suspend
  - [ ] `docker unpause <container>` on resume
  - [ ] Memory preserved, instant resume
- [ ] Fallback to terminate_preserve_storage:
  - [ ] Stop container but keep volume
  - [ ] Create new container with same volume on resume

### 5.3 Context Snapshot

- [ ] Define context snapshot schema:
  ```go
  type ContextSnapshot struct {
      WorkingDirectory string
      Environment      map[string]string
      ConversationID   string  // For agents that support it
      AgentState       json.RawMessage
      LastActivity     time.Time
  }
  ```
- [ ] Save context on suspend:
  - [ ] Request context from agent via gRPC
  - [ ] Store in session.context_snapshot (JSONB)
  - [ ] Store agent_version for compatibility check
- [ ] Restore context on resume:
  - [ ] Check agent version compatibility
  - [ ] Send context in AttachSession command
  - [ ] Agent restores working directory, environment

### 5.4 CAS Storage Implementation

- [ ] Implement content-defined chunking (`pkg/storage/cas/`):
  - [ ] Use restic chunker (Rabin fingerprinting)
  - [ ] Chunk size: 512KB min, 1MB target, 8MB max
  - [ ] SHA-256 hash per chunk
- [ ] Chunk storage:
  - [ ] Local storage provider (development)
  - [ ] S3 storage provider (production)
  - [ ] GCS storage provider (production)
  - [ ] Tenant-scoped paths: `chunks/{tenant_id}/{hash[:2]}/{hash}.blob.enc`
- [ ] Chunk encryption:
  - [ ] Per-tenant DEK from envelope encryption
  - [ ] zstd compression before encryption
  - [ ] AES-256-GCM encryption
- [ ] Manifest management:
  - [ ] JSONL format for streaming
  - [ ] Header line with metadata
  - [ ] One file entry per line
  - [ ] Store in `manifests/{tenant_id}/{workspace_id}/{manifest_id}.jsonl.zst.enc`
- [ ] Single chunk mode:
  - [ ] For workspaces < 100MB
  - [ ] Create tar.zst archive
  - [ ] Store as single encrypted chunk

### 5.5 Workspace Sync

- [ ] Sync service (`pkg/storage/cas/sync.go`):
  - [ ] `Sync(ctx, workspaceID, tenantID, srcDir string) error`
  - [ ] `Restore(ctx, workspaceID, tenantID, dstDir string) error`
  - [ ] `ValidateManifest(ctx, manifest *Manifest) error`
- [ ] Incremental sync:
  - [ ] Get previous manifest chunk hashes
  - [ ] Only upload new/modified chunks
  - [ ] Update chunk ref_count
- [ ] Memory-efficient implementation:
  - [ ] Stream chunks to temp files (not memory)
  - [ ] Parallel upload with bounded concurrency (10)
  - [ ] Streaming manifest save/load

### 5.6 Garbage Collection

- [ ] Mark-and-sweep GC (`pkg/jobs/chunk_gc.go`):
  - [ ] Phase 1: Mark orphaned chunks (ref_count = 0)
  - [ ] Set deleted_at timestamp (soft delete)
  - [ ] Phase 2: Sweep after grace period (7 days)
  - [ ] Re-verify ref_count before physical delete
- [ ] Chunk resurrection:
  - [ ] Clear deleted_at if chunk referenced during grace period
  - [ ] Atomic ref_count update
- [ ] GC job scheduling:
  - [ ] Run daily at low-traffic time
  - [ ] Configurable schedule
  - [ ] Metrics for deleted chunks

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

### 6.1 mTLS Implementation

- [ ] Upgrade from TLS to mTLS:
  - [ ] Server presents certificate
  - [ ] Server verifies client certificate
  - [ ] Client presents certificate (marionette-agent)
- [ ] Certificate management:
  - [ ] CA certificate generation script
  - [ ] Server certificate generation
  - [ ] Agent certificate generation
  - [ ] Certificate signing workflow
- [ ] Certificate configuration:
  ```yaml
  tls:
    ca_cert: /etc/marionette/ca.crt
    server_cert: /etc/marionette/server.crt
    server_key: /etc/marionette/server.key
    client_cert: /etc/marionette/client.crt  # Agent
    client_key: /etc/marionette/client.key
    verify_client: true  # Require client cert
  ```
- [ ] Certificate rotation:
  - [ ] Reload certificates without restart
  - [ ] File watcher for cert changes
  - [ ] Graceful connection migration

### 6.2 Sandbox Verification

- [ ] Sandbox escape testing:
  - [ ] Verify container isolation
  - [ ] Test filesystem access restrictions
  - [ ] Test network isolation
  - [ ] Test process isolation
- [ ] Resource limit enforcement:
  - [ ] Memory limits (OOM handling)
  - [ ] CPU limits (throttling)
  - [ ] Disk quota enforcement
  - [ ] Process count limits
- [ ] Sandbox type detection:
  - [ ] Detect available sandbox types on runner
  - [ ] Validate requested sandbox type is available
  - [ ] Report capabilities to server

### 6.3 Audit Logging

- [ ] Implement action_logs table operations:
  - [ ] `CreateActionLog(ctx, log *ActionLog) error`
  - [ ] `ListActionLogs(ctx, opts ActionLogListOptions) ([]*ActionLog, error)`
- [ ] Log sensitive actions:
  - [ ] Permission approved/denied
  - [ ] Session created/terminated
  - [ ] API key created/revoked
  - [ ] Agent config created/updated/deleted
  - [ ] Runner spawned/destroyed
  - [ ] Login attempts (success/failure)
- [ ] Action log content:
  - [ ] Actor (api_key, system, runner)
  - [ ] Action type
  - [ ] Resource type and ID
  - [ ] Details (JSON)
  - [ ] IP address, user agent
  - [ ] Success/failure
- [ ] Admin API for audit logs:
  - [ ] `GET /admin/api/v1/action-logs`
  - [ ] Filter by actor, action, resource, time range
  - [ ] Pagination support

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

### 7.1 Project Setup

- [ ] Initialize React project:
  - [ ] Vite + React + TypeScript
  - [ ] TanStack Router for routing
  - [ ] TanStack Query for data fetching
  - [ ] Tailwind CSS for styling
- [ ] Directory structure:
  ```
  web/
  ├── src/
  │   ├── components/
  │   ├── pages/
  │   ├── hooks/
  │   ├── api/
  │   └── utils/
  ├── package.json
  └── vite.config.ts
  ```
- [ ] API client setup:
  - [ ] Axios or fetch wrapper
  - [ ] Authentication handling
  - [ ] Error handling
  - [ ] TypeScript types from OpenAPI

### 7.2 Admin UI

- [ ] Login page:
  - [ ] Basic Auth login form
  - [ ] Session management (cookie)
  - [ ] Redirect to dashboard on success
- [ ] API Key management:
  - [ ] List API keys (masked)
  - [ ] Create new key (show once)
  - [ ] Revoke key
  - [ ] Show last used time
- [ ] Agent Config management:
  - [ ] List agent configs
  - [ ] Create/edit form
  - [ ] API key input (masked)
  - [ ] Test connection button
- [ ] Provider Config management:
  - [ ] List provider configs
  - [ ] Create/edit form
  - [ ] JSON editor for config

### 7.3 Dashboard

- [ ] Overview page:
  - [ ] Active sessions count
  - [ ] Running tasks count
  - [ ] Online runners count
  - [ ] Recent activity feed
- [ ] Sessions page:
  - [ ] Session list with status
  - [ ] Create session form
  - [ ] Session detail view
  - [ ] Suspend/resume/terminate buttons
- [ ] Task execution page:
  - [ ] Prompt input
  - [ ] Real-time log viewer (WebSocket)
  - [ ] Task status display
  - [ ] Cancel button
- [ ] Permission approval:
  - [ ] Pending permissions list
  - [ ] Permission detail (tool, action, context)
  - [ ] Approve/deny buttons
  - [ ] Timeout countdown

### 7.4 Real-time Log Viewer

- [ ] WebSocket connection:
  - [ ] Connect to log stream endpoint
  - [ ] Reconnection on disconnect
  - [ ] Connection status indicator
- [ ] Log display:
  - [ ] ANSI color support
  - [ ] Auto-scroll to bottom
  - [ ] Pause auto-scroll on scroll up
  - [ ] Search/filter logs
- [ ] Log buffering:
  - [ ] Virtual scrolling for large logs
  - [ ] Limit displayed lines (configurable)
  - [ ] Download full logs button

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

### 8.1 Docker & Deployment

- [ ] Server Dockerfile:
  - [ ] Multi-stage build
  - [ ] Minimal runtime image (distroless/alpine)
  - [ ] Non-root user
  - [ ] Health check
- [ ] Agent Dockerfile:
  - [ ] Include Claude Code CLI
  - [ ] Include common tools (git, etc.)
  - [ ] Non-root user
- [ ] docker-compose.yaml:
  - [ ] Server + PostgreSQL + Agent
  - [ ] Volume mounts for persistence
  - [ ] Network configuration
  - [ ] Environment variables
- [ ] Kubernetes manifests:
  - [ ] Deployment for server
  - [ ] StatefulSet for runners (optional)
  - [ ] Service and Ingress
  - [ ] ConfigMap and Secrets
  - [ ] PersistentVolumeClaim for workspaces
  - [ ] NetworkPolicy

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
