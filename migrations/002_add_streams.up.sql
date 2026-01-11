-- Streams table for unified streaming infrastructure
-- Supports desktop, browser, iOS, and Android streaming types

CREATE TABLE streams (
    id TEXT PRIMARY KEY,                    -- strm_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    tenant_id TEXT,

    type TEXT NOT NULL,                     -- desktop, browser, ios, android
    state TEXT NOT NULL DEFAULT 'pending',  -- pending, starting, active, paused, stopping, stopped, error

    signaling_url TEXT,
    ice_servers JSONB NOT NULL DEFAULT '[]',

    resolution_width INT,
    resolution_height INT,
    frame_rate INT,
    bitrate INT,
    video_codec TEXT,
    audio_codec TEXT,
    audio_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    input_enabled BOOLEAN NOT NULL DEFAULT FALSE,

    provider_name TEXT NOT NULL,
    provider_stream_id TEXT,

    error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    CONSTRAINT valid_stream_type CHECK (
        type IN ('desktop', 'browser', 'ios', 'android')
    ),
    CONSTRAINT valid_stream_state CHECK (
        state IN ('pending', 'starting', 'active', 'paused', 'stopping', 'stopped', 'error')
    )
);

-- Index for looking up streams by session
CREATE INDEX idx_streams_session ON streams(session_id);

-- Index for looking up streams by runner
CREATE INDEX idx_streams_runner ON streams(runner_id);

-- Index for tenant isolation
CREATE INDEX idx_streams_tenant ON streams(tenant_id);

-- Index for filtering by state
CREATE INDEX idx_streams_state ON streams(state);

-- Partial index for finding active streams by session and type
CREATE INDEX idx_streams_session_type_active ON streams(session_id, type)
    WHERE state NOT IN ('stopped', 'error');

-- Partial index for cleanup of expired streams
CREATE INDEX idx_streams_expires ON streams(expires_at)
    WHERE expires_at IS NOT NULL AND state NOT IN ('stopped', 'error');
