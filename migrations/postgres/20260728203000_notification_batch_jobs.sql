-- +migrate Up

CREATE TABLE IF NOT EXISTS notification_batch_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    template_code VARCHAR(100) NOT NULL,
    locale VARCHAR(10) NOT NULL DEFAULT 'fa',
    channels JSONB NOT NULL DEFAULT '[]'::jsonb,
    priority VARCHAR(16) NOT NULL DEFAULT 'normal',
    variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    action_url TEXT,
    scheduled_at TIMESTAMPTZ,
    created_by UUID,
    total_recipients INT NOT NULL DEFAULT 0,
    processed_recipients INT NOT NULL DEFAULT 0,
    failed_recipients INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notification_batch_job_recipients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES notification_batch_jobs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'failed')),
    notification_id UUID,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_status
    ON notification_batch_jobs (status, created_at);

CREATE INDEX IF NOT EXISTS idx_batch_job_recipients_claim
    ON notification_batch_job_recipients (job_id, status)
    WHERE status = 'pending';

-- +migrate Down

DROP TABLE IF EXISTS notification_batch_job_recipients;
DROP TABLE IF EXISTS notification_batch_jobs;
