package notificationrepo

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	notificationdto "notification-service/internal/notification/dto"
)

// PromoteScheduledBatch atomically claims up to limit due ('scheduled' and
// scheduled_at <= NOW()) notifications, flips them to 'pending', and writes
// outbox rows for their existing deliveries — all within a single
// transaction using SELECT ... FOR UPDATE SKIP LOCKED so multiple
// scheduler replicas never double-publish the same notification.
//
// It returns the number of notifications promoted.
func (r *Repository) PromoteScheduledBatch(ctx context.Context, limit int) (int, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var ids []uuid.UUID
	err = tx.SelectContext(ctx, &ids, `
		SELECT id FROM notifications
		WHERE status = 'scheduled' AND scheduled_at IS NOT NULL AND scheduled_at <= NOW()
		ORDER BY scheduled_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, tx.Commit()
	}

	promoted := 0
	for _, id := range ids {
		var priority string
		if err := tx.GetContext(ctx, &priority, `
			UPDATE notifications SET status = 'pending', updated_at = NOW() WHERE id = $1
			RETURNING priority`, id); err != nil {
			return promoted, err
		}

		var deliveries []deliveryRow
		if err := tx.SelectContext(ctx, &deliveries, `
			SELECT * FROM notification_deliveries WHERE notification_id = $1`, id); err != nil {
			return promoted, err
		}

		routingKey := "notification." + priority
		for _, d := range deliveries {
			job := notificationdto.QueueJob{
				NotificationID: id.String(),
				DeliveryID:     d.ID.String(),
				Channel:        d.Channel,
				Attempt:        0,
			}
			payload, err := json.Marshal(job)
			if err != nil {
				return promoted, err
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO notification_outbox (
					id, aggregate_id, delivery_id, event_type, routing_key, payload, status, available_at
				) VALUES ($1,$2,$3,'notification.delivery.created',$4,$5,'pending',NOW())`,
				uuid.New(), id, d.ID, routingKey, payload,
			)
			if err != nil {
				return promoted, err
			}
		}
		promoted++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return promoted, nil
}
