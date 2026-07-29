-- +migrate Up

-- Optional user_id: callers may send email/sms/push without a platform user.
ALTER TABLE notifications
    ALTER COLUMN user_id DROP NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS source_service TEXT,
    ADD COLUMN IF NOT EXISTS message_id TEXT,
    ADD COLUMN IF NOT EXISTS correlation_id TEXT,
    ADD COLUMN IF NOT EXISTS trace_id TEXT,
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS device_tokens JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS display_name TEXT;

CREATE INDEX IF NOT EXISTS idx_notifications_message_id
    ON notifications (message_id)
    WHERE message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notifications_source_service
    ON notifications (source_service)
    WHERE source_service IS NOT NULL;

-- +migrate Down

DROP INDEX IF EXISTS idx_notifications_source_service;
DROP INDEX IF EXISTS idx_notifications_message_id;

ALTER TABLE notifications
    DROP COLUMN IF EXISTS display_name,
    DROP COLUMN IF EXISTS device_tokens,
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS trace_id,
    DROP COLUMN IF EXISTS correlation_id,
    DROP COLUMN IF EXISTS message_id,
    DROP COLUMN IF EXISTS source_service;

-- Re-adding NOT NULL may fail if null rows exist; only apply when safe.
UPDATE notifications SET user_id = '00000000-0000-0000-0000-000000000000' WHERE user_id IS NULL;
ALTER TABLE notifications
    ALTER COLUMN user_id SET NOT NULL;
