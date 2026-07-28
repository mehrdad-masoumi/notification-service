package notificationrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"notification-service/internal/notification/entity"
	"notification-service/pkg/sharederrors"
)

type batchJobRow struct {
	ID                  uuid.UUID       `db:"id"`
	Status              string          `db:"status"`
	TemplateCode        string          `db:"template_code"`
	Locale              string          `db:"locale"`
	Channels            json.RawMessage `db:"channels"`
	Priority            string          `db:"priority"`
	Variables           json.RawMessage `db:"variables"`
	ActionURL           *string         `db:"action_url"`
	ScheduledAt         *time.Time      `db:"scheduled_at"`
	CreatedBy           *uuid.UUID      `db:"created_by"`
	TotalRecipients     int             `db:"total_recipients"`
	ProcessedRecipients int             `db:"processed_recipients"`
	FailedRecipients    int             `db:"failed_recipients"`
	LastError           *string         `db:"last_error"`
	CreatedAt           time.Time       `db:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
}

func mapBatchJob(row batchJobRow) entity.BatchJob {
	var raw []string
	_ = json.Unmarshal(row.Channels, &raw)
	channels := make([]entity.Channel, 0, len(raw))
	for _, c := range raw {
		channels = append(channels, entity.Channel(c))
	}
	return entity.BatchJob{
		ID:                  row.ID,
		Status:              entity.BatchJobStatus(row.Status),
		TemplateCode:        row.TemplateCode,
		Locale:              row.Locale,
		Channels:            channels,
		Priority:            entity.Priority(row.Priority),
		Variables:           row.Variables,
		ActionURL:           row.ActionURL,
		ScheduledAt:         row.ScheduledAt,
		CreatedBy:           row.CreatedBy,
		TotalRecipients:     row.TotalRecipients,
		ProcessedRecipients: row.ProcessedRecipients,
		FailedRecipients:    row.FailedRecipients,
		LastError:           row.LastError,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

// CreateBatchJob creates an async fan-out job together with its recipient
// rows in one transaction. Used when an admin batch targets more
// recipients than Batch.SyncMaxRecipients.
func (r *Repository) CreateBatchJob(ctx context.Context, job entity.BatchJob, userIDs []uuid.UUID) (entity.BatchJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.Variables == nil {
		job.Variables = json.RawMessage(`{}`)
	}
	channelsJSON, err := json.Marshal(job.Channels)
	if err != nil {
		return entity.BatchJob{}, err
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return entity.BatchJob{}, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO notification_batch_jobs (
			id, status, template_code, locale, channels, priority, variables,
			action_url, scheduled_at, created_by, total_recipients
		) VALUES ($1,'pending',$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		job.ID, job.TemplateCode, job.Locale, channelsJSON, job.Priority, job.Variables,
		job.ActionURL, job.ScheduledAt, job.CreatedBy, len(userIDs),
	)
	if err != nil {
		return entity.BatchJob{}, err
	}

	for _, uid := range userIDs {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notification_batch_job_recipients (id, job_id, user_id, status)
			VALUES ($1,$2,$3,'pending')
			ON CONFLICT (job_id, user_id) DO NOTHING`, uuid.New(), job.ID, uid)
		if err != nil {
			return entity.BatchJob{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return entity.BatchJob{}, err
	}
	return r.GetBatchJob(ctx, job.ID)
}

func (r *Repository) GetBatchJob(ctx context.Context, id uuid.UUID) (entity.BatchJob, error) {
	var row batchJobRow
	err := r.db.GetContext(ctx, &row, `SELECT * FROM notification_batch_jobs WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.BatchJob{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.BatchJob{}, err
	}
	return mapBatchJob(row), nil
}

type batchRecipientRow struct {
	ID             uuid.UUID  `db:"id"`
	JobID          uuid.UUID  `db:"job_id"`
	UserID         uuid.UUID  `db:"user_id"`
	Status         string     `db:"status"`
	NotificationID *uuid.UUID `db:"notification_id"`
	Error          *string    `db:"error"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

func mapBatchRecipient(row batchRecipientRow) entity.BatchJobRecipient {
	return entity.BatchJobRecipient{
		ID:             row.ID,
		JobID:          row.JobID,
		UserID:         row.UserID,
		Status:         entity.BatchRecipientStatus(row.Status),
		NotificationID: row.NotificationID,
		Error:          row.Error,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

// ClaimRecipients locks up to limit pending recipients of a job for
// processing by this worker, using SKIP LOCKED so multiple batch workers
// may process the same job concurrently.
func (r *Repository) ClaimRecipients(ctx context.Context, jobID uuid.UUID, limit int) ([]entity.BatchJobRecipient, error) {
	var rows []batchRecipientRow
	err := r.db.SelectContext(ctx, &rows, `
		UPDATE notification_batch_job_recipients
		SET status = 'accepted', updated_at = NOW()
		WHERE id IN (
			SELECT id FROM notification_batch_job_recipients
			WHERE job_id = $1 AND status = 'pending'
			ORDER BY created_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *`, jobID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]entity.BatchJobRecipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapBatchRecipient(row))
	}
	return out, nil
}

func (r *Repository) MarkRecipientResult(ctx context.Context, recipientID uuid.UUID, notificationID *uuid.UUID, errMsg *string) error {
	status := "accepted"
	if errMsg != nil {
		status = "failed"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE notification_batch_job_recipients
		SET status = $2, notification_id = $3, error = $4, updated_at = NOW()
		WHERE id = $1`, recipientID, status, notificationID, errMsg)
	return err
}

// UpdateBatchJobProgress increments processed/failed counters and moves the
// job to 'completed' once every recipient has been accounted for.
func (r *Repository) UpdateBatchJobProgress(ctx context.Context, jobID uuid.UUID, processedDelta, failedDelta int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notification_batch_jobs
		SET processed_recipients = processed_recipients + $2,
		    failed_recipients = failed_recipients + $3,
		    status = CASE
		        WHEN processed_recipients + $2 >= total_recipients THEN 'completed'
		        ELSE 'processing'
		    END,
		    updated_at = NOW()
		WHERE id = $1`, jobID, processedDelta, failedDelta)
	return err
}

// ClaimPendingBatchJob picks the oldest pending/processing job with
// remaining recipients so a worker can pick up async batch fan-out.
func (r *Repository) ClaimPendingBatchJob(ctx context.Context) (entity.BatchJob, error) {
	var row batchJobRow
	err := r.db.GetContext(ctx, &row, `
		SELECT * FROM notification_batch_jobs
		WHERE status IN ('pending', 'processing')
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.BatchJob{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.BatchJob{}, err
	}
	return mapBatchJob(row), nil
}
