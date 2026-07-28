-- +migrate Up

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS template_code VARCHAR(100),
    ADD COLUMN IF NOT EXISTS locale VARCHAR(10) NOT NULL DEFAULT 'fa',
    ADD COLUMN IF NOT EXISTS variables JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Allow legacy title/message while template-driven rows may fill them at create time.
ALTER TABLE notifications
    ALTER COLUMN title DROP NOT NULL,
    ALTER COLUMN message DROP NOT NULL;

ALTER TABLE notifications
    ALTER COLUMN title SET DEFAULT '',
    ALTER COLUMN message SET DEFAULT '';

UPDATE notifications SET title = COALESCE(title, ''), message = COALESCE(message, '');

CREATE INDEX IF NOT EXISTS idx_notifications_template_code
    ON notifications (template_code)
    WHERE template_code IS NOT NULL;

-- Visibility: scheduled in-app must not appear before scheduled_at
CREATE INDEX IF NOT EXISTS idx_notifications_user_visible
    ON notifications (user_id, created_at DESC)
    WHERE status <> 'scheduled';

-- Idempotency hash lookup / recovery
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_status_updated
    ON idempotency_keys (status, updated_at);

-- Delivery claim helpers
CREATE INDEX IF NOT EXISTS idx_deliveries_claim
    ON notification_deliveries (status, updated_at)
    WHERE status IN ('pending', 'failed', 'sending');

-- Prefer JSONB containment over text LIKE for in_app channel filters
CREATE INDEX IF NOT EXISTS idx_notifications_channels_gin
    ON notifications USING GIN (channels);

-- +migrate Down

DROP INDEX IF EXISTS idx_notifications_channels_gin;
DROP INDEX IF EXISTS idx_deliveries_claim;
DROP INDEX IF EXISTS idx_idempotency_keys_status_updated;
DROP INDEX IF EXISTS idx_notifications_user_visible;
DROP INDEX IF EXISTS idx_notifications_template_code;

ALTER TABLE notifications
    DROP COLUMN IF EXISTS variables,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS template_code;
