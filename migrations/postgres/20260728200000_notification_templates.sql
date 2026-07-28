-- +migrate Up

CREATE TABLE IF NOT EXISTS notification_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(100) NOT NULL,
    locale VARCHAR(10) NOT NULL DEFAULT 'fa',
    channel VARCHAR(20) NOT NULL
        CHECK (channel IN ('in_app', 'email', 'sms', 'whatsapp', 'push')),
    subject TEXT,
    body TEXT NOT NULL,
    default_priority VARCHAR(16) NOT NULL DEFAULT 'normal'
        CHECK (default_priority IN ('high', 'normal', 'low')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (code, locale, channel)
);

CREATE INDEX IF NOT EXISTS idx_notification_templates_code
    ON notification_templates (code);

CREATE INDEX IF NOT EXISTS idx_notification_templates_lookup
    ON notification_templates (code, locale, channel)
    WHERE enabled = TRUE;

-- Seed common templates (idempotent via ON CONFLICT DO NOTHING)
INSERT INTO notification_templates (code, locale, channel, subject, body, default_priority)
VALUES
    ('withdrawal_approved', 'fa', 'in_app', 'برداشت تایید شد', 'برداشت {{amount}} {{currency}} تایید شد.', 'high'),
    ('withdrawal_approved', 'fa', 'email', 'برداشت تایید شد', '<p>برداشت <b>{{amount}} {{currency}}</b> تایید شد.</p>', 'high'),
    ('login_otp', 'fa', 'sms', NULL, 'کد ورود شما: {{code}}', 'high'),
    ('deposit_confirmed', 'fa', 'in_app', 'واریز تایید شد', 'واریز {{amount}} {{currency}} تایید شد.', 'normal'),
    ('deposit_confirmed', 'fa', 'email', 'واریز تایید شد', '<p>واریز <b>{{amount}} {{currency}}</b> تایید شد.</p>', 'normal')
ON CONFLICT (code, locale, channel) DO NOTHING;

-- +migrate Down

DROP TABLE IF EXISTS notification_templates;
