package notificationrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"notification-service/internal/notification/entity"
	"notification-service/pkg/sharederrors"
)

func (r *Repository) ListDeliveries(ctx context.Context, notificationID uuid.UUID) ([]entity.Delivery, error) {
	var rows []deliveryRow
	err := r.db.SelectContext(ctx, &rows, `
		SELECT * FROM notification_deliveries WHERE notification_id = $1 ORDER BY created_at ASC`, notificationID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.Delivery, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDelivery(row))
	}
	return out, nil
}

func (r *Repository) GetDelivery(ctx context.Context, id uuid.UUID) (entity.Delivery, error) {
	var row deliveryRow
	err := r.db.GetContext(ctx, &row, `SELECT * FROM notification_deliveries WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Delivery{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.Delivery{}, err
	}
	return mapDelivery(row), nil
}

func (r *Repository) UpdateDelivery(ctx context.Context, d entity.Delivery) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notification_deliveries
		SET provider = $2, status = $3, attempts = $4, error = $5, sent_at = $6, delivered_at = $7, updated_at = NOW()
		WHERE id = $1`,
		d.ID, d.Provider, d.Status, d.Attempts, d.Error, d.SentAt, d.DeliveredAt,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sharederrors.ErrNotFound
	}
	return nil
}

// ClaimDelivery atomically transitions a delivery from pending/failed to
// sending and bumps its attempt counter. If the delivery is already
// sending/sent/delivered/permanent_failed (claimed by another worker or
// already terminal), it returns sharederrors.ErrNotFound so callers can
// skip the job without double-sending.
func (r *Repository) ClaimDelivery(ctx context.Context, id uuid.UUID) (entity.Delivery, error) {
	var row deliveryRow
	err := r.db.GetContext(ctx, &row, `
		UPDATE notification_deliveries
		SET status = 'sending', attempts = attempts + 1, updated_at = NOW()
		WHERE id = $1 AND status IN ('pending', 'failed')
		RETURNING *`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Delivery{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.Delivery{}, err
	}
	return mapDelivery(row), nil
}

// RecoverStuckSending resets deliveries stuck in 'sending' for longer than
// timeout back to 'failed' so they become eligible for retry/claim again.
func (r *Repository) RecoverStuckSending(ctx context.Context, timeout time.Duration) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notification_deliveries
		SET status = 'failed', error = COALESCE(error, 'recovered from stuck sending'), updated_at = NOW()
		WHERE status = 'sending' AND updated_at < NOW() - ($1 * INTERVAL '1 second')`,
		int64(timeout.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
