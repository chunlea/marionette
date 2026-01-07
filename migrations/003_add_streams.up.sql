-- Add streams table for desktop/browser/mobile streaming
-- This extends the tunnel concept for WebRTC-based streaming

CREATE TABLE streams (
    id TEXT PRIMARY KEY,  -- strm_xxx
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    runner_id TEXT REFERENCES runners(id) ON DELETE SET NULL,
    tenant_id TEXT,

    -- Stream type: "desktop", "browser", "ios", "android"
    type TEXT NOT NULL,

    -- Stream state: "pending", "starting", "active", "paused", "stopping", "stopped", "error"
    state TEXT NOT NULL DEFAULT 'pending',

    -- WebRTC signaling
    signaling_url TEXT,
    ice_servers JSONB NOT NULL DEFAULT '[]',

    -- Stream configuration
    resolution_width INT,
    resolution_height INT,
    frame_rate INT,
    bitrate INT,
    audio_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    input_enabled BOOLEAN NOT NULL DEFAULT FALSE,

    -- Provider info
    provider_name TEXT NOT NULL,
    provider_stream_id TEXT,

    -- Error info
    error TEXT,

    -- Additional metadata
    metadata JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    -- Constraints
    CONSTRAINT valid_stream_type CHECK (
        type IN ('desktop', 'browser', 'ios', 'android')
    ),
    CONSTRAINT valid_stream_state CHECK (
        state IN ('pending', 'starting', 'active', 'paused', 'stopping', 'stopped', 'error')
    )
);

-- Indexes for common query patterns
CREATE INDEX idx_streams_session ON streams(session_id);
CREATE INDEX idx_streams_runner ON streams(runner_id);
CREATE INDEX idx_streams_tenant ON streams(tenant_id);
CREATE INDEX idx_streams_state ON streams(state);
CREATE INDEX idx_streams_type ON streams(type);

-- Active streams by session (most common query)
CREATE INDEX idx_streams_session_active ON streams(session_id, type)
    WHERE state NOT IN ('stopped', 'error');

-- Cleanup expired streams
CREATE INDEX idx_streams_expires ON streams(expires_at)
    WHERE expires_at IS NOT NULL AND state NOT IN ('stopped', 'error');
