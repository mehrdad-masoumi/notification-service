-- +migrate Up

CREATE INDEX IF NOT EXISTS idx_notifications_direct_recipient_created
    ON notifications (created_at DESC)
    WHERE type = 'template_direct';

-- +migrate Down

DROP INDEX IF EXISTS idx_notifications_direct_recipient_created;
