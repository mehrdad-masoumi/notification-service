-- +migrate Up

CREATE TABLE IF NOT EXISTS notification_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id UUID NOT NULL,
    delivery_id UUID NOT NULL REFERENCES notification_deliveries(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    routing_key VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'publishing', 'published', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    locked_by VARCHAR(100),
    published_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_outbox_claim
    ON notification_outbox (status, available_at, created_at)
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS idx_notification_outbox_locked
    ON notification_outbox (locked_at)
    WHERE status = 'publishing';

CREATE INDEX IF NOT EXISTS idx_notification_outbox_delivery
    ON notification_outbox (delivery_id);

CREATE INDEX IF NOT EXISTS idx_notification_outbox_published_at
    ON notification_outbox (published_at)
    WHERE status = 'published';

-- +migrate Down

DROP TABLE IF EXISTS notification_outbox;
