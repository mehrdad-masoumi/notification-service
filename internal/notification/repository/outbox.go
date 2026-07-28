package notificationrepo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"notification-service/internal/notification/entity"
)

type outboxRow struct {
	ID          uuid.UUID       `db:"id"`
	AggregateID uuid.UUID       `db:"aggregate_id"`
	DeliveryID  uuid.UUID       `db:"delivery_id"`
	EventType   string          `db:"event_type"`
	RoutingKey  string          `db:"routing_key"`
	Payload     json.RawMessage `db:"payload"`
	Status      string          `db:"status"`
	Attempts    int             `db:"attempts"`
	AvailableAt time.Time       `db:"available_at"`
	LockedAt    *time.Time      `db:"locked_at"`
	LockedBy    *string         `db:"locked_by"`
	PublishedAt *time.Time      `db:"published_at"`
	LastError   *string         `db:"last_error"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

func mapOutbox(row outboxRow) entity.OutboxEvent {
	return entity.OutboxEvent{
		ID:          row.ID,
		AggregateID: row.AggregateID,
		DeliveryID:  row.DeliveryID,
		EventType:   row.EventType,
		RoutingKey:  row.RoutingKey,
		Payload:     row.Payload,
		Status:      entity.OutboxStatus(row.Status),
		Attempts:    row.Attempts,
		AvailableAt: row.AvailableAt,
		LockedAt:    row.LockedAt,
		LockedBy:    row.LockedBy,
		PublishedAt: row.PublishedAt,
		LastError:   row.LastError,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

// InsertOutboxEvents writes outbox rows outside of the notification-create
// transaction (used by the scheduler when promoting due notifications, and
// by EnqueueExisting/backfill paths).
func (r *Repository) InsertOutboxEvents(ctx context.Context, events []entity.OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, e := range events {
		if e.ID == uuid.Nil {
			e.ID = uuid.New()
		}
		if e.Status == "" {
			e.Status = entity.OutboxPending
		}
		if e.AvailableAt.IsZero() {
			e.AvailableAt = time.Now().UTC()
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notification_outbox (
				id, aggregate_id, delivery_id, event_type, routing_key, payload, status, available_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			e.ID, e.AggregateID, e.DeliveryID, e.EventType, e.RoutingKey, e.Payload, e.Status, e.AvailableAt,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// HasOutboxForDelivery reports whether an outbox row already exists for a
// delivery, used to avoid duplicate outbox writes on re-enqueue.
func (r *Repository) HasOutboxForDelivery(ctx context.Context, deliveryID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM notification_outbox WHERE delivery_id = $1)`, deliveryID)
	return exists, err
}

// ClaimOutboxBatch locks up to limit publishable rows (pending or failed
// and due) using FOR UPDATE SKIP LOCKED so multiple publisher replicas can
// run concurrently without contention.
func (r *Repository) ClaimOutboxBatch(ctx context.Context, limit int, lockedBy string) ([]entity.OutboxEvent, error) {
	var rows []outboxRow
	err := r.db.SelectContext(ctx, &rows, `
		UPDATE notification_outbox
		SET status = 'publishing', locked_at = NOW(), locked_by = $2, updated_at = NOW()
		WHERE id IN (
			SELECT id FROM notification_outbox
			WHERE status IN ('pending', 'failed') AND available_at <= NOW()
			ORDER BY available_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *`, limit, lockedBy)
	if err != nil {
		return nil, err
	}
	out := make([]entity.OutboxEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapOutbox(row))
	}
	return out, nil
}

func (r *Repository) MarkOutboxPublished(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notification_outbox
		SET status = 'published', published_at = NOW(), locked_at = NULL, locked_by = NULL, updated_at = NOW()
		WHERE id = $1`, id)
	return err
}

func (r *Repository) MarkOutboxFailed(ctx context.Context, id uuid.UUID, errMsg string, availableAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notification_outbox
		SET status = 'failed', last_error = $2, available_at = $3, locked_at = NULL, locked_by = NULL, updated_at = NOW()
		WHERE id = $1`, id, errMsg, availableAt)
	return err
}

// RecoverStuckOutboxLocks resets rows stuck in 'publishing' for longer than
// lockTimeout back to 'pending' (e.g. publisher crashed mid-batch).
func (r *Repository) RecoverStuckOutboxLocks(ctx context.Context, lockTimeout time.Duration) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notification_outbox
		SET status = 'pending', locked_at = NULL, locked_by = NULL, updated_at = NOW()
		WHERE status = 'publishing' AND locked_at < NOW() - ($1 * INTERVAL '1 second')`,
		int64(lockTimeout.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *Repository) CountPendingOutbox(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM notification_outbox WHERE status IN ('pending', 'failed')`)
	return count, err
}

// OldestPendingOutboxAge returns how long the oldest unpublished outbox row
// has been waiting, or zero if there are none.
func (r *Repository) OldestPendingOutboxAge(ctx context.Context) (time.Duration, error) {
	var seconds float64
	err := r.db.GetContext(ctx, &seconds, `
		SELECT COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(created_at))), 0)
		FROM notification_outbox WHERE status IN ('pending', 'failed', 'publishing')`)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// CleanupPublishedOutbox deletes published outbox rows older than
// olderThan, keeping the table bounded.
func (r *Repository) CleanupPublishedOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM notification_outbox
		WHERE status = 'published' AND published_at < NOW() - ($1 * INTERVAL '1 second')`,
		int64(olderThan.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
