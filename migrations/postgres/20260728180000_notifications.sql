-- +migrate Up

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    batch_id UUID,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    type VARCHAR(64) NOT NULL DEFAULT 'system',
    priority VARCHAR(16) NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('high', 'normal', 'low')),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    action_url TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'queued', 'processing', 'sent', 'partially_failed', 'failed', 'scheduled')),
    read_at TIMESTAMPTZ,
    idempotency_key TEXT,
    channels JSONB NOT NULL DEFAULT '[]'::jsonb,
    scheduled_at TIMESTAMPTZ,
    created_by UUID,
    email TEXT,
    phone TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_notifications_idempotency_key
    ON notifications (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notifications_user_created
    ON notifications (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_user_unread
    ON notifications (user_id)
    WHERE read_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_notifications_scheduled
    ON notifications (status, scheduled_at)
    WHERE status = 'scheduled';

CREATE INDEX IF NOT EXISTS idx_notifications_batch_id
    ON notifications (batch_id)
    WHERE batch_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notifications_created_at
    ON notifications (created_at DESC);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    channel VARCHAR(32) NOT NULL
        CHECK (channel IN ('in_app', 'email', 'sms', 'whatsapp', 'push')),
    provider VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sending', 'sent', 'delivered', 'failed', 'permanent_failed')),
    attempts INT NOT NULL DEFAULT 0,
    error TEXT,
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (notification_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_deliveries_notification_id
    ON notification_deliveries (notification_id);

CREATE INDEX IF NOT EXISTS idx_deliveries_status
    ON notification_deliveries (status);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ide_key TEXT NOT NULL UNIQUE,
    operation TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'processing'
        CHECK (status IN ('processing', 'succeeded', 'failed')),
    response_code INT,
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '24 hours')
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires
    ON idempotency_keys (expires_at);

-- +migrate Down

DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS notification_deliveries;
DROP TABLE IF EXISTS notifications;
