-- Marionette Database Schema
-- PostgreSQL 15+
--
-- ID Format: {prefix}_{base62_timestamp}{nanoid}
-- Examples: sess_0002xK9mNpV1StGXR8, task_0002xK9mNqW2TuHYS9
--
-- Structure:
--   - prefix: resource type (sess, task, run, ws, etc.)
--   - base62_timestamp: zero-padded milliseconds since epoch (8 chars fixed)
--   - nanoid: 8 random chars (base62 alphabet)
--
-- Benefits:
--   - Human-readable (type visible in ID)
--   - Time-ordered (fixed-width base62 ensures lexicographic = chronological)
--   - Short (~21 chars vs UUID's 36)
--   - URL-safe (no special chars)
--
-- Prefixes:
--   run_    runner
--   sess_   session
--   conv_   conversation
--   task_   task
--   trun_   task_run
--   stsk_   scheduled_task
--   perm_   permission_request
--   ws_     workspace
--   key_    api_key
--   rtok_   runner_token
--   dek_    data_key
--   rlog_   raw_log (original agent output)
--   evt_    agent_event (parsed/structured event)
--   arch_   log_archive
--   acfg_   agent_config
--   pcfg_   provider_config
--   prof_   profile
--   snap_   snapshot
--   tun_    tunnel
--   ttok_   tunnel_token
--   mfst_   manifest
--   alog_   action_log

--------------------------------------------------------------------------------
-- Tenant Isolation
--------------------------------------------------------------------------------
--
-- DEPLOYMENT MODES:
--   1. Single-tenant: tenant_id = NULL everywhere (default for self-hosted)
--   2. Multi-tenant:  tenant_id NOT NULL enforced at application layer
--
-- SECURITY MODEL:
--   - tenant_id is injected by auth middleware, NEVER from user input
--   - All queries filtered by tenant_id (middleware-injected WHERE clause)
--   - Cross-tenant references prevented at application layer (store package)
--   - For multi-tenant SaaS: set config.multi_tenant = true
--
-- APPLICATION LAYER ENFORCEMENT (see pkg/store/tenant.go):
--   1. TenantContext: wraps DB operations with tenant_id injection
--   2. CreateSession: validates workspace.tenant_id == session.tenant_id
--   3. CreateTask: validates session.tenant_id == task.tenant_id
--   4. AttachRunner: validates runner.tenant_id == session.tenant_id
--   5. All List/Get queries: add WHERE tenant_id = ? filter
--
-- NULL HANDLING:
--   - PostgreSQL: NULL = NULL is false, so UNIQUE(tenant_id, name) allows
--     multiple rows with NULL tenant_id and same name.
--   - Solution: Use COALESCE(tenant_id, '') in unique indexes.
--   - This treats NULL as empty string for uniqueness, but keeps NULL in data.
--
-- WHY NOT DATABASE TRIGGERS:
--   - Portability: support MySQL, SQLite, CockroachDB in the future
--   - Debuggability: easier to trace validation errors in application code
--   - Performance: avoid trigger overhead on every write
--   - Flexibility: different validation rules for different deployment modes

--------------------------------------------------------------------------------
-- Configuration Tables (created first to avoid circular references)
--------------------------------------------------------------------------------

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,  -- pcfg_xxx
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',

    -- Suspend configuration
    -- See docs/providers.md for strategy details
    suspend_config JSONB NOT NULL DEFAULT '{
        "strategy": "terminate",
        "min_duration": "60s",
        "max_duration": "24h",
        "fallback": "terminate"
    }',

    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Note: Use index with COALESCE instead of constraint for NULL handling
    CONSTRAINT valid_suspend_strategy CHECK (
        suspend_config->>'strategy' IS NULL OR
        suspend_config->>'strategy' IN (
            'pause', 'snapshot', 'terminate_preserve_storage',
            'release_to_pool', 'terminate'
        )
    )
);

CREATE INDEX idx_provider_configs_provider ON provider_configs(provider);
CREATE INDEX idx_provider_configs_tenant ON provider_configs(tenant_id);
-- COALESCE handles NULL: treats NULL tenant_id as '' for uniqueness
CREATE UNIQUE INDEX idx_provider_configs_name_unique ON provider_configs(COALESCE(tenant_id, ''), name);
CREATE UNIQUE INDEX idx_provider_configs_default ON provider_configs(provider, COALESCE(tenant_id, ''))
    WHERE is_default = TRUE;

CREATE TABLE agent_configs (
    id TEXT PRIMARY KEY,  -- acfg_xxx
    name TEXT NOT NULL,
    agent TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    model TEXT,
    base_url TEXT,
    extra JSONB NOT NULL DEFAULT '{}',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Note: Use index with COALESCE instead of constraint for NULL handling
);

CREATE INDEX idx_agent_configs_agent ON agent_configs(agent);
CREATE INDEX idx_agent_configs_tenant ON agent_configs(tenant_id);
-- COALESCE handles NULL: treats NULL tenant_id as '' for uniqueness
CREATE UNIQUE INDEX idx_agent_configs_name_unique ON agent_configs(COALESCE(tenant_id, ''), name);
CREATE UNIQUE INDEX idx_agent_configs_default ON agent_configs(agent, COALESCE(tenant_id, ''))
    WHERE is_default = TRUE;

CREATE TABLE profiles (
    id TEXT PRIMARY KEY,  -- prof_xxx
    name TEXT NOT NULL,
    description TEXT,
    provider_config_id TEXT REFERENCES provider_configs(id) ON DELETE SET NULL,
    tenant_id TEXT,
    resources JSONB NOT NULL DEFAULT '{}',
    network JSONB NOT NULL DEFAULT '{}',
    init_script TEXT,
    cleanup_script TEXT,
    tunnels JSONB NOT NULL DEFAULT '[]',
    selector JSONB NOT NULL DEFAULT '{}',
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    is_builtin BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Note: Use index with COALESCE instead of constraint for NULL handling
);

CREATE INDEX idx_profiles_tenant ON profiles(tenant_id);
-- COALESCE handles NULL: treats NULL tenant_id as '' for uniqueness
CREATE UNIQUE INDEX idx_profiles_name_unique ON profiles(COALESCE(tenant_id, ''), name);

--------------------------------------------------------------------------------
-- Core Tables
--------------------------------------------------------------------------------

-- runners (execution environments)
CREATE TABLE runners (
    id TEXT PRIMARY KEY,  -- run_xxx
    name TEXT NOT NULL,
    hostname TEXT NOT NULL,

    -- Connection state (server-tracked): offline, idle, busy, paused
    status TEXT NOT NULL DEFAULT 'offline',

    -- Taint status for pool runners (dirty state after crash)
    tainted BOOLEAN NOT NULL DEFAULT FALSE,
    taint_reason TEXT,

    -- Sandbox configuration
    sandbox_mode TEXT NOT NULL DEFAULT 'runner-is-sandbox',
    sandbox_types TEXT[] DEFAULT '{}',

    -- Provider info
    provider_config_id TEXT REFERENCES provider_configs(id) ON DELETE SET NULL,
    provider_instance_id TEXT,

    -- Pool info
    pool_name TEXT,
    profile_id TEXT REFERENCES profiles(id) ON DELETE SET NULL,
    capabilities TEXT[] NOT NULL DEFAULT '{}',

    -- Tenant isolation
    tenant_id TEXT,

    -- Metadata
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_sandbox_mode CHECK (
        sandbox_mode IN ('runner-is-sandbox', 'runner-creates-sandbox', 'none')
    ),
    CONSTRAINT valid_runner_status CHECK (
        status IN ('offline', 'idle', 'busy', 'paused')
    )
    -- Note: Use index with COALESCE instead of constraint for NULL handling
);

CREATE INDEX idx_runners_labels ON runners USING GIN (labels);
CREATE INDEX idx_runners_pool ON runners(pool_name) WHERE pool_name IS NOT NULL;
CREATE INDEX idx_runners_status ON runners(status);
CREATE INDEX idx_runners_tenant ON runners(tenant_id);
CREATE INDEX idx_runners_tainted ON runners(pool_name, tainted) WHERE tainted = TRUE;
-- COALESCE handles NULL: treats NULL tenant_id as '' for uniqueness
CREATE UNIQUE INDEX idx_runners_name_unique ON runners(COALESCE(tenant_id, ''), name);

-- workspaces: persistent filesystem
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,  -- ws_xxx
    name TEXT NOT NULL,

    persist BOOLEAN NOT NULL DEFAULT TRUE,
    storage_type TEXT NOT NULL DEFAULT 'volume',
    storage_config JSONB NOT NULL DEFAULT '{}',

    mobility TEXT NOT NULL DEFAULT 'local',
    storage_domain TEXT,
    storage_key TEXT,
    storage_size_bytes BIGINT,
    last_synced_at TIMESTAMPTZ,

    disk_quota_mb INT,
    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,

    CONSTRAINT valid_mobility CHECK (mobility IN ('local', 'shared', 'object_sync'))
);

CREATE INDEX idx_workspaces_tenant ON workspaces(tenant_id);
CREATE INDEX idx_workspaces_expires ON workspaces(expires_at) WHERE deleted_at IS NULL;

-- sessions (long-lived work contexts)
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,  -- sess_xxx
    name TEXT,

    status TEXT NOT NULL DEFAULT 'pending',

    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,

    -- Agent configuration
    agent TEXT NOT NULL,
    is_byok BOOLEAN NOT NULL DEFAULT FALSE,
    agent_config_id TEXT REFERENCES agent_configs(id) ON DELETE SET NULL,
    agent_config_metadata JSONB,

    -- Suspend state tracking
    -- Which strategy was used to suspend this session
    suspend_strategy TEXT,
    -- Snapshot ID if snapshot strategy was used
    suspend_snapshot_id TEXT,
    -- Whether workspace was synced to CAS during suspend
    suspend_workspace_synced BOOLEAN,
    -- Previous runner ID (for resume tracking)
    previous_runner_id TEXT,

    -- Network policy
    network_policy TEXT NOT NULL DEFAULT 'allow_list',
    allowed_hosts TEXT[] NOT NULL DEFAULT '{}',

    -- Lifecycle mode
    -- "on_demand": suspend when idle (default, cost-efficient)
    -- "always_on": 7x24 persistent session, never auto-suspend
    -- "scheduled": activated by cron schedule, suspend between runs
    lifecycle_mode TEXT NOT NULL DEFAULT 'on_demand',

    -- Lifecycle settings
    idle_timeout_seconds INT DEFAULT 1800,  -- For on_demand: suspend after idle
    max_lifetime_seconds INT,               -- Max session lifetime (NULL = unlimited)

    -- Schedule settings (for lifecycle_mode = 'scheduled')
    -- Cron expression: "0 9 * * 1-5" (weekdays at 9am)
    schedule_cron TEXT,
    schedule_timezone TEXT DEFAULT 'UTC',
    -- Next scheduled activation time (computed from cron)
    next_scheduled_at TIMESTAMPTZ,

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    last_activity_at TIMESTAMPTZ,
    suspended_at TIMESTAMPTZ,
    resumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_session_status CHECK (
        status IN ('pending', 'active', 'suspended', 'resuming', 'terminated')
    ),
    CONSTRAINT valid_network_policy CHECK (
        network_policy IN ('none', 'allow_list', 'proxy', 'air_gapped')
    ),
    CONSTRAINT valid_suspend_strategy CHECK (
        suspend_strategy IS NULL OR
        suspend_strategy IN (
            'pause', 'snapshot', 'terminate_preserve_storage',
            'release_to_pool', 'terminate'
        )
    ),
    CONSTRAINT valid_lifecycle_mode CHECK (
        lifecycle_mode IN ('on_demand', 'always_on', 'scheduled')
    ),
    CONSTRAINT scheduled_requires_cron CHECK (
        lifecycle_mode != 'scheduled' OR schedule_cron IS NOT NULL
    )
);

CREATE INDEX idx_sessions_status ON sessions(status);
CREATE INDEX idx_sessions_runner ON sessions(runner_id);
CREATE INDEX idx_sessions_tenant ON sessions(tenant_id);
CREATE INDEX idx_sessions_labels ON sessions USING GIN (labels);
CREATE INDEX idx_sessions_suspended ON sessions(tenant_id, suspended_at)
    WHERE status = 'suspended';
CREATE INDEX idx_sessions_scheduled ON sessions(next_scheduled_at)
    WHERE lifecycle_mode = 'scheduled' AND status = 'suspended';
CREATE INDEX idx_sessions_always_on ON sessions(tenant_id)
    WHERE lifecycle_mode = 'always_on';

-- conversations: dialogue contexts within a session
-- A session can have multiple conversations (e.g., parallel work with worktrees)
-- Each conversation maintains its own context and message queue (tasks)
CREATE TABLE conversations (
    id TEXT PRIMARY KEY,  -- conv_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,

    -- Soft fork support (same session, different worktree)
    parent_conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,

    -- Working directory (may be a worktree path)
    working_dir TEXT,

    -- Context snapshot (for suspend/resume)
    -- Contains: conversation_state, tool history, environment, etc.
    context_snapshot JSONB,

    -- Agent version tracking (for compatibility checking on resume)
    -- Format: semantic version string, e.g., "1.0.45"
    agent_version TEXT,

    -- Conversation status
    status TEXT NOT NULL DEFAULT 'active',

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    last_activity_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_conversation_status CHECK (
        status IN ('active', 'completed', 'terminated')
    )
);

CREATE INDEX idx_conversations_session ON conversations(session_id);
CREATE INDEX idx_conversations_parent ON conversations(parent_conversation_id)
    WHERE parent_conversation_id IS NOT NULL;
CREATE INDEX idx_conversations_status ON conversations(status);
CREATE INDEX idx_conversations_tenant ON conversations(tenant_id);
CREATE INDEX idx_conversations_active ON conversations(session_id, status)
    WHERE status = 'active';

-- tasks: message queue entries within a conversation
-- Each task is a user prompt/request that the agent processes
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,  -- task_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,
    prompt TEXT NOT NULL,

    status TEXT NOT NULL DEFAULT 'pending',
    max_retries INT NOT NULL DEFAULT 0,
    retry_count INT NOT NULL DEFAULT 0,
    timeout_seconds INT NOT NULL DEFAULT 3600,

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_task_status CHECK (
        status IN ('pending', 'running', 'completed', 'failed', 'canceled')
    )
);

CREATE INDEX idx_tasks_session ON tasks(session_id);
CREATE INDEX idx_tasks_conversation ON tasks(conversation_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_tenant ON tasks(tenant_id);
CREATE INDEX idx_tasks_pending ON tasks(conversation_id, status) WHERE status = 'pending';

-- task_runs: execution attempts
CREATE TABLE task_runs (
    id TEXT PRIMARY KEY,  -- trun_xxx
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    attempt INT NOT NULL DEFAULT 1,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,

    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT,
    exit_code INT,
    tokens_input INT,
    tokens_output INT,

    -- Tenant isolation (denormalized for query performance)
    tenant_id TEXT,

    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_task_run_status CHECK (
        status IN ('pending', 'assigned', 'running', 'completed', 'failed', 'timeout', 'canceled')
    ),
    UNIQUE(task_id, attempt)
);

CREATE INDEX idx_task_runs_task ON task_runs(task_id);
CREATE INDEX idx_task_runs_runner ON task_runs(runner_id);
CREATE INDEX idx_task_runs_status ON task_runs(status);
CREATE INDEX idx_task_runs_tenant ON task_runs(tenant_id);

-- scheduled_tasks: recurring tasks with cron schedule
-- These create regular tasks when triggered
CREATE TABLE scheduled_tasks (
    id TEXT PRIMARY KEY,  -- stsk_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,

    -- Cron schedule: "0 9 * * 1-5" (weekdays at 9am)
    cron_expression TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',

    -- Task template
    prompt_template TEXT NOT NULL,  -- May contain {{.Date}}, {{.RunNumber}} etc.

    -- Execution settings
    timeout_seconds INT NOT NULL DEFAULT 3600,
    max_retries INT NOT NULL DEFAULT 0,

    -- State
    status TEXT NOT NULL DEFAULT 'active',  -- "active", "paused", "disabled"
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    run_count INT NOT NULL DEFAULT 0,
    failure_count INT NOT NULL DEFAULT 0,

    -- Error handling
    -- "continue": keep running even if last run failed
    -- "pause_on_failure": pause after failure until manually resumed
    -- "disable_on_failure": disable after N consecutive failures
    on_failure TEXT NOT NULL DEFAULT 'continue',
    max_consecutive_failures INT DEFAULT 3,
    consecutive_failures INT NOT NULL DEFAULT 0,

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_scheduled_task_status CHECK (
        status IN ('active', 'paused', 'disabled')
    ),
    CONSTRAINT valid_on_failure CHECK (
        on_failure IN ('continue', 'pause_on_failure', 'disable_on_failure')
    )
);

CREATE INDEX idx_scheduled_tasks_session ON scheduled_tasks(session_id);
CREATE INDEX idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at)
    WHERE status = 'active';
CREATE INDEX idx_scheduled_tasks_tenant ON scheduled_tasks(tenant_id);

-- permission_requests: async permission approval with suspend/resume support
CREATE TABLE permission_requests (
    id TEXT PRIMARY KEY,  -- perm_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,

    -- Request details
    tool TEXT NOT NULL,           -- "bash", "edit", "browser", etc.
    action TEXT NOT NULL,         -- Command or description
    context TEXT,                 -- Additional context for display
    risk_level TEXT NOT NULL DEFAULT 'medium',

    -- Status: pending -> approved/denied/canceled
    status TEXT NOT NULL DEFAULT 'pending',

    -- Timing
    suspend_after_seconds INT NOT NULL DEFAULT 1800,  -- Auto-suspend session after 30 min
    -- No auto-deny: permissions stay pending until explicit response or session termination

    -- Response (when approved/denied)
    responded_by TEXT,            -- User/API key that responded
    response_reason TEXT,         -- Optional reason for approval/denial
    responded_at TIMESTAMPTZ,

    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_permission_status CHECK (
        status IN ('pending', 'approved', 'denied', 'canceled')
    ),
    CONSTRAINT valid_risk_level CHECK (
        risk_level IN ('low', 'medium', 'high', 'critical')
    )
);

CREATE INDEX idx_permission_requests_session ON permission_requests(session_id);
CREATE INDEX idx_permission_requests_conversation ON permission_requests(conversation_id);
CREATE INDEX idx_permission_requests_task ON permission_requests(task_id);
CREATE INDEX idx_permission_requests_pending ON permission_requests(session_id, status)
    WHERE status = 'pending';
CREATE INDEX idx_permission_requests_tenant ON permission_requests(tenant_id);

--------------------------------------------------------------------------------
-- CAS Storage (Content-Addressable Storage)
--------------------------------------------------------------------------------

-- Content-addressed chunks (global dedup with tenant-scoped encryption)
-- Note: Chunks are encrypted per-tenant, NOT globally deduped across tenants
CREATE TABLE chunks (
    hash TEXT NOT NULL,                -- SHA-256 hash of compressed content
    tenant_id TEXT NOT NULL,           -- Tenant isolation (chunks NOT shared across tenants)
    size BIGINT NOT NULL,              -- Compressed size in bytes
    ref_count INT NOT NULL DEFAULT 1,  -- Number of manifests referencing this chunk
    deleted_at TIMESTAMPTZ,            -- Soft delete for GC safety
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, hash)      -- Tenant-scoped dedup
);

CREATE INDEX idx_chunks_gc ON chunks(deleted_at) WHERE deleted_at IS NOT NULL;

-- Workspace manifests (snapshot metadata)
CREATE TABLE manifests (
    id TEXT PRIMARY KEY,  -- mfst_xxx
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    parent_id TEXT REFERENCES manifests(id) ON DELETE SET NULL,
    total_size BIGINT NOT NULL,

    -- Single chunk mode (workspaces < 100MB)
    single_chunk BOOLEAN NOT NULL DEFAULT FALSE,
    chunk_hash TEXT,

    -- CDC mode (workspaces >= 100MB)
    -- For large workspaces, file list is stored in object storage as JSONL:
    -- manifests/{tenant_id}/{workspace_id}/{manifest_id}.jsonl.zst.enc
    -- files_json is kept NULL for CDC mode (use JSONL for streaming efficiency)
    chunk_count INT NOT NULL DEFAULT 0,
    files_json JSONB,  -- Only used for small CDC manifests as cache; NULL for large ones

    tenant_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_manifests_workspace ON manifests(workspace_id, created_at DESC);
CREATE INDEX idx_manifests_tenant ON manifests(tenant_id);

--------------------------------------------------------------------------------
-- Encryption
--------------------------------------------------------------------------------

CREATE TABLE data_keys (
    id TEXT PRIMARY KEY,  -- dek_xxx
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    dek_encrypted TEXT NOT NULL,
    algorithm TEXT NOT NULL DEFAULT 'AES-256-GCM',
    kek_id TEXT,

    tenant_id TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(resource_type, resource_id)
);

CREATE INDEX idx_data_keys_resource ON data_keys(resource_type, resource_id);
CREATE INDEX idx_data_keys_tenant ON data_keys(tenant_id);

--------------------------------------------------------------------------------
-- Authentication
--------------------------------------------------------------------------------

CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,  -- key_xxx
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,    -- SHA-256 hex (64 chars)
    key_prefix TEXT NOT NULL,         -- mk_xxxxxxxx (for display/logging)
    hash_version INT NOT NULL DEFAULT 1,  -- 1=SHA-256, 2=HMAC-SHA256 (reserved)
    scopes TEXT[] NOT NULL DEFAULT '{}',
    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT
);

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX idx_api_keys_expires ON api_keys(expires_at)
    WHERE revoked_at IS NULL AND expires_at IS NOT NULL;

--------------------------------------------------------------------------------
-- Runner Tokens (for pool runners authentication)
--------------------------------------------------------------------------------

CREATE TABLE runner_tokens (
    id TEXT PRIMARY KEY,  -- rtok_xxx
    token_hash TEXT NOT NULL UNIQUE,    -- SHA-256 hex (64 chars)
    token_prefix TEXT NOT NULL,         -- rtok_xxxxxxxx (for display/logging)
    hash_version INT NOT NULL DEFAULT 1,  -- 1=SHA-256, 2=HMAC-SHA256 (reserved)

    -- Associated runner (NULL until first use)
    runner_id TEXT REFERENCES runners(id) ON DELETE CASCADE,

    -- Pool assignment
    pool_name TEXT NOT NULL,

    -- Token state
    status TEXT NOT NULL DEFAULT 'active',

    -- Rotation support
    previous_token_hash TEXT,  -- For graceful rotation
    rotation_deadline TIMESTAMPTZ,  -- Old token valid until this time

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,

    CONSTRAINT valid_token_status CHECK (
        status IN ('active', 'rotating', 'revoked', 'expired')
    )
);

CREATE INDEX idx_runner_tokens_hash ON runner_tokens(token_hash);
CREATE INDEX idx_runner_tokens_pool ON runner_tokens(pool_name);
CREATE INDEX idx_runner_tokens_runner ON runner_tokens(runner_id);
CREATE INDEX idx_runner_tokens_tenant ON runner_tokens(tenant_id);
CREATE INDEX idx_runner_tokens_expires ON runner_tokens(expires_at)
    WHERE status = 'active' AND expires_at IS NOT NULL;

--------------------------------------------------------------------------------
-- Raw Logs (Partitioned by day)
--------------------------------------------------------------------------------
-- Stores original stream output from agents exactly as received.
-- This is the source of truth for replay/audit.
-- Parsed into agent_events for structured queries.

-- Note: Foreign keys omitted for partitioned table compatibility
-- Referential integrity enforced at application layer
CREATE TABLE raw_logs (
    id TEXT NOT NULL,  -- rlog_xxx
    session_id TEXT NOT NULL,
    conversation_id TEXT,
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    runner_id TEXT NOT NULL,

    -- Stream type: stdout, stderr, json (agent-specific format)
    stream TEXT NOT NULL,

    -- Raw content as received from agent
    content BYTEA NOT NULL,

    -- Ordering
    sequence BIGINT NOT NULL,

    -- Processing status for async event parsing
    processed BOOLEAN NOT NULL DEFAULT FALSE,

    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE UNIQUE INDEX idx_raw_logs_run_seq ON raw_logs(run_id, sequence, created_at);
CREATE INDEX idx_raw_logs_task ON raw_logs(task_id, sequence);
CREATE INDEX idx_raw_logs_session ON raw_logs(session_id, created_at);
CREATE INDEX idx_raw_logs_tenant ON raw_logs(tenant_id, created_at);
CREATE INDEX idx_raw_logs_unprocessed ON raw_logs(created_at)
    WHERE processed = FALSE;

--------------------------------------------------------------------------------
-- Agent Events (Structured events parsed from raw logs)
--------------------------------------------------------------------------------
-- Provides a unified event format across all agent types.
-- Each agent executor implements a parser to convert raw logs to events.
-- Used by API/WebSocket for real-time streaming and queries.

CREATE TABLE agent_events (
    id TEXT PRIMARY KEY,  -- evt_xxx
    session_id TEXT NOT NULL,
    conversation_id TEXT,
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,

    -- Event type (unified across agents)
    -- text: assistant text output
    -- thinking: thinking/reasoning (if exposed)
    -- tool_use: tool invocation
    -- tool_result: tool execution result
    -- error: error message
    -- system: system notification
    -- usage: token usage stats
    type TEXT NOT NULL,

    -- Text content (for text, thinking, error, system types)
    content TEXT,

    -- Structured data (for tool_use, tool_result, usage types)
    -- tool_use: {"id": "...", "name": "bash", "input": {...}}
    -- tool_result: {"id": "...", "output": "...", "is_error": false}
    -- usage: {"input_tokens": 100, "output_tokens": 50}
    data JSONB,

    -- Reference to source raw log (for traceability)
    raw_log_id TEXT,

    -- Ordering (same sequence as raw_logs for correlation)
    sequence BIGINT NOT NULL,

    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_event_type CHECK (
        type IN ('text', 'thinking', 'tool_use', 'tool_result', 'error', 'system', 'usage')
    )
);

CREATE INDEX idx_agent_events_run ON agent_events(run_id, sequence);
CREATE INDEX idx_agent_events_task ON agent_events(task_id, sequence);
CREATE INDEX idx_agent_events_session ON agent_events(session_id, created_at);
CREATE INDEX idx_agent_events_conversation ON agent_events(conversation_id, created_at)
    WHERE conversation_id IS NOT NULL;
CREATE INDEX idx_agent_events_type ON agent_events(run_id, type);
CREATE INDEX idx_agent_events_tenant ON agent_events(tenant_id, created_at);

-- GIN index for querying structured data
CREATE INDEX idx_agent_events_data ON agent_events USING GIN (data)
    WHERE data IS NOT NULL;

--------------------------------------------------------------------------------
-- Log Archives
--------------------------------------------------------------------------------

CREATE TABLE log_archives (
    id TEXT PRIMARY KEY,  -- arch_xxx
    session_id TEXT NOT NULL UNIQUE,
    tenant_id TEXT,
    storage_key TEXT NOT NULL,
    storage_size_bytes BIGINT,
    log_count BIGINT NOT NULL,
    first_log_at TIMESTAMPTZ,
    last_log_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_log_archives_tenant ON log_archives(tenant_id);
CREATE INDEX idx_log_archives_expires ON log_archives(expires_at) WHERE deleted_at IS NULL;

--------------------------------------------------------------------------------
-- Snapshots
--------------------------------------------------------------------------------

CREATE TABLE snapshots (
    id TEXT PRIMARY KEY,  -- snap_xxx
    runner_id TEXT NOT NULL REFERENCES runners(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    provider_snapshot_id TEXT NOT NULL,
    storage_key TEXT,
    tenant_id TEXT,
    size_bytes BIGINT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX idx_snapshots_runner ON snapshots(runner_id);
CREATE UNIQUE INDEX idx_snapshots_runner_name ON snapshots(runner_id, name);
CREATE INDEX idx_snapshots_tenant ON snapshots(tenant_id);

--------------------------------------------------------------------------------
-- Audit Logs
--------------------------------------------------------------------------------

-- Action logs for security audit trail
-- Records sensitive actions like permission responses, config changes, etc.
CREATE TABLE action_logs (
    id TEXT PRIMARY KEY,  -- alog_xxx

    -- Actor
    actor_type TEXT NOT NULL,         -- "user", "api_key", "system", "runner"
    actor_id TEXT,                    -- API key ID, runner ID, or NULL for system
    actor_name TEXT,                  -- Human-readable name for display

    -- Action
    action TEXT NOT NULL,             -- "permission.approved", "permission.denied", etc.
    resource_type TEXT NOT NULL,      -- "permission_request", "session", "runner", etc.
    resource_id TEXT NOT NULL,        -- ID of the affected resource

    -- Context
    session_id TEXT,                  -- Associated session (if applicable)
    task_id TEXT,                     -- Associated task (if applicable)

    -- Details
    details JSONB NOT NULL DEFAULT '{}',  -- Action-specific details
    ip_address TEXT,                  -- Client IP address
    user_agent TEXT,                  -- Client user agent

    -- Result
    success BOOLEAN NOT NULL DEFAULT TRUE,
    error_message TEXT,

    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- No foreign keys for performance and to preserve logs even after resource deletion
    CONSTRAINT valid_actor_type CHECK (
        actor_type IN ('user', 'api_key', 'system', 'runner')
    )
);

-- Indexes for common query patterns
CREATE INDEX idx_action_logs_actor ON action_logs(actor_type, actor_id);
CREATE INDEX idx_action_logs_action ON action_logs(action);
CREATE INDEX idx_action_logs_resource ON action_logs(resource_type, resource_id);
CREATE INDEX idx_action_logs_session ON action_logs(session_id) WHERE session_id IS NOT NULL;
CREATE INDEX idx_action_logs_tenant_time ON action_logs(tenant_id, created_at DESC);
CREATE INDEX idx_action_logs_created ON action_logs(created_at DESC);

-- Partition by month for large deployments (optional)
-- Can convert to partitioned table later if needed

--------------------------------------------------------------------------------
-- Tunnels
--------------------------------------------------------------------------------

CREATE TABLE tunnels (
    id TEXT PRIMARY KEY,  -- tun_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,  -- Nullable: runner can detach
    type TEXT NOT NULL,

    -- Direction for network policy enforcement (especially air_gapped mode)
    -- "inbound": Server/User → Agent (viewing agent's screen/browser)
    -- "outbound": Agent → External (exposing agent's port to internet)
    direction TEXT NOT NULL DEFAULT 'inbound',

    local_port INT NOT NULL,
    public_url TEXT,

    -- Token authentication (hashed for security)
    -- See docs/auth.md for token design
    token_hash TEXT NOT NULL,           -- SHA-256 hex (64 chars)
    token_prefix TEXT NOT NULL,         -- ttok_xxxxxxxx (for display/logging)
    hash_version INT NOT NULL DEFAULT 1,  -- 1=SHA-256, 2=HMAC-SHA256 (reserved)

    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,  -- Required: tunnels must expire
    closed_at TIMESTAMPTZ,

    CONSTRAINT valid_tunnel_type CHECK (
        type IN ('http', 'tcp', 'desktop', 'browser', 'ios', 'android')
    ),
    CONSTRAINT valid_tunnel_direction CHECK (
        direction IN ('inbound', 'outbound')
    ),
    -- Enforce direction based on type for consistency
    -- inbound: desktop, browser, ios, android (streaming to user)
    -- outbound: http, tcp (exposing to internet)
    CONSTRAINT valid_type_direction CHECK (
        (type IN ('desktop', 'browser', 'ios', 'android') AND direction = 'inbound') OR
        (type IN ('http', 'tcp') AND direction = 'outbound')
    )
);

CREATE INDEX idx_tunnels_session ON tunnels(session_id);
CREATE INDEX idx_tunnels_active ON tunnels(session_id) WHERE closed_at IS NULL;
CREATE INDEX idx_tunnels_tenant ON tunnels(tenant_id);

--------------------------------------------------------------------------------
-- Log Partition Management
--------------------------------------------------------------------------------

-- Create initial partitions (current day + 7 days ahead)
-- Run this during initial setup

-- Function to create a partition for a specific date
CREATE OR REPLACE FUNCTION create_raw_log_partition(partition_date DATE)
RETURNS void AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    partition_name := 'raw_logs_' || TO_CHAR(partition_date, 'YYYYMMDD');
    start_date := partition_date;
    end_date := partition_date + INTERVAL '1 day';

    -- Check if partition already exists
    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = partition_name AND n.nspname = 'public'
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF raw_logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Function to ensure partitions exist for upcoming days
-- Should be called daily by a cron job or pg_cron
CREATE OR REPLACE FUNCTION maintain_raw_log_partitions(days_ahead INT DEFAULT 7)
RETURNS void AS $$
DECLARE
    target_date DATE;
BEGIN
    FOR i IN 0..days_ahead LOOP
        target_date := CURRENT_DATE + (i || ' days')::INTERVAL;
        PERFORM create_raw_log_partition(target_date);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Function to drop old partitions (beyond retention period)
-- Should be called by the log archiver job after archiving
CREATE OR REPLACE FUNCTION drop_old_raw_log_partitions(retention_days INT DEFAULT 7)
RETURNS void AS $$
DECLARE
    partition_record RECORD;
    cutoff_date DATE;
    partition_date DATE;
BEGIN
    cutoff_date := CURRENT_DATE - (retention_days || ' days')::INTERVAL;

    FOR partition_record IN
        SELECT c.relname as partition_name
        FROM pg_class c
        JOIN pg_inherits i ON c.oid = i.inhrelid
        JOIN pg_class p ON i.inhparent = p.oid
        WHERE p.relname = 'raw_logs'
    LOOP
        -- Extract date from partition name (raw_logs_YYYYMMDD)
        BEGIN
            partition_date := TO_DATE(
                SUBSTRING(partition_record.partition_name FROM 'raw_logs_(\d{8})'),
                'YYYYMMDD'
            );

            IF partition_date < cutoff_date THEN
                EXECUTE format('DROP TABLE IF EXISTS %I', partition_record.partition_name);
                RAISE NOTICE 'Dropped partition: %', partition_record.partition_name;
            END IF;
        EXCEPTION WHEN OTHERS THEN
            -- Skip partitions with unexpected naming
            CONTINUE;
        END;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Initialize partitions for current week + 7 days ahead
SELECT maintain_raw_log_partitions(7);

-- Example pg_cron setup (run after installing pg_cron extension):
-- SELECT cron.schedule('maintain-raw-log-partitions', '0 0 * * *', 'SELECT maintain_raw_log_partitions(7)');
