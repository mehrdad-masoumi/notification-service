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

func (r *Repository) CreateNotificationWithDeliveries(ctx context.Context, n entity.Notification, deliveries []entity.Delivery) (entity.Notification, []entity.Delivery, error) {
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
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO notifications (
			id, user_id, batch_id, title, message, type, priority, payload, action_url,
			status, idempotency_key, channels, scheduled_at, created_by, email, phone
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16
		)`,
		n.ID, n.UserID, n.BatchID, n.Title, n.Message, n.Type, n.Priority, n.Payload, n.ActionURL,
		n.Status, n.IdempotencyKey, channelsJSON, n.ScheduledAt, n.CreatedBy, n.Email, n.Phone,
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
	_, err := r.db.ExecContext(ctx, `
		UPDATE notification_deliveries
		SET provider = $2, status = $3, attempts = $4, error = $5, sent_at = $6, delivered_at = $7, updated_at = NOW()
		WHERE id = $1`,
		d.ID, d.Provider, d.Status, d.Attempts, d.Error, d.SentAt, d.DeliveredAt,
	)
	return err
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

func (r *Repository) ListForUser(ctx context.Context, userID uuid.UUID, page, perPage int) ([]entity.Notification, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	var total int64
	if err := r.db.GetContext(ctx, &total, `
		SELECT COUNT(*) FROM notifications
		WHERE user_id = $1 AND channels::text LIKE '%in_app%'`, userID); err != nil {
		return nil, 0, err
	}

	var rows []notificationRow
	err := r.db.SelectContext(ctx, &rows, `
		SELECT * FROM notifications
		WHERE user_id = $1 AND channels::text LIKE '%in_app%'
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
		WHERE user_id = $1 AND read_at IS NULL AND channels::text LIKE '%in_app%'`, userID)
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
		WHERE user_id = $1 AND read_at IS NULL AND channels::text LIKE '%in_app%'`, userID)
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
			n.id,
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
		FROM notifications n
		INNER JOIN agg a ON COALESCE(n.batch_id, n.id) = a.group_key
		INNER JOIN LATERAL (
			SELECT id FROM notifications n2
			WHERE COALESCE(n2.batch_id, n2.id) = a.group_key
			ORDER BY n2.created_at ASC
			LIMIT 1
		) first_row ON n.id = first_row.id
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

func (r *Repository) ListDueScheduled(ctx context.Context, limit int) ([]entity.Notification, error) {
	var rows []notificationRow
	err := r.db.SelectContext(ctx, &rows, `
		SELECT * FROM notifications
		WHERE status = 'scheduled' AND scheduled_at IS NOT NULL AND scheduled_at <= NOW()
		ORDER BY scheduled_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
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

func (r *Repository) ClaimScheduled(ctx context.Context, id uuid.UUID) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET status = 'queued', updated_at = NOW()
		WHERE id = $1 AND status = 'scheduled'`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Idempotency helpers

type IdempotencyRecord struct {
	Status       string
	ResponseCode int
	ResponseBody json.RawMessage
}

func (r *Repository) BeginIdempotency(ctx context.Context, key, operation, requestHash string) (*IdempotencyRecord, bool, error) {
	var status string
	var code sql.NullInt64
	var body []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT status, response_code, response_body FROM idempotency_keys WHERE ide_key = $1`, key,
	).Scan(&status, &code, &body)
	if err == nil {
		rec := &IdempotencyRecord{Status: status, ResponseBody: body}
		if code.Valid {
			rec.ResponseCode = int(code.Int64)
		}
		return rec, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO idempotency_keys (ide_key, operation, request_hash, status)
		VALUES ($1,$2,$3,'processing')
		ON CONFLICT (ide_key) DO NOTHING`, key, operation, requestHash)
	if err != nil {
		return nil, false, err
	}

	err = r.db.QueryRowContext(ctx, `
		SELECT status, response_code, response_body FROM idempotency_keys WHERE ide_key = $1`, key,
	).Scan(&status, &code, &body)
	if err != nil {
		return nil, false, err
	}
	rec := &IdempotencyRecord{Status: status, ResponseBody: body}
	if code.Valid {
		rec.ResponseCode = int(code.Int64)
	}
	return rec, status != "processing", nil
}

func (r *Repository) CompleteIdempotency(ctx context.Context, key string, code int, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET status = 'succeeded', response_code = $2, response_body = $3, updated_at = NOW()
		WHERE ide_key = $1`, key, code, b)
	return err
}

func (r *Repository) FailIdempotency(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys SET status = 'failed', updated_at = NOW() WHERE ide_key = $1`, key)
	return err
}
