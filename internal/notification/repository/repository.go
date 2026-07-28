package notificationrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"notification-service/internal/notification/entity"
	"notification-service/pkg/sharederrors"
)

type Repository struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

type notificationRow struct {
	ID             uuid.UUID       `db:"id"`
	UserID         uuid.UUID       `db:"user_id"`
	BatchID        *uuid.UUID      `db:"batch_id"`
	Title          string          `db:"title"`
	Message        string          `db:"message"`
	Type           string          `db:"type"`
	Priority       string          `db:"priority"`
	Payload        json.RawMessage `db:"payload"`
	ActionURL      *string         `db:"action_url"`
	Status         string          `db:"status"`
	ReadAt         *time.Time      `db:"read_at"`
	IdempotencyKey *string         `db:"idempotency_key"`
	Channels       json.RawMessage `db:"channels"`
	ScheduledAt    *time.Time      `db:"scheduled_at"`
	CreatedBy      *uuid.UUID      `db:"created_by"`
	Email          *string         `db:"email"`
	Phone          *string         `db:"phone"`
	TemplateCode   *string         `db:"template_code"`
	Locale         string          `db:"locale"`
	Variables      json.RawMessage `db:"variables"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

type deliveryRow struct {
	ID             uuid.UUID  `db:"id"`
	NotificationID uuid.UUID  `db:"notification_id"`
	Channel        string     `db:"channel"`
	Provider       string     `db:"provider"`
	Status         string     `db:"status"`
	Attempts       int        `db:"attempts"`
	Error          *string    `db:"error"`
	SentAt         *time.Time `db:"sent_at"`
	DeliveredAt    *time.Time `db:"delivered_at"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

func mapNotification(row notificationRow) (entity.Notification, error) {
	var channels []entity.Channel
	if len(row.Channels) > 0 {
		var raw []string
		if err := json.Unmarshal(row.Channels, &raw); err != nil {
			return entity.Notification{}, err
		}
		for _, c := range raw {
			channels = append(channels, entity.Channel(c))
		}
	}
	return entity.Notification{
		ID:             row.ID,
		UserID:         row.UserID,
		BatchID:        row.BatchID,
		Title:          row.Title,
		Message:        row.Message,
		Type:           row.Type,
		Priority:       entity.Priority(row.Priority),
		Payload:        row.Payload,
		ActionURL:      row.ActionURL,
		Status:         entity.NotificationStatus(row.Status),
		ReadAt:         row.ReadAt,
		IdempotencyKey: row.IdempotencyKey,
		Channels:       channels,
		ScheduledAt:    row.ScheduledAt,
		CreatedBy:      row.CreatedBy,
		Email:          row.Email,
		Phone:          row.Phone,
		TemplateCode:   row.TemplateCode,
		Locale:         row.Locale,
		Variables:      row.Variables,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

func mapDelivery(row deliveryRow) entity.Delivery {
	return entity.Delivery{
		ID:             row.ID,
		NotificationID: row.NotificationID,
		Channel:        entity.Channel(row.Channel),
		Provider:       row.Provider,
		Status:         entity.DeliveryStatus(row.Status),
		Attempts:       row.Attempts,
		Error:          row.Error,
		SentAt:         row.SentAt,
		DeliveredAt:    row.DeliveredAt,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

// CreateNotificationBundle inserts a notification, its deliveries, and any
// outbox events in a single transaction (transactional outbox pattern).
// outboxEvents may be empty (e.g. for notifications that are scheduled for
// the future; outbox rows are written later when the scheduler promotes
// them to due).
func (r *Repository) CreateNotificationBundle(
	ctx context.Context,
	n entity.Notification,
	deliveries []entity.Delivery,
	outboxEvents []entity.OutboxEvent,
) (entity.Notification, []entity.Delivery, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return entity.Notification{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	channelsJSON, err := json.Marshal(n.Channels)
	if err != nil {
		return entity.Notification{}, nil, err
	}
	if n.Payload == nil {
		n.Payload = json.RawMessage(`{}`)
	}
	if n.Variables == nil {
		n.Variables = json.RawMessage(`{}`)
	}
	if n.Locale == "" {
		n.Locale = entity.DefaultLocale
	}
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO notifications (
			id, user_id, batch_id, title, message, type, priority, payload, action_url,
			status, idempotency_key, channels, scheduled_at, created_by, email, phone,
			template_code, locale, variables
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19
		)`,
		n.ID, n.UserID, n.BatchID, n.Title, n.Message, n.Type, n.Priority, n.Payload, n.ActionURL,
		n.Status, n.IdempotencyKey, channelsJSON, n.ScheduledAt, n.CreatedBy, n.Email, n.Phone,
		n.TemplateCode, n.Locale, n.Variables,
	)
	if err != nil {
		return entity.Notification{}, nil, fmt.Errorf("insert notification: %w", err)
	}

	outDeliveries := make([]entity.Delivery, 0, len(deliveries))
	for _, d := range deliveries {
		if d.ID == uuid.Nil {
			d.ID = uuid.New()
		}
		d.NotificationID = n.ID
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notification_deliveries (
				id, notification_id, channel, provider, status, attempts
			) VALUES ($1,$2,$3,$4,$5,$6)`,
			d.ID, d.NotificationID, d.Channel, d.Provider, d.Status, d.Attempts,
		)
		if err != nil {
			return entity.Notification{}, nil, fmt.Errorf("insert delivery: %w", err)
		}
		outDeliveries = append(outDeliveries, d)
	}

	for _, e := range outboxEvents {
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
			return entity.Notification{}, nil, fmt.Errorf("insert outbox event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return entity.Notification{}, nil, err
	}

	created, err := r.GetByID(ctx, n.ID)
	if err != nil {
		return n, outDeliveries, nil
	}
	return created, outDeliveries, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (entity.Notification, error) {
	var row notificationRow
	err := r.db.GetContext(ctx, &row, `SELECT * FROM notifications WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Notification{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.Notification{}, err
	}
	return mapNotification(row)
}

func (r *Repository) GetByIdempotencyKey(ctx context.Context, key string) (entity.Notification, error) {
	var row notificationRow
	err := r.db.GetContext(ctx, &row, `SELECT * FROM notifications WHERE idempotency_key = $1`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Notification{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.Notification{}, err
	}
	return mapNotification(row)
}

func (r *Repository) UpdateNotificationStatus(ctx context.Context, id uuid.UUID, status entity.NotificationStatus) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}

func (r *Repository) RecomputeNotificationStatus(ctx context.Context, notificationID uuid.UUID) error {
	deliveries, err := r.ListDeliveries(ctx, notificationID)
	if err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return r.UpdateNotificationStatus(ctx, notificationID, entity.StatusFailed)
	}

	allSent := true
	anySent := false
	allFailed := true
	for _, d := range deliveries {
		switch d.Status {
		case entity.DeliverySent, entity.DeliveryDelivered:
			anySent = true
			allFailed = false
		case entity.DeliveryPermanentFailed, entity.DeliveryFailed:
			allSent = false
		default:
			allSent = false
			allFailed = false
		}
	}

	status := entity.StatusProcessing
	switch {
	case allSent:
		status = entity.StatusSent
	case allFailed:
		status = entity.StatusFailed
	case anySent:
		status = entity.StatusPartiallyFailed
	}
	return r.UpdateNotificationStatus(ctx, notificationID, status)
}

// ListForUser returns in-app-visible notifications for a user. A
// notification is visible if it targets the in_app channel (either via the
// channels array or via an in_app delivery row) and is not still awaiting
// its scheduled time.
func (r *Repository) ListForUser(ctx context.Context, userID uuid.UUID, page, perPage int) ([]entity.Notification, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	const visibilityWhere = `
		user_id = $1
		AND status <> 'scheduled'
		AND (
			channels @> '"in_app"'::jsonb
			OR EXISTS (
				SELECT 1 FROM notification_deliveries d
				WHERE d.notification_id = notifications.id AND d.channel = 'in_app'
			)
		)`

	var total int64
	if err := r.db.GetContext(ctx, &total, `
		SELECT COUNT(*) FROM notifications WHERE `+visibilityWhere, userID); err != nil {
		return nil, 0, err
	}

	var rows []notificationRow
	err := r.db.SelectContext(ctx, &rows, `
		SELECT * FROM notifications
		WHERE `+visibilityWhere+`
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, userID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}

	out := make([]entity.Notification, 0, len(rows))
	for _, row := range rows {
		n, err := mapNotification(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, total, nil
}

func (r *Repository) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM notifications
		WHERE user_id = $1 AND read_at IS NULL AND status <> 'scheduled'
		AND (
			channels @> '"in_app"'::jsonb
			OR EXISTS (
				SELECT 1 FROM notification_deliveries d
				WHERE d.notification_id = notifications.id AND d.channel = 'in_app'
			)
		)`, userID)
	return count, err
}

func (r *Repository) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND read_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// either already read or not found / not owned
		var exists bool
		_ = r.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM notifications WHERE id=$1 AND user_id=$2)`, id, userID)
		if !exists {
			return sharederrors.ErrNotFound
		}
	}
	return nil
}

func (r *Repository) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = NOW(), updated_at = NOW()
		WHERE user_id = $1 AND read_at IS NULL AND status <> 'scheduled'
		AND (
			channels @> '"in_app"'::jsonb
			OR EXISTS (
				SELECT 1 FROM notification_deliveries d
				WHERE d.notification_id = notifications.id AND d.channel = 'in_app'
			)
		)`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type batchAggRow struct {
	ID              uuid.UUID       `db:"id"`
	BatchID         *uuid.UUID      `db:"batch_id"`
	Title           string          `db:"title"`
	Message         string          `db:"message"`
	Type            string          `db:"type"`
	Priority        string          `db:"priority"`
	Channels        json.RawMessage `db:"channels"`
	Status          string          `db:"status"`
	RecipientsCount int             `db:"recipients_count"`
	SuccessCount    int             `db:"success_count"`
	FailedCount     int             `db:"failed_count"`
	CreatedBy       *uuid.UUID      `db:"created_by"`
	CreatedAt       time.Time       `db:"created_at"`
	ScheduledAt     *time.Time      `db:"scheduled_at"`
}

// ListAdminBatches lists one row per batch (grouped by batch_id, or by id
// for single-recipient notifications). The notification's own ID is always
// preserved in BatchSummary.ID; BatchSummary.BatchID is populated
// separately and only when the notification belongs to a batch.
func (r *Repository) ListAdminBatches(ctx context.Context, page, perPage int) ([]entity.BatchSummary, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	var total int64
	if err := r.db.GetContext(ctx, &total, `
		SELECT COUNT(*) FROM (
			SELECT COALESCE(batch_id, id) AS group_key FROM notifications GROUP BY COALESCE(batch_id, id)
		) t`); err != nil {
		return nil, 0, err
	}

	var rows []batchAggRow
	err := r.db.SelectContext(ctx, &rows, `
		WITH groups AS (
			SELECT
				COALESCE(batch_id, id) AS group_key,
				MIN(created_at) AS created_at
			FROM notifications
			GROUP BY COALESCE(batch_id, id)
			ORDER BY MIN(created_at) DESC
			LIMIT $1 OFFSET $2
		),
		agg AS (
			SELECT
				g.group_key,
				COUNT(*)::int AS recipients_count,
				COUNT(*) FILTER (WHERE n.status = 'sent')::int AS success_count,
				COUNT(*) FILTER (WHERE n.status IN ('failed', 'partially_failed'))::int AS failed_count,
				MIN(n.created_at) AS created_at,
				MIN(n.scheduled_at) AS scheduled_at
			FROM notifications n
			INNER JOIN groups g ON COALESCE(n.batch_id, n.id) = g.group_key
			GROUP BY g.group_key
		)
		SELECT
			first_row.id,
			n.batch_id,
			n.title,
			n.message,
			n.type,
			n.priority,
			n.channels,
			n.status,
			a.recipients_count,
			a.success_count,
			a.failed_count,
			n.created_by,
			a.created_at,
			a.scheduled_at
		FROM agg a
		INNER JOIN LATERAL (
			SELECT id FROM notifications n2
			WHERE COALESCE(n2.batch_id, n2.id) = a.group_key
			ORDER BY n2.created_at ASC
			LIMIT 1
		) first_row ON true
		INNER JOIN notifications n ON n.id = first_row.id
		ORDER BY a.created_at DESC
	`, perPage, offset)
	if err != nil {
		return nil, 0, err
	}

	out := make([]entity.BatchSummary, 0, len(rows))
	for _, row := range rows {
		var channels []entity.Channel
		var raw []string
		_ = json.Unmarshal(row.Channels, &raw)
		for _, c := range raw {
			channels = append(channels, entity.Channel(c))
		}
		out = append(out, entity.BatchSummary{
			ID:              row.ID,
			BatchID:         row.BatchID,
			Title:           row.Title,
			Message:         row.Message,
			Type:            row.Type,
			Priority:        entity.Priority(row.Priority),
			Channels:        channels,
			Status:          entity.NotificationStatus(row.Status),
			RecipientsCount: row.RecipientsCount,
			SuccessCount:    row.SuccessCount,
			FailedCount:     row.FailedCount,
			CreatedBy:       row.CreatedBy,
			CreatedAt:       row.CreatedAt,
			ScheduledAt:     row.ScheduledAt,
		})
	}
	return out, total, nil
}

// GetBatch returns every notification belonging to a batch (identified by
// batch_id), ordered by creation time.
func (r *Repository) GetBatch(ctx context.Context, batchID uuid.UUID) ([]entity.Notification, error) {
	var rows []notificationRow
	err := r.db.SelectContext(ctx, &rows, `
		SELECT * FROM notifications WHERE batch_id = $1 ORDER BY created_at ASC`, batchID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sharederrors.ErrNotFound
	}
	out := make([]entity.Notification, 0, len(rows))
	for _, row := range rows {
		n, err := mapNotification(row)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
