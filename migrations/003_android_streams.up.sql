-- Android streams for screen mirroring
CREATE TABLE android_streams (
    id TEXT PRIMARY KEY,  -- astr_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    device_serial TEXT NOT NULL,

    -- Stream state: starting, active, paused, closing, closed, failed
    state TEXT NOT NULL DEFAULT 'starting',
    error_message TEXT,

    -- Stream options (stored as JSONB for flexibility)
    options JSONB NOT NULL DEFAULT '{}',

    -- Video info (populated after stream starts)
    width INT,
    height INT,
    video_codec TEXT,
    audio_codec TEXT,

    -- Local port for scrcpy connection (on the agent)
    local_port INT,

    -- Tenant isolation
    tenant_id TEXT,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_stream_state CHECK (
        state IN ('starting', 'active', 'paused', 'closing', 'closed', 'failed')
    )
);

CREATE INDEX idx_android_streams_session ON android_streams(session_id);
CREATE INDEX idx_android_streams_runner ON android_streams(runner_id);
CREATE INDEX idx_android_streams_tenant ON android_streams(tenant_id);
CREATE INDEX idx_android_streams_active ON android_streams(session_id, state)
    WHERE state IN ('starting', 'active', 'paused');
