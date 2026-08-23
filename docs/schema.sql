--
-- GENERATED FILE — DO NOT EDIT.
--
-- Source of truth: migrations/*.up.sql
-- Prose/design notes: scripts/schema-header.sql
-- Regenerate with:  make schema
-- Drift is checked in CI (make schema-check).
--
-- Rendered from postgres:16-alpine. The daily partitions of `logs` are omitted: they
-- are created at runtime by pkg/jobs.PartitionMaintainer, not by migrations.
--

-- Marionette Database Schema — DESIGN NOTES
--
-- This file is the hand-written preamble that scripts/gen-schema.sh prepends
-- to the generated docs/schema.sql. Edit this file for prose; edit
-- migrations/*.up.sql for the actual schema.
--
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
--   task_   task
--   trun_   task_run
--   stsk_   scheduled_task
--   perm_   permission_request
--   ws_     workspace
--   key_    api_key
--   rtok_   runner_token
--   dek_    data_key
--   log_    log entry
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

--
-- PostgreSQL database dump
--

--
-- Name: create_log_partition(date); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.create_log_partition(partition_date date) RETURNS void
    LANGUAGE plpgsql
    AS $$
DECLARE
    partition_name TEXT;
    start_date DATE;
    end_date DATE;
    stranded BIGINT := 0;
BEGIN
    partition_name := 'logs_' || TO_CHAR(partition_date, 'YYYYMMDD');
    start_date := partition_date;
    end_date := partition_date + INTERVAL '1 day';

    IF to_regclass('public.' || quote_ident(partition_name)) IS NOT NULL THEN
        RETURN;
    END IF;

    -- Rows for this day may already sit in the default partition (maintenance
    -- was not running). Count them before deciding how to create the partition.
    IF to_regclass('public.logs_default') IS NOT NULL THEN
        EXECUTE format(
            'SELECT count(*) FROM logs_default WHERE created_at >= %L AND created_at < %L',
            start_date, end_date
        ) INTO stranded;
    END IF;

    IF stranded = 0 THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
    ELSE
        -- Attaching on top of those rows would fail, so build the table
        -- detached, move the rows into it, then attach.
        EXECUTE format(
            'CREATE TABLE %I (LIKE logs INCLUDING DEFAULTS INCLUDING CONSTRAINTS)',
            partition_name
        );
        EXECUTE format(
            'WITH moved AS (' ||
            '  DELETE FROM logs_default WHERE created_at >= %L AND created_at < %L RETURNING *' ||
            ') INSERT INTO %I SELECT * FROM moved',
            start_date, end_date, partition_name
        );
        EXECUTE format(
            'ALTER TABLE logs ATTACH PARTITION %I FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Drained % rows from logs_default into %', stranded, partition_name;
    END IF;

    RAISE NOTICE 'Created partition: %', partition_name;
END;
$$;

--
-- Name: drop_old_log_partitions(integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.drop_old_log_partitions(retention_days integer DEFAULT 7) RETURNS void
    LANGUAGE plpgsql
    AS $$
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
        IF partition_record.partition_name = 'logs_default' THEN
            CONTINUE;
        END IF;

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
            -- Skip partitions with unexpected naming
            CONTINUE;
        END;
    END LOOP;
END;
$$;

--
-- Name: maintain_log_partitions(integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.maintain_log_partitions(days_ahead integer DEFAULT 7) RETURNS void
    LANGUAGE plpgsql
    AS $$
DECLARE
    target_date DATE;
BEGIN
    FOR i IN 0..days_ahead LOOP
        target_date := CURRENT_DATE + (i || ' days')::INTERVAL;
        PERFORM create_log_partition(target_date);
    END LOOP;
END;
$$;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: action_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.action_logs (
    id text NOT NULL,
    actor_type text NOT NULL,
    actor_id text,
    actor_name text,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    session_id text,
    task_id text,
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    ip_address text,
    user_agent text,
    success boolean DEFAULT true NOT NULL,
    error_message text,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_actor_type CHECK ((actor_type = ANY (ARRAY['user'::text, 'api_key'::text, 'system'::text, 'runner'::text])))
);

ALTER TABLE ONLY public.action_logs FORCE ROW LEVEL SECURITY;

--
-- Name: agent_configs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_configs (
    id text NOT NULL,
    name text NOT NULL,
    agent text NOT NULL,
    api_key_encrypted text NOT NULL,
    model text,
    base_url text,
    extra jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.agent_configs FORCE ROW LEVEL SECURITY;

--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id text NOT NULL,
    name text NOT NULL,
    key_hash text NOT NULL,
    key_prefix text NOT NULL,
    hash_version integer DEFAULT 1 NOT NULL,
    scopes text[] DEFAULT '{}'::text[] NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by text,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    revoke_reason text
);

ALTER TABLE ONLY public.api_keys FORCE ROW LEVEL SECURITY;

--
-- Name: chunks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chunks (
    hash text NOT NULL,
    tenant_id text NOT NULL,
    size bigint NOT NULL,
    ref_count integer DEFAULT 1 NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.chunks FORCE ROW LEVEL SECURITY;

--
-- Name: data_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.data_keys (
    id text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    dek_encrypted text NOT NULL,
    algorithm text DEFAULT 'AES-256-GCM'::text NOT NULL,
    kek_id text,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    rotated_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.data_keys FORCE ROW LEVEL SECURITY;

--
-- Name: log_archives; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.log_archives (
    id text NOT NULL,
    session_id text NOT NULL,
    tenant_id text,
    storage_key text NOT NULL,
    storage_size_bytes bigint,
    log_count bigint NOT NULL,
    first_log_at timestamp with time zone,
    last_log_at timestamp with time zone,
    archived_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    deleted_at timestamp with time zone,
    format text DEFAULT 'ndjson+zstd/frames1'::text NOT NULL,
    encrypted boolean DEFAULT false NOT NULL,
    last_log_id text,
    last_log_sequence bigint
);

ALTER TABLE ONLY public.log_archives FORCE ROW LEVEL SECURITY;

--
-- Name: logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.logs (
    id text NOT NULL,
    session_id text NOT NULL,
    task_id text NOT NULL,
    run_id text NOT NULL,
    runner_id text NOT NULL,
    stream text DEFAULT 'stdout'::text NOT NULL,
    level text DEFAULT 'info'::text NOT NULL,
    content text NOT NULL,
    sequence bigint NOT NULL,
    tenant_id text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (created_at);

ALTER TABLE ONLY public.logs FORCE ROW LEVEL SECURITY;

--
-- Name: logs_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.logs_default (
    id text NOT NULL,
    session_id text NOT NULL,
    task_id text NOT NULL,
    run_id text NOT NULL,
    runner_id text NOT NULL,
    stream text DEFAULT 'stdout'::text NOT NULL,
    level text DEFAULT 'info'::text NOT NULL,
    content text NOT NULL,
    sequence bigint NOT NULL,
    tenant_id text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.logs_default FORCE ROW LEVEL SECURITY;

--
-- Name: manifests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.manifests (
    id text NOT NULL,
    workspace_id text NOT NULL,
    parent_id text,
    total_size bigint NOT NULL,
    single_chunk boolean DEFAULT false NOT NULL,
    chunk_hash text,
    chunk_count integer DEFAULT 0 NOT NULL,
    files_json jsonb,
    tenant_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.manifests FORCE ROW LEVEL SECURITY;

--
-- Name: permission_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.permission_requests (
    id text NOT NULL,
    original_request_id text NOT NULL,
    session_id text NOT NULL,
    task_id text NOT NULL,
    run_id text NOT NULL,
    tool text NOT NULL,
    action text NOT NULL,
    context text,
    risk_level text DEFAULT 'medium'::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    suspend_after_seconds integer DEFAULT 1800 NOT NULL,
    responded_by text,
    response_reason text,
    responded_at timestamp with time zone,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_permission_status CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'denied'::text, 'canceled'::text]))),
    CONSTRAINT valid_risk_level CHECK ((risk_level = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text])))
);

ALTER TABLE ONLY public.permission_requests FORCE ROW LEVEL SECURITY;

--
-- Name: profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.profiles (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    provider_config_id text,
    tenant_id text,
    resources jsonb DEFAULT '{}'::jsonb NOT NULL,
    network jsonb DEFAULT '{}'::jsonb NOT NULL,
    init_script text,
    cleanup_script text,
    tunnels jsonb DEFAULT '[]'::jsonb NOT NULL,
    selector jsonb DEFAULT '{}'::jsonb NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_builtin boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.profiles FORCE ROW LEVEL SECURITY;

--
-- Name: provider_configs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_configs (
    id text NOT NULL,
    name text NOT NULL,
    provider text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    suspend_config jsonb DEFAULT '{"fallback": "terminate", "strategy": "terminate", "max_duration": "24h", "min_duration": "60s"}'::jsonb NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_suspend_strategy CHECK ((((suspend_config ->> 'strategy'::text) IS NULL) OR ((suspend_config ->> 'strategy'::text) = ANY (ARRAY['pause'::text, 'snapshot'::text, 'terminate_preserve_storage'::text, 'release_to_pool'::text, 'terminate'::text]))))
);

ALTER TABLE ONLY public.provider_configs FORCE ROW LEVEL SECURITY;

--
-- Name: runner_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.runner_tokens (
    id text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    hash_version integer DEFAULT 1 NOT NULL,
    runner_id text,
    pool_name text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    previous_token_hash text,
    rotation_deadline timestamp with time zone,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by text,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    revoke_reason text,
    CONSTRAINT valid_token_status CHECK ((status = ANY (ARRAY['active'::text, 'rotating'::text, 'revoked'::text, 'expired'::text])))
);

ALTER TABLE ONLY public.runner_tokens FORCE ROW LEVEL SECURITY;

--
-- Name: runners; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.runners (
    id text NOT NULL,
    name text NOT NULL,
    hostname text NOT NULL,
    status text DEFAULT 'offline'::text NOT NULL,
    tainted boolean DEFAULT false NOT NULL,
    taint_reason text,
    sandbox_mode text DEFAULT 'runner-is-sandbox'::text NOT NULL,
    sandbox_types text[] DEFAULT '{}'::text[],
    provider_config_id text,
    provider_instance_id text,
    pool_name text,
    profile_id text,
    capabilities text[] DEFAULT '{}'::text[] NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    claim_session_id text,
    claimed_at timestamp with time zone,
    connected_replica_id text,
    connected_at timestamp with time zone,
    CONSTRAINT valid_runner_status CHECK ((status = ANY (ARRAY['offline'::text, 'idle'::text, 'busy'::text, 'paused'::text]))),
    CONSTRAINT valid_sandbox_mode CHECK ((sandbox_mode = ANY (ARRAY['runner-is-sandbox'::text, 'runner-creates-sandbox'::text, 'none'::text])))
);

ALTER TABLE ONLY public.runners FORCE ROW LEVEL SECURITY;

--
-- Name: scheduled_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scheduled_tasks (
    id text NOT NULL,
    session_id text NOT NULL,
    name text NOT NULL,
    description text,
    cron_expression text NOT NULL,
    timezone text DEFAULT 'UTC'::text NOT NULL,
    prompt_template text NOT NULL,
    timeout_seconds integer DEFAULT 3600 NOT NULL,
    max_retries integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    next_run_at timestamp with time zone,
    last_run_at timestamp with time zone,
    last_task_id text,
    run_count integer DEFAULT 0 NOT NULL,
    failure_count integer DEFAULT 0 NOT NULL,
    on_failure text DEFAULT 'continue'::text NOT NULL,
    max_consecutive_failures integer DEFAULT 3,
    consecutive_failures integer DEFAULT 0 NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_on_failure CHECK ((on_failure = ANY (ARRAY['continue'::text, 'pause_on_failure'::text, 'disable_on_failure'::text]))),
    CONSTRAINT valid_scheduled_task_status CHECK ((status = ANY (ARRAY['active'::text, 'paused'::text, 'disabled'::text])))
);

ALTER TABLE ONLY public.scheduled_tasks FORCE ROW LEVEL SECURITY;

--
-- Name: server_replicas; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.server_replicas (
    id text NOT NULL,
    advertise_addr text NOT NULL,
    version text,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    last_heartbeat_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id text NOT NULL,
    name text,
    status text DEFAULT 'pending'::text NOT NULL,
    runner_id text,
    workspace_id text NOT NULL,
    agent text NOT NULL,
    is_byok boolean DEFAULT false NOT NULL,
    agent_config_id text,
    agent_config_metadata jsonb,
    context_snapshot jsonb,
    agent_version text,
    suspend_strategy text,
    suspend_snapshot_id text,
    suspend_workspace_synced boolean,
    previous_runner_id text,
    network_policy text DEFAULT 'allow_list'::text NOT NULL,
    allowed_hosts text[] DEFAULT '{}'::text[] NOT NULL,
    lifecycle_mode text DEFAULT 'on_demand'::text NOT NULL,
    idle_timeout_seconds integer DEFAULT 1800,
    max_lifetime_seconds integer,
    schedule_cron text,
    schedule_timezone text DEFAULT 'UTC'::text,
    next_scheduled_at timestamp with time zone,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_activity_at timestamp with time zone,
    suspended_at timestamp with time zone,
    resumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    profile_id text,
    workspace_manifest_id text,
    CONSTRAINT scheduled_requires_cron CHECK (((lifecycle_mode <> 'scheduled'::text) OR (schedule_cron IS NOT NULL))),
    CONSTRAINT valid_lifecycle_mode CHECK ((lifecycle_mode = ANY (ARRAY['on_demand'::text, 'always_on'::text, 'scheduled'::text]))),
    CONSTRAINT valid_network_policy CHECK ((network_policy = ANY (ARRAY['none'::text, 'allow_list'::text, 'proxy'::text, 'air_gapped'::text]))),
    CONSTRAINT valid_session_status CHECK ((status = ANY (ARRAY['pending'::text, 'active'::text, 'suspended'::text, 'resuming'::text, 'terminated'::text]))),
    CONSTRAINT valid_suspend_strategy CHECK (((suspend_strategy IS NULL) OR (suspend_strategy = ANY (ARRAY['pause'::text, 'snapshot'::text, 'terminate_preserve_storage'::text, 'release_to_pool'::text, 'terminate'::text]))))
);

ALTER TABLE ONLY public.sessions FORCE ROW LEVEL SECURITY;

--
-- Name: snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.snapshots (
    id text NOT NULL,
    runner_id text NOT NULL,
    name text NOT NULL,
    provider_snapshot_id text NOT NULL,
    storage_key text,
    tenant_id text,
    size_bytes bigint,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone
);

ALTER TABLE ONLY public.snapshots FORCE ROW LEVEL SECURITY;

--
-- Name: streams; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.streams (
    id text NOT NULL,
    session_id text NOT NULL,
    runner_id text,
    tenant_id text,
    type text NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    signaling_url text,
    ice_servers jsonb DEFAULT '[]'::jsonb NOT NULL,
    resolution_width integer,
    resolution_height integer,
    frame_rate integer,
    bitrate integer,
    video_codec text,
    audio_codec text,
    audio_enabled boolean DEFAULT false NOT NULL,
    input_enabled boolean DEFAULT false NOT NULL,
    provider_name text NOT NULL,
    provider_stream_id text,
    error text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    stopped_at timestamp with time zone,
    expires_at timestamp with time zone,
    device_serial text,
    device_name text,
    android_version text,
    CONSTRAINT valid_stream_state CHECK ((state = ANY (ARRAY['pending'::text, 'starting'::text, 'active'::text, 'paused'::text, 'stopping'::text, 'stopped'::text, 'error'::text]))),
    CONSTRAINT valid_stream_type CHECK ((type = ANY (ARRAY['desktop'::text, 'browser'::text, 'ios'::text, 'android'::text])))
);

ALTER TABLE ONLY public.streams FORCE ROW LEVEL SECURITY;

--
-- Name: COLUMN streams.device_serial; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.streams.device_serial IS 'Android device serial number (e.g., emulator-5554 or USB serial)';

--
-- Name: COLUMN streams.device_name; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.streams.device_name IS 'Android device model name';

--
-- Name: COLUMN streams.android_version; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.streams.android_version IS 'Android SDK version';

--
-- Name: task_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_runs (
    id text NOT NULL,
    task_id text NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    runner_id text,
    status text DEFAULT 'pending'::text NOT NULL,
    error text,
    exit_code integer,
    tokens_input integer,
    tokens_output integer,
    tenant_id text,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    assigned_at timestamp with time zone,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_task_run_status CHECK ((status = ANY (ARRAY['pending'::text, 'assigned'::text, 'running'::text, 'completed'::text, 'failed'::text, 'timeout'::text, 'canceled'::text])))
);

ALTER TABLE ONLY public.task_runs FORCE ROW LEVEL SECURITY;

--
-- Name: tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tasks (
    id text NOT NULL,
    session_id text NOT NULL,
    prompt text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    max_retries integer DEFAULT 0 NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    timeout_seconds integer DEFAULT 3600 NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    next_dispatch_after timestamp with time zone,
    dispatch_attempts integer DEFAULT 0 NOT NULL,
    dispatch_parked_reason text,
    CONSTRAINT valid_task_status CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'completed'::text, 'failed'::text, 'canceled'::text])))
);

ALTER TABLE ONLY public.tasks FORCE ROW LEVEL SECURITY;

--
-- Name: tunnels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tunnels (
    id text NOT NULL,
    session_id text NOT NULL,
    runner_id text,
    type text NOT NULL,
    direction text DEFAULT 'inbound'::text NOT NULL,
    local_port integer NOT NULL,
    public_url text,
    is_public boolean DEFAULT false NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    hash_version integer DEFAULT 1 NOT NULL,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    closed_at timestamp with time zone,
    CONSTRAINT valid_tunnel_direction CHECK ((direction = ANY (ARRAY['inbound'::text, 'outbound'::text]))),
    CONSTRAINT valid_tunnel_type CHECK ((type = ANY (ARRAY['http'::text, 'tcp'::text, 'desktop'::text, 'browser'::text, 'ios'::text, 'android'::text]))),
    CONSTRAINT valid_type_direction CHECK ((((type = ANY (ARRAY['desktop'::text, 'browser'::text, 'ios'::text, 'android'::text])) AND (direction = 'inbound'::text)) OR ((type = ANY (ARRAY['http'::text, 'tcp'::text])) AND (direction = 'outbound'::text))))
);

ALTER TABLE ONLY public.tunnels FORCE ROW LEVEL SECURITY;

--
-- Name: webhook_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_events (
    id text NOT NULL,
    webhook_id text NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text,
    last_status_code integer,
    next_retry_at timestamp with time zone,
    delivered_at timestamp with time zone,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_webhook_event_status CHECK ((status = ANY (ARRAY['pending'::text, 'delivered'::text, 'failed'::text, 'exhausted'::text, 'canceled'::text])))
);

ALTER TABLE ONLY public.webhook_events FORCE ROW LEVEL SECURITY;

--
-- Name: webhooks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhooks (
    id text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    events text[] DEFAULT '{}'::text[] NOT NULL,
    secret_encrypted text NOT NULL,
    secret_hash text NOT NULL,
    secret_prefix text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    max_retries integer DEFAULT 3 NOT NULL,
    retry_delay_seconds integer DEFAULT 60 NOT NULL,
    timeout_seconds integer DEFAULT 30 NOT NULL,
    headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.webhooks FORCE ROW LEVEL SECURITY;

--
-- Name: workspaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspaces (
    id text NOT NULL,
    name text NOT NULL,
    persist boolean DEFAULT true NOT NULL,
    storage_type text DEFAULT 'volume'::text NOT NULL,
    storage_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    mobility text DEFAULT 'local'::text NOT NULL,
    storage_domain text,
    storage_key text,
    storage_size_bytes bigint,
    last_synced_at timestamp with time zone,
    disk_quota_mb integer,
    tenant_id text,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT valid_mobility CHECK ((mobility = ANY (ARRAY['local'::text, 'shared'::text, 'object_sync'::text])))
);

ALTER TABLE ONLY public.workspaces FORCE ROW LEVEL SECURITY;

--
-- Name: logs_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.logs ATTACH PARTITION public.logs_default DEFAULT;

--
-- Name: action_logs action_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.action_logs
    ADD CONSTRAINT action_logs_pkey PRIMARY KEY (id);

--
-- Name: agent_configs agent_configs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_configs
    ADD CONSTRAINT agent_configs_pkey PRIMARY KEY (id);

--
-- Name: api_keys api_keys_key_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_key_hash_key UNIQUE (key_hash);

--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);

--
-- Name: chunks chunks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chunks
    ADD CONSTRAINT chunks_pkey PRIMARY KEY (tenant_id, hash);

--
-- Name: data_keys data_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.data_keys
    ADD CONSTRAINT data_keys_pkey PRIMARY KEY (id);

--
-- Name: data_keys data_keys_resource_type_resource_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.data_keys
    ADD CONSTRAINT data_keys_resource_type_resource_id_key UNIQUE (resource_type, resource_id);

--
-- Name: log_archives log_archives_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.log_archives
    ADD CONSTRAINT log_archives_pkey PRIMARY KEY (id);

--
-- Name: log_archives log_archives_session_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.log_archives
    ADD CONSTRAINT log_archives_session_id_key UNIQUE (session_id);

--
-- Name: logs logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.logs
    ADD CONSTRAINT logs_pkey PRIMARY KEY (id, created_at);

--
-- Name: logs_default logs_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.logs_default
    ADD CONSTRAINT logs_default_pkey PRIMARY KEY (id, created_at);

--
-- Name: manifests manifests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.manifests
    ADD CONSTRAINT manifests_pkey PRIMARY KEY (id);

--
-- Name: permission_requests permission_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.permission_requests
    ADD CONSTRAINT permission_requests_pkey PRIMARY KEY (id);

--
-- Name: profiles profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.profiles
    ADD CONSTRAINT profiles_pkey PRIMARY KEY (id);

--
-- Name: provider_configs provider_configs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_configs
    ADD CONSTRAINT provider_configs_pkey PRIMARY KEY (id);

--
-- Name: runner_tokens runner_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runner_tokens
    ADD CONSTRAINT runner_tokens_pkey PRIMARY KEY (id);

--
-- Name: runner_tokens runner_tokens_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runner_tokens
    ADD CONSTRAINT runner_tokens_token_hash_key UNIQUE (token_hash);

--
-- Name: runners runners_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runners
    ADD CONSTRAINT runners_pkey PRIMARY KEY (id);

--
-- Name: scheduled_tasks scheduled_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_tasks
    ADD CONSTRAINT scheduled_tasks_pkey PRIMARY KEY (id);

--
-- Name: server_replicas server_replicas_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.server_replicas
    ADD CONSTRAINT server_replicas_pkey PRIMARY KEY (id);

--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);

--
-- Name: snapshots snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.snapshots
    ADD CONSTRAINT snapshots_pkey PRIMARY KEY (id);

--
-- Name: streams streams_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.streams
    ADD CONSTRAINT streams_pkey PRIMARY KEY (id);

--
-- Name: task_runs task_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_runs
    ADD CONSTRAINT task_runs_pkey PRIMARY KEY (id);

--
-- Name: task_runs task_runs_task_id_attempt_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_runs
    ADD CONSTRAINT task_runs_task_id_attempt_key UNIQUE (task_id, attempt);

--
-- Name: tasks tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);

--
-- Name: tunnels tunnels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tunnels
    ADD CONSTRAINT tunnels_pkey PRIMARY KEY (id);

--
-- Name: webhook_events webhook_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_events
    ADD CONSTRAINT webhook_events_pkey PRIMARY KEY (id);

--
-- Name: webhooks webhooks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhooks
    ADD CONSTRAINT webhooks_pkey PRIMARY KEY (id);

--
-- Name: workspaces workspaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);

--
-- Name: idx_action_logs_action; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_action_logs_action ON public.action_logs USING btree (action);

--
-- Name: idx_action_logs_actor; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_action_logs_actor ON public.action_logs USING btree (actor_type, actor_id);

--
-- Name: idx_action_logs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_action_logs_created ON public.action_logs USING btree (created_at DESC);

--
-- Name: idx_action_logs_resource; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_action_logs_resource ON public.action_logs USING btree (resource_type, resource_id);

--
-- Name: idx_action_logs_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_action_logs_session ON public.action_logs USING btree (session_id) WHERE (session_id IS NOT NULL);

--
-- Name: idx_action_logs_tenant_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_action_logs_tenant_time ON public.action_logs USING btree (tenant_id, created_at DESC);

--
-- Name: idx_agent_configs_agent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_configs_agent ON public.agent_configs USING btree (agent);

--
-- Name: idx_agent_configs_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_agent_configs_default ON public.agent_configs USING btree (agent, COALESCE(tenant_id, ''::text)) WHERE (is_default = true);

--
-- Name: idx_agent_configs_name_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_agent_configs_name_unique ON public.agent_configs USING btree (COALESCE(tenant_id, ''::text), name);

--
-- Name: idx_agent_configs_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_agent_configs_tenant ON public.agent_configs USING btree (tenant_id);

--
-- Name: idx_api_keys_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_expires ON public.api_keys USING btree (expires_at) WHERE ((revoked_at IS NULL) AND (expires_at IS NOT NULL));

--
-- Name: idx_api_keys_key_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_key_hash ON public.api_keys USING btree (key_hash);

--
-- Name: idx_api_keys_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_tenant ON public.api_keys USING btree (tenant_id);

--
-- Name: idx_chunks_gc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chunks_gc ON public.chunks USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);

--
-- Name: idx_data_keys_resource; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_data_keys_resource ON public.data_keys USING btree (resource_type, resource_id);

--
-- Name: idx_data_keys_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_data_keys_tenant ON public.data_keys USING btree (tenant_id);

--
-- Name: idx_log_archives_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_log_archives_expires ON public.log_archives USING btree (expires_at) WHERE (deleted_at IS NULL);

--
-- Name: idx_log_archives_purge; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_log_archives_purge ON public.log_archives USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);

--
-- Name: idx_log_archives_session_coverage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_log_archives_session_coverage ON public.log_archives USING btree (session_id, last_log_at) WHERE (deleted_at IS NULL);

--
-- Name: idx_log_archives_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_log_archives_tenant ON public.log_archives USING btree (tenant_id);

--
-- Name: idx_logs_run_seq_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_logs_run_seq_unique ON ONLY public.logs USING btree (run_id, sequence, created_at);

--
-- Name: idx_logs_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_session ON ONLY public.logs USING btree (session_id, created_at);

--
-- Name: idx_logs_task_seq; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_task_seq ON ONLY public.logs USING btree (task_id, sequence);

--
-- Name: idx_logs_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_logs_tenant ON ONLY public.logs USING btree (tenant_id, created_at);

--
-- Name: idx_manifests_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_manifests_tenant ON public.manifests USING btree (tenant_id);

--
-- Name: idx_manifests_workspace; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_manifests_workspace ON public.manifests USING btree (workspace_id, created_at DESC);

--
-- Name: idx_permission_requests_original_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_permission_requests_original_id ON public.permission_requests USING btree (original_request_id);

--
-- Name: idx_permission_requests_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_permission_requests_pending ON public.permission_requests USING btree (session_id, status) WHERE (status = 'pending'::text);

--
-- Name: idx_permission_requests_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_permission_requests_session ON public.permission_requests USING btree (session_id);

--
-- Name: idx_permission_requests_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_permission_requests_task ON public.permission_requests USING btree (task_id);

--
-- Name: idx_permission_requests_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_permission_requests_tenant ON public.permission_requests USING btree (tenant_id);

--
-- Name: idx_profiles_name_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_profiles_name_unique ON public.profiles USING btree (COALESCE(tenant_id, ''::text), name);

--
-- Name: idx_profiles_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_profiles_tenant ON public.profiles USING btree (tenant_id);

--
-- Name: idx_provider_configs_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_provider_configs_default ON public.provider_configs USING btree (provider, COALESCE(tenant_id, ''::text)) WHERE (is_default = true);

--
-- Name: idx_provider_configs_name_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_provider_configs_name_unique ON public.provider_configs USING btree (COALESCE(tenant_id, ''::text), name);

--
-- Name: idx_provider_configs_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_configs_provider ON public.provider_configs USING btree (provider);

--
-- Name: idx_provider_configs_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_configs_tenant ON public.provider_configs USING btree (tenant_id);

--
-- Name: idx_runner_tokens_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runner_tokens_expires ON public.runner_tokens USING btree (expires_at) WHERE ((status = 'active'::text) AND (expires_at IS NOT NULL));

--
-- Name: idx_runner_tokens_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runner_tokens_hash ON public.runner_tokens USING btree (token_hash);

--
-- Name: idx_runner_tokens_pool; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runner_tokens_pool ON public.runner_tokens USING btree (pool_name);

--
-- Name: idx_runner_tokens_runner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runner_tokens_runner ON public.runner_tokens USING btree (runner_id);

--
-- Name: idx_runner_tokens_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runner_tokens_tenant ON public.runner_tokens USING btree (tenant_id);

--
-- Name: idx_runners_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_claim ON public.runners USING btree (claim_session_id, claimed_at) WHERE (claim_session_id IS NOT NULL);

--
-- Name: idx_runners_connected_replica; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_connected_replica ON public.runners USING btree (connected_replica_id) WHERE (connected_replica_id IS NOT NULL);

--
-- Name: idx_runners_labels; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_labels ON public.runners USING gin (labels);

--
-- Name: idx_runners_name_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_runners_name_unique ON public.runners USING btree (COALESCE(tenant_id, ''::text), name);

--
-- Name: idx_runners_pool; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_pool ON public.runners USING btree (pool_name) WHERE (pool_name IS NOT NULL);

--
-- Name: idx_runners_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_status ON public.runners USING btree (status);

--
-- Name: idx_runners_tainted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_tainted ON public.runners USING btree (pool_name, tainted) WHERE (tainted = true);

--
-- Name: idx_runners_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runners_tenant ON public.runners USING btree (tenant_id);

--
-- Name: idx_scheduled_tasks_next_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_next_run ON public.scheduled_tasks USING btree (next_run_at) WHERE (status = 'active'::text);

--
-- Name: idx_scheduled_tasks_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_session ON public.scheduled_tasks USING btree (session_id);

--
-- Name: idx_scheduled_tasks_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_tenant ON public.scheduled_tasks USING btree (tenant_id);

--
-- Name: idx_server_replicas_heartbeat; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_server_replicas_heartbeat ON public.server_replicas USING btree (last_heartbeat_at);

--
-- Name: idx_sessions_active_runner; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_sessions_active_runner ON public.sessions USING btree (runner_id) WHERE ((runner_id IS NOT NULL) AND (status = 'active'::text));

--
-- Name: idx_sessions_always_on; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_always_on ON public.sessions USING btree (tenant_id) WHERE (lifecycle_mode = 'always_on'::text);

--
-- Name: idx_sessions_labels; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_labels ON public.sessions USING gin (labels);

--
-- Name: idx_sessions_profile; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_profile ON public.sessions USING btree (profile_id) WHERE (profile_id IS NOT NULL);

--
-- Name: idx_sessions_runner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_runner ON public.sessions USING btree (runner_id);

--
-- Name: idx_sessions_scheduled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_scheduled ON public.sessions USING btree (next_scheduled_at) WHERE ((lifecycle_mode = 'scheduled'::text) AND (status = 'suspended'::text));

--
-- Name: idx_sessions_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_status ON public.sessions USING btree (status);

--
-- Name: idx_sessions_suspended; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_suspended ON public.sessions USING btree (tenant_id, suspended_at) WHERE (status = 'suspended'::text);

--
-- Name: idx_sessions_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_tenant ON public.sessions USING btree (tenant_id);

--
-- Name: idx_snapshots_runner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_snapshots_runner ON public.snapshots USING btree (runner_id);

--
-- Name: idx_snapshots_runner_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_snapshots_runner_name ON public.snapshots USING btree (runner_id, name);

--
-- Name: idx_snapshots_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_snapshots_tenant ON public.snapshots USING btree (tenant_id);

--
-- Name: idx_streams_device; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_device ON public.streams USING btree (device_serial) WHERE (type = 'android'::text);

--
-- Name: idx_streams_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_expires ON public.streams USING btree (expires_at) WHERE ((expires_at IS NOT NULL) AND (state <> ALL (ARRAY['stopped'::text, 'error'::text])));

--
-- Name: idx_streams_runner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_runner ON public.streams USING btree (runner_id);

--
-- Name: idx_streams_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_session ON public.streams USING btree (session_id);

--
-- Name: idx_streams_session_type_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_session_type_active ON public.streams USING btree (session_id, type) WHERE (state <> ALL (ARRAY['stopped'::text, 'error'::text]));

--
-- Name: idx_streams_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_state ON public.streams USING btree (state);

--
-- Name: idx_streams_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_streams_tenant ON public.streams USING btree (tenant_id);

--
-- Name: idx_task_runs_runner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_runs_runner ON public.task_runs USING btree (runner_id);

--
-- Name: idx_task_runs_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_runs_status ON public.task_runs USING btree (status);

--
-- Name: idx_task_runs_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_runs_task ON public.task_runs USING btree (task_id);

--
-- Name: idx_task_runs_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_runs_tenant ON public.task_runs USING btree (tenant_id);

--
-- Name: idx_tasks_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_pending ON public.tasks USING btree (session_id, status) WHERE (status = 'pending'::text);

--
-- Name: idx_tasks_redispatch; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_redispatch ON public.tasks USING btree (next_dispatch_after) WHERE ((status = 'pending'::text) AND (dispatch_parked_reason IS NULL));

--
-- Name: idx_tasks_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_session ON public.tasks USING btree (session_id);

--
-- Name: idx_tasks_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_status ON public.tasks USING btree (status);

--
-- Name: idx_tasks_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant ON public.tasks USING btree (tenant_id);

--
-- Name: idx_tunnels_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tunnels_active ON public.tunnels USING btree (session_id) WHERE (closed_at IS NULL);

--
-- Name: idx_tunnels_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tunnels_session ON public.tunnels USING btree (session_id);

--
-- Name: idx_tunnels_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tunnels_tenant ON public.tunnels USING btree (tenant_id);

--
-- Name: idx_webhook_events_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_created ON public.webhook_events USING btree (webhook_id, created_at DESC);

--
-- Name: idx_webhook_events_delivered; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_delivered ON public.webhook_events USING btree (delivered_at) WHERE (status = 'delivered'::text);

--
-- Name: idx_webhook_events_retry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_retry ON public.webhook_events USING btree (next_retry_at) WHERE ((status = ANY (ARRAY['pending'::text, 'failed'::text])) AND (next_retry_at IS NOT NULL));

--
-- Name: idx_webhook_events_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_status ON public.webhook_events USING btree (status);

--
-- Name: idx_webhook_events_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_tenant ON public.webhook_events USING btree (tenant_id);

--
-- Name: idx_webhook_events_webhook; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_webhook ON public.webhook_events USING btree (webhook_id);

--
-- Name: idx_webhooks_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhooks_active ON public.webhooks USING btree (tenant_id) WHERE (is_active = true);

--
-- Name: idx_webhooks_events; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhooks_events ON public.webhooks USING gin (events);

--
-- Name: idx_webhooks_name_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_webhooks_name_unique ON public.webhooks USING btree (COALESCE(tenant_id, ''::text), name);

--
-- Name: idx_webhooks_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhooks_tenant ON public.webhooks USING btree (tenant_id);

--
-- Name: idx_workspaces_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspaces_expires ON public.workspaces USING btree (expires_at) WHERE (deleted_at IS NULL);

--
-- Name: idx_workspaces_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspaces_tenant ON public.workspaces USING btree (tenant_id);

--
-- Name: logs_default_run_id_sequence_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX logs_default_run_id_sequence_created_at_idx ON public.logs_default USING btree (run_id, sequence, created_at);

--
-- Name: logs_default_session_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX logs_default_session_id_created_at_idx ON public.logs_default USING btree (session_id, created_at);

--
-- Name: logs_default_task_id_sequence_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX logs_default_task_id_sequence_idx ON public.logs_default USING btree (task_id, sequence);

--
-- Name: logs_default_tenant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX logs_default_tenant_id_created_at_idx ON public.logs_default USING btree (tenant_id, created_at);

--
-- Name: logs_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.logs_pkey ATTACH PARTITION public.logs_default_pkey;

--
-- Name: logs_default_run_id_sequence_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_logs_run_seq_unique ATTACH PARTITION public.logs_default_run_id_sequence_created_at_idx;

--
-- Name: logs_default_session_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_logs_session ATTACH PARTITION public.logs_default_session_id_created_at_idx;

--
-- Name: logs_default_task_id_sequence_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_logs_task_seq ATTACH PARTITION public.logs_default_task_id_sequence_idx;

--
-- Name: logs_default_tenant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_logs_tenant ATTACH PARTITION public.logs_default_tenant_id_created_at_idx;

--
-- Name: manifests manifests_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.manifests
    ADD CONSTRAINT manifests_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.manifests(id) ON DELETE SET NULL;

--
-- Name: manifests manifests_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.manifests
    ADD CONSTRAINT manifests_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

--
-- Name: permission_requests permission_requests_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.permission_requests
    ADD CONSTRAINT permission_requests_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.task_runs(id) ON DELETE CASCADE;

--
-- Name: permission_requests permission_requests_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.permission_requests
    ADD CONSTRAINT permission_requests_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON DELETE CASCADE;

--
-- Name: permission_requests permission_requests_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.permission_requests
    ADD CONSTRAINT permission_requests_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: profiles profiles_provider_config_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.profiles
    ADD CONSTRAINT profiles_provider_config_id_fkey FOREIGN KEY (provider_config_id) REFERENCES public.provider_configs(id) ON DELETE SET NULL;

--
-- Name: runner_tokens runner_tokens_runner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runner_tokens
    ADD CONSTRAINT runner_tokens_runner_id_fkey FOREIGN KEY (runner_id) REFERENCES public.runners(id) ON DELETE CASCADE;

--
-- Name: runners runners_connected_replica_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runners
    ADD CONSTRAINT runners_connected_replica_id_fkey FOREIGN KEY (connected_replica_id) REFERENCES public.server_replicas(id) ON DELETE SET NULL;

--
-- Name: runners runners_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runners
    ADD CONSTRAINT runners_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE SET NULL;

--
-- Name: runners runners_provider_config_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runners
    ADD CONSTRAINT runners_provider_config_id_fkey FOREIGN KEY (provider_config_id) REFERENCES public.provider_configs(id) ON DELETE SET NULL;

--
-- Name: scheduled_tasks scheduled_tasks_last_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_tasks
    ADD CONSTRAINT scheduled_tasks_last_task_id_fkey FOREIGN KEY (last_task_id) REFERENCES public.tasks(id) ON DELETE SET NULL;

--
-- Name: scheduled_tasks scheduled_tasks_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_tasks
    ADD CONSTRAINT scheduled_tasks_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON DELETE CASCADE;

--
-- Name: sessions sessions_agent_config_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_agent_config_id_fkey FOREIGN KEY (agent_config_id) REFERENCES public.agent_configs(id) ON DELETE SET NULL;

--
-- Name: sessions sessions_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE SET NULL;

--
-- Name: sessions sessions_runner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_runner_id_fkey FOREIGN KEY (runner_id) REFERENCES public.runners(id) ON DELETE SET NULL;

--
-- Name: sessions sessions_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE RESTRICT;

--
-- Name: snapshots snapshots_runner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.snapshots
    ADD CONSTRAINT snapshots_runner_id_fkey FOREIGN KEY (runner_id) REFERENCES public.runners(id) ON DELETE CASCADE;

--
-- Name: streams streams_runner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.streams
    ADD CONSTRAINT streams_runner_id_fkey FOREIGN KEY (runner_id) REFERENCES public.runners(id) ON DELETE SET NULL;

--
-- Name: streams streams_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.streams
    ADD CONSTRAINT streams_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON DELETE CASCADE;

--
-- Name: task_runs task_runs_runner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_runs
    ADD CONSTRAINT task_runs_runner_id_fkey FOREIGN KEY (runner_id) REFERENCES public.runners(id) ON DELETE SET NULL;

--
-- Name: task_runs task_runs_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_runs
    ADD CONSTRAINT task_runs_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: tasks tasks_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON DELETE CASCADE;

--
-- Name: tunnels tunnels_runner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tunnels
    ADD CONSTRAINT tunnels_runner_id_fkey FOREIGN KEY (runner_id) REFERENCES public.runners(id) ON DELETE SET NULL;

--
-- Name: tunnels tunnels_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tunnels
    ADD CONSTRAINT tunnels_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON DELETE CASCADE;

--
-- Name: webhook_events webhook_events_webhook_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_events
    ADD CONSTRAINT webhook_events_webhook_id_fkey FOREIGN KEY (webhook_id) REFERENCES public.webhooks(id) ON DELETE CASCADE;

--
-- Name: action_logs; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.action_logs ENABLE ROW LEVEL SECURITY;

--
-- Name: agent_configs; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.agent_configs ENABLE ROW LEVEL SECURITY;

--
-- Name: api_keys; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.api_keys ENABLE ROW LEVEL SECURITY;

--
-- Name: chunks; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.chunks ENABLE ROW LEVEL SECURITY;

--
-- Name: data_keys; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.data_keys ENABLE ROW LEVEL SECURITY;

--
-- Name: log_archives; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.log_archives ENABLE ROW LEVEL SECURITY;

--
-- Name: logs; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.logs ENABLE ROW LEVEL SECURITY;

--
-- Name: logs_default; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.logs_default ENABLE ROW LEVEL SECURITY;

--
-- Name: manifests; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.manifests ENABLE ROW LEVEL SECURITY;

--
-- Name: permission_requests; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.permission_requests ENABLE ROW LEVEL SECURITY;

--
-- Name: profiles; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY;

--
-- Name: provider_configs; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.provider_configs ENABLE ROW LEVEL SECURITY;

--
-- Name: runner_tokens; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.runner_tokens ENABLE ROW LEVEL SECURITY;

--
-- Name: runners; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.runners ENABLE ROW LEVEL SECURITY;

--
-- Name: scheduled_tasks; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.scheduled_tasks ENABLE ROW LEVEL SECURITY;

--
-- Name: sessions; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.sessions ENABLE ROW LEVEL SECURITY;

--
-- Name: snapshots; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.snapshots ENABLE ROW LEVEL SECURITY;

--
-- Name: streams; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.streams ENABLE ROW LEVEL SECURITY;

--
-- Name: task_runs; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.task_runs ENABLE ROW LEVEL SECURITY;

--
-- Name: tasks; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.tasks ENABLE ROW LEVEL SECURITY;

--
-- Name: action_logs tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.action_logs USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: agent_configs tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.agent_configs USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: api_keys tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.api_keys USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: chunks tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.chunks USING (((current_setting('app.system'::text, true) = 'on'::text) OR (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NULL) OR (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text)))) WITH CHECK (((current_setting('app.system'::text, true) = 'on'::text) OR (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NULL) OR (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))));

--
-- Name: data_keys tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.data_keys USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: log_archives tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.log_archives USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: logs tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.logs USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: logs_default tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.logs_default USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: manifests tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.manifests USING (((current_setting('app.system'::text, true) = 'on'::text) OR (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NULL) OR (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text)))) WITH CHECK (((current_setting('app.system'::text, true) = 'on'::text) OR (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NULL) OR (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))));

--
-- Name: permission_requests tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.permission_requests USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: profiles tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.profiles USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: provider_configs tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.provider_configs USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: runner_tokens tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.runner_tokens USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: runners tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.runners USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: scheduled_tasks tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.scheduled_tasks USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: sessions tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.sessions USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: snapshots tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.snapshots USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: streams tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.streams USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: task_runs tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.task_runs USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: tasks tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.tasks USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: tunnels tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.tunnels USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: webhook_events tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.webhook_events USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: webhooks tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.webhooks USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: workspaces tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_isolation ON public.workspaces USING (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END) WITH CHECK (
CASE
    WHEN (current_setting('app.system'::text, true) = 'on'::text) THEN true
    WHEN (NULLIF(current_setting('app.tenant_id'::text, true), ''::text) IS NOT NULL) THEN (tenant_id = NULLIF(current_setting('app.tenant_id'::text, true), ''::text))
    WHEN (COALESCE(current_setting('app.multi_tenant'::text, true), 'off'::text) = 'on'::text) THEN false
    ELSE (tenant_id IS NULL)
END);

--
-- Name: tunnels; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.tunnels ENABLE ROW LEVEL SECURITY;

--
-- Name: webhook_events; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.webhook_events ENABLE ROW LEVEL SECURITY;

--
-- Name: webhooks; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.webhooks ENABLE ROW LEVEL SECURITY;

--
-- Name: workspaces; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.workspaces ENABLE ROW LEVEL SECURITY;

--
-- PostgreSQL database dump complete
--

