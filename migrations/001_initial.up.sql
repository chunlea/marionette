-- Marionette Database Schema
-- PostgreSQL 15+
--
-- Migration: 001_initial
-- Description: Initial schema for Marionette

--------------------------------------------------------------------------------
-- Configuration Tables (created first to avoid circular references)
--------------------------------------------------------------------------------

CREATE TABLE provider_configs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
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
CREATE UNIQUE INDEX idx_provider_configs_name_unique ON provider_configs(COALESCE(tenant_id, ''), name);
CREATE UNIQUE INDEX idx_provider_configs_default ON provider_configs(provider, COALESCE(tenant_id, ''))
    WHERE is_default = TRUE;

CREATE TABLE agent_configs (
    id TEXT PRIMARY KEY,
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_configs_agent ON agent_configs(agent);
CREATE INDEX idx_agent_configs_tenant ON agent_configs(tenant_id);
CREATE UNIQUE INDEX idx_agent_configs_name_unique ON agent_configs(COALESCE(tenant_id, ''), name);
CREATE UNIQUE INDEX idx_agent_configs_default ON agent_configs(agent, COALESCE(tenant_id, ''))
    WHERE is_default = TRUE;

CREATE TABLE profiles (
    id TEXT PRIMARY KEY,
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_profiles_tenant ON profiles(tenant_id);
CREATE UNIQUE INDEX idx_profiles_name_unique ON profiles(COALESCE(tenant_id, ''), name);

--------------------------------------------------------------------------------
-- Core Tables
--------------------------------------------------------------------------------

CREATE TABLE runners (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'offline',
    tainted BOOLEAN NOT NULL DEFAULT FALSE,
    taint_reason TEXT,
    sandbox_mode TEXT NOT NULL DEFAULT 'runner-is-sandbox',
    sandbox_types TEXT[] DEFAULT '{}',
    provider_config_id TEXT REFERENCES provider_configs(id) ON DELETE SET NULL,
    provider_instance_id TEXT,
    pool_name TEXT,
    profile_id TEXT REFERENCES profiles(id) ON DELETE SET NULL,
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    tenant_id TEXT,
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
);

CREATE INDEX idx_runners_labels ON runners USING GIN (labels);
CREATE INDEX idx_runners_pool ON runners(pool_name) WHERE pool_name IS NOT NULL;
CREATE INDEX idx_runners_status ON runners(status);
CREATE INDEX idx_runners_tenant ON runners(tenant_id);
CREATE INDEX idx_runners_tainted ON runners(pool_name, tainted) WHERE tainted = TRUE;
CREATE UNIQUE INDEX idx_runners_name_unique ON runners(COALESCE(tenant_id, ''), name);

CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
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

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    name TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    agent TEXT NOT NULL,
    is_byok BOOLEAN NOT NULL DEFAULT FALSE,
    agent_config_id TEXT REFERENCES agent_configs(id) ON DELETE SET NULL,
    agent_config_metadata JSONB,
    context_snapshot JSONB,
    agent_version TEXT,
    suspend_strategy TEXT,
    suspend_snapshot_id TEXT,
    suspend_workspace_synced BOOLEAN,
    previous_runner_id TEXT,
    network_policy TEXT NOT NULL DEFAULT 'allow_list',
    allowed_hosts TEXT[] NOT NULL DEFAULT '{}',
    lifecycle_mode TEXT NOT NULL DEFAULT 'on_demand',
    idle_timeout_seconds INT DEFAULT 1800,
    max_lifetime_seconds INT,
    schedule_cron TEXT,
    schedule_timezone TEXT DEFAULT 'UTC',
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

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
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
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_tenant ON tasks(tenant_id);
CREATE INDEX idx_tasks_pending ON tasks(session_id, status) WHERE status = 'pending';

CREATE TABLE task_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    attempt INT NOT NULL DEFAULT 1,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT,
    exit_code INT,
    tokens_input INT,
    tokens_output INT,
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

CREATE TABLE scheduled_tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    cron_expression TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    prompt_template TEXT NOT NULL,
    timeout_seconds INT NOT NULL DEFAULT 3600,
    max_retries INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    run_count INT NOT NULL DEFAULT 0,
    failure_count INT NOT NULL DEFAULT 0,
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

CREATE TABLE permission_requests (
    id TEXT PRIMARY KEY,
    original_request_id TEXT NOT NULL,  -- Original request ID from agent (e.g., tool_use_id)
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    tool TEXT NOT NULL,
    action TEXT NOT NULL,
    context TEXT,
    risk_level TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'pending',
    suspend_after_seconds INT NOT NULL DEFAULT 1800,
    responded_by TEXT,
    response_reason TEXT,
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
CREATE INDEX idx_permission_requests_task ON permission_requests(task_id);
CREATE INDEX idx_permission_requests_pending ON permission_requests(session_id, status)
    WHERE status = 'pending';
CREATE INDEX idx_permission_requests_tenant ON permission_requests(tenant_id);
CREATE INDEX idx_permission_requests_original_id ON permission_requests(original_request_id);

--------------------------------------------------------------------------------
-- CAS Storage (Content-Addressable Storage)
--------------------------------------------------------------------------------

CREATE TABLE chunks (
    hash TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    size BIGINT NOT NULL,
    ref_count INT NOT NULL DEFAULT 1,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, hash)
);

CREATE INDEX idx_chunks_gc ON chunks(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE manifests (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    parent_id TEXT REFERENCES manifests(id) ON DELETE SET NULL,
    total_size BIGINT NOT NULL,
    single_chunk BOOLEAN NOT NULL DEFAULT FALSE,
    chunk_hash TEXT,
    chunk_count INT NOT NULL DEFAULT 0,
    files_json JSONB,
    tenant_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_manifests_workspace ON manifests(workspace_id, created_at DESC);
CREATE INDEX idx_manifests_tenant ON manifests(tenant_id);

--------------------------------------------------------------------------------
-- Encryption
--------------------------------------------------------------------------------

CREATE TABLE data_keys (
    id TEXT PRIMARY KEY,
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
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    hash_version INT NOT NULL DEFAULT 1,
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

CREATE TABLE runner_tokens (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    hash_version INT NOT NULL DEFAULT 1,
    runner_id TEXT REFERENCES runners(id) ON DELETE CASCADE,
    pool_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    previous_token_hash TEXT,
    rotation_deadline TIMESTAMPTZ,
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
-- Logs (Partitioned by day)
--------------------------------------------------------------------------------

CREATE TABLE logs (
    id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    runner_id TEXT NOT NULL,
    stream TEXT NOT NULL DEFAULT 'stdout',
    level TEXT NOT NULL DEFAULT 'info',
    content TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    tenant_id TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE UNIQUE INDEX idx_logs_run_seq_unique ON logs(run_id, sequence, created_at);
CREATE INDEX idx_logs_task_seq ON logs(task_id, sequence);
CREATE INDEX idx_logs_session ON logs(session_id, created_at);
CREATE INDEX idx_logs_tenant ON logs(tenant_id, created_at);

CREATE TABLE log_archives (
    id TEXT PRIMARY KEY,
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
    id TEXT PRIMARY KEY,
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

CREATE TABLE action_logs (
    id TEXT PRIMARY KEY,
    actor_type TEXT NOT NULL,
    actor_id TEXT,
    actor_name TEXT,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    session_id TEXT,
    task_id TEXT,
    details JSONB NOT NULL DEFAULT '{}',
    ip_address TEXT,
    user_agent TEXT,
    success BOOLEAN NOT NULL DEFAULT TRUE,
    error_message TEXT,
    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_actor_type CHECK (
        actor_type IN ('user', 'api_key', 'system', 'runner')
    )
);

CREATE INDEX idx_action_logs_actor ON action_logs(actor_type, actor_id);
CREATE INDEX idx_action_logs_action ON action_logs(action);
CREATE INDEX idx_action_logs_resource ON action_logs(resource_type, resource_id);
CREATE INDEX idx_action_logs_session ON action_logs(session_id) WHERE session_id IS NOT NULL;
CREATE INDEX idx_action_logs_tenant_time ON action_logs(tenant_id, created_at DESC);
CREATE INDEX idx_action_logs_created ON action_logs(created_at DESC);

--------------------------------------------------------------------------------
-- Tunnels
--------------------------------------------------------------------------------

CREATE TABLE tunnels (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    type TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'inbound',
    local_port INT NOT NULL,
    public_url TEXT,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    hash_version INT NOT NULL DEFAULT 1,
    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    CONSTRAINT valid_tunnel_type CHECK (
        type IN ('http', 'tcp', 'desktop', 'browser', 'ios', 'android')
    ),
    CONSTRAINT valid_tunnel_direction CHECK (
        direction IN ('inbound', 'outbound')
    ),
    CONSTRAINT valid_type_direction CHECK (
        (type IN ('desktop', 'browser', 'ios', 'android') AND direction = 'inbound') OR
        (type IN ('http', 'tcp') AND direction = 'outbound')
    )
);

CREATE INDEX idx_tunnels_session ON tunnels(session_id);
CREATE INDEX idx_tunnels_active ON tunnels(session_id) WHERE closed_at IS NULL;
CREATE INDEX idx_tunnels_tenant ON tunnels(tenant_id);

--------------------------------------------------------------------------------
-- Log Partition Management Functions
--------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION create_log_partition(partition_date DATE)
RETURNS void AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
BEGIN
    partition_name := 'logs_' || TO_CHAR(partition_date, 'YYYYMMDD');
    start_date := partition_date;
    end_date := partition_date + INTERVAL '1 day';

    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = partition_name AND n.nspname = 'public'
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION maintain_log_partitions(days_ahead INT DEFAULT 7)
RETURNS void AS $$
DECLARE
    target_date DATE;
BEGIN
    FOR i IN 0..days_ahead LOOP
        target_date := CURRENT_DATE + (i || ' days')::INTERVAL;
        PERFORM create_log_partition(target_date);
    END LOOP;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION drop_old_log_partitions(retention_days INT DEFAULT 7)
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
        WHERE p.relname = 'logs'
    LOOP
        BEGIN
            partition_date := TO_DATE(
                SUBSTRING(partition_record.partition_name FROM 'logs_(\d{8})'),
                'YYYYMMDD'
            );

            IF partition_date < cutoff_date THEN
                EXECUTE format('DROP TABLE IF EXISTS %I', partition_record.partition_name);
                RAISE NOTICE 'Dropped partition: %', partition_record.partition_name;
            END IF;
        EXCEPTION WHEN OTHERS THEN
            CONTINUE;
        END;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Initialize partitions for current week + 7 days ahead
SELECT maintain_log_partitions(7);
