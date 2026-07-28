package notificationrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"notification-service/internal/notification/entity"
	"notification-service/pkg/sharederrors"
)

type templateRow struct {
	ID              uuid.UUID `db:"id"`
	Code            string    `db:"code"`
	Locale          string    `db:"locale"`
	Channel         string    `db:"channel"`
	Subject         *string   `db:"subject"`
	Body            string    `db:"body"`
	DefaultPriority string    `db:"default_priority"`
	Enabled         bool      `db:"enabled"`
	Version         int       `db:"version"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

func mapTemplate(row templateRow) entity.Template {
	return entity.Template{
		ID:              row.ID,
		Code:            row.Code,
		Locale:          row.Locale,
		Channel:         entity.Channel(row.Channel),
		Subject:         row.Subject,
		Body:            row.Body,
		DefaultPriority: entity.Priority(row.DefaultPriority),
		Enabled:         row.Enabled,
		Version:         row.Version,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func (r *Repository) CreateTemplate(ctx context.Context, t entity.Template) (entity.Template, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.Locale == "" {
		t.Locale = entity.DefaultLocale
	}
	if t.DefaultPriority == "" {
		t.DefaultPriority = entity.PriorityNormal
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notification_templates (id, code, locale, channel, subject, body, default_priority, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.Code, t.Locale, t.Channel, t.Subject, t.Body, t.DefaultPriority, t.Enabled,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return entity.Template{}, sharederrors.ErrAlreadyExists
		}
		return entity.Template{}, err
	}
	return r.GetTemplateByID(ctx, t.ID)
}

func (r *Repository) GetTemplateByID(ctx context.Context, id uuid.UUID) (entity.Template, error) {
	var row templateRow
	err := r.db.GetContext(ctx, &row, `SELECT * FROM notification_templates WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Template{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.Template{}, err
	}
	return mapTemplate(row), nil
}

func (r *Repository) UpdateTemplate(ctx context.Context, id uuid.UUID, subject *string, body *string, defaultPriority *string) (entity.Template, error) {
	current, err := r.GetTemplateByID(ctx, id)
	if err != nil {
		return entity.Template{}, err
	}
	if subject != nil {
		current.Subject = subject
	}
	if body != nil {
		current.Body = *body
	}
	if defaultPriority != nil {
		current.DefaultPriority = entity.Priority(*defaultPriority)
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE notification_templates
		SET subject = $2, body = $3, default_priority = $4, version = version + 1, updated_at = NOW()
		WHERE id = $1`,
		id, current.Subject, current.Body, current.DefaultPriority,
	)
	if err != nil {
		return entity.Template{}, err
	}
	return r.GetTemplateByID(ctx, id)
}

func (r *Repository) SetTemplateEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notification_templates SET enabled = $2, updated_at = NOW() WHERE id = $1`, id, enabled)
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

func (r *Repository) ListTemplates(ctx context.Context, code, locale, channel string, enabled *bool, page, perPage int) ([]entity.Template, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if code != "" {
		conds = append(conds, "code = "+arg(code))
	}
	if locale != "" {
		conds = append(conds, "locale = "+arg(locale))
	}
	if channel != "" {
		conds = append(conds, "channel = "+arg(channel))
	}
	if enabled != nil {
		conds = append(conds, "enabled = "+arg(*enabled))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int64
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM notification_templates `+where, args...); err != nil {
		return nil, 0, err
	}

	limitArgs := append(append([]any{}, args...), perPage, offset)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	offsetPlaceholder := fmt.Sprintf("$%d", len(args)+2)

	var rows []templateRow
	q := fmt.Sprintf(`SELECT * FROM notification_templates %s ORDER BY code, locale, channel LIMIT %s OFFSET %s`,
		where, limitPlaceholder, offsetPlaceholder)
	if err := r.db.SelectContext(ctx, &rows, q, limitArgs...); err != nil {
		return nil, 0, err
	}

	out := make([]entity.Template, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapTemplate(row))
	}
	return out, total, nil
}

// GetEnabledTemplate resolves the best enabled template for (code, channel):
// exact locale match first, then fallback locale ("fa"), then any enabled
// template for that code+channel.
func (r *Repository) GetEnabledTemplate(ctx context.Context, code, locale, channel string) (entity.Template, error) {
	var row templateRow
	err := r.db.GetContext(ctx, &row, `
		SELECT * FROM notification_templates
		WHERE code = $1 AND channel = $2 AND enabled = TRUE
		ORDER BY
			CASE WHEN locale = $3 THEN 0 WHEN locale = $4 THEN 1 ELSE 2 END,
			updated_at DESC
		LIMIT 1`, code, channel, locale, entity.DefaultLocale)
	if errors.Is(err, sql.ErrNoRows) {
		return entity.Template{}, sharederrors.ErrNotFound
	}
	if err != nil {
		return entity.Template{}, err
	}
	return mapTemplate(row), nil
}

// ListEnabledChannelsForCode returns the distinct channels that have at
// least one enabled template for the given code (used to default the
// channel set of a v1 command when the caller does not specify one).
func (r *Repository) ListEnabledChannelsForCode(ctx context.Context, code string) ([]string, error) {
	var channels []string
	err := r.db.SelectContext(ctx, &channels, `
		SELECT DISTINCT channel FROM notification_templates WHERE code = $1 AND enabled = TRUE`, code)
	if err != nil {
		return nil, err
	}
	return channels, nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "duplicate key value violates unique constraint")
}
