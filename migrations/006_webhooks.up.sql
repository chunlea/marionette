-- Webhooks for external system integration
--
-- ID Format: whk_{base62_timestamp}{nanoid} for webhooks
--            whev_{base62_timestamp}{nanoid} for webhook events
--
-- Webhooks allow users to subscribe to system events and receive
-- HTTP callbacks when those events occur.

--------------------------------------------------------------------------------
-- Webhook Configuration
--------------------------------------------------------------------------------

CREATE TABLE webhooks (
    id TEXT PRIMARY KEY,  -- whk_xxx
    name TEXT NOT NULL,
    url TEXT NOT NULL,

    -- Event subscription (supports wildcards like "task.*")
    events TEXT[] NOT NULL DEFAULT '{}',

    -- Secret for HMAC-SHA256 signature (stored as hash)
    -- The plain secret is shown once on creation, then only hash stored
    secret_hash TEXT NOT NULL,
    secret_prefix TEXT NOT NULL,  -- First 8 chars for identification

    -- Configuration
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    max_retries INT NOT NULL DEFAULT 3,
    retry_delay_seconds INT NOT NULL DEFAULT 60,
    timeout_seconds INT NOT NULL DEFAULT 30,

    -- Custom headers to include with requests (JSON object)
    headers JSONB NOT NULL DEFAULT '{}',

    -- Tenant isolation
    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhooks_tenant ON webhooks(tenant_id);
CREATE INDEX idx_webhooks_active ON webhooks(tenant_id) WHERE is_active = TRUE;
CREATE INDEX idx_webhooks_events ON webhooks USING GIN (events);
-- COALESCE handles NULL: treats NULL tenant_id as '' for uniqueness
CREATE UNIQUE INDEX idx_webhooks_name_unique ON webhooks(COALESCE(tenant_id, ''), name);

--------------------------------------------------------------------------------
-- Webhook Events (Delivery Queue)
--------------------------------------------------------------------------------

-- Tracks individual webhook delivery attempts
CREATE TABLE webhook_events (
    id TEXT PRIMARY KEY,  -- whev_xxx
    webhook_id TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,

    -- Event details
    event_type TEXT NOT NULL,  -- e.g., "session.created", "task.completed"
    payload JSONB NOT NULL,

    -- Delivery status
    -- pending: waiting for delivery
    -- delivered: successfully delivered (2xx response)
    -- failed: delivery failed, will retry
    -- exhausted: max retries exceeded
    -- canceled: webhook was deleted or disabled
    status TEXT NOT NULL DEFAULT 'pending',

    -- Retry tracking
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    last_status_code INT,
    next_retry_at TIMESTAMPTZ,

    -- Delivery tracking
    delivered_at TIMESTAMPTZ,

    -- Tenant isolation (denormalized for query performance)
    tenant_id TEXT,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_webhook_event_status CHECK (
        status IN ('pending', 'delivered', 'failed', 'exhausted', 'canceled')
    )
);

CREATE INDEX idx_webhook_events_webhook ON webhook_events(webhook_id);
CREATE INDEX idx_webhook_events_status ON webhook_events(status);
CREATE INDEX idx_webhook_events_tenant ON webhook_events(tenant_id);
-- For retry job: find pending/failed events ready for retry
CREATE INDEX idx_webhook_events_retry ON webhook_events(next_retry_at)
    WHERE status IN ('pending', 'failed') AND next_retry_at IS NOT NULL;
-- For cleanup: find old delivered events
CREATE INDEX idx_webhook_events_delivered ON webhook_events(delivered_at)
    WHERE status = 'delivered';
-- For listing events by creation time
CREATE INDEX idx_webhook_events_created ON webhook_events(webhook_id, created_at DESC);
