package httptransport

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	application "notification-service/internal/application/notification"
)

// AdminHandler exposes Admin create endpoints under /admin/v1.
type AdminHandler struct {
	cmds *application.CommandService
}

func NewAdminHandler(cmds *application.CommandService) *AdminHandler {
	return &AdminHandler{cmds: cmds}
}

func (h *AdminHandler) Register(g *echo.Group) {
	g.POST("/notifications", h.Create)
	g.POST("/notification-batches", h.CreateBatch)
}

type adminCreateRequest struct {
	IdempotencyKey string            `json:"idempotency_key"`
	TemplateCode   string            `json:"template_code"`
	Locale         string            `json:"locale"`
	Channels       []string          `json:"channels"`
	Variables      map[string]any    `json:"variables"`
	Metadata       map[string]string `json:"metadata"`
	ScheduledAt    *time.Time        `json:"scheduled_at"`
	Recipient      adminRecipient    `json:"recipient"`
	Priority       string            `json:"priority"`
}

type adminRecipient struct {
	UserID       string   `json:"user_id"`
	Email        string   `json:"email"`
	Phone        string   `json:"phone"`
	DeviceTokens []string `json:"device_tokens"`
	DisplayName  string   `json:"display_name"`
}

type adminBatchRequest struct {
	IdempotencyKey string            `json:"idempotency_key"`
	TemplateCode   string            `json:"template_code"`
	Locale         string            `json:"locale"`
	Channels       []string          `json:"channels"`
	Variables      map[string]any    `json:"variables"`
	Metadata       map[string]string `json:"metadata"`
	ScheduledAt    *time.Time        `json:"scheduled_at"`
	Recipients     []adminRecipient  `json:"recipients"`
	Priority       string            `json:"priority"`
}

type adminAcceptedResponse struct {
	ID      string `json:"notification_id,omitempty"`
	BatchID string `json:"batch_id,omitempty"`
	Status  string `json:"status"`
}

func (h *AdminHandler) Create(c echo.Context) error {
	var req adminCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid json")
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = "admin:" + uuid.NewString()
	}
	cmd := application.SendNotificationCommand{
		MessageID:      uuid.NewString(),
		IdempotencyKey: req.IdempotencyKey,
		SourceService:  "admin-panel",
		TemplateCode:   req.TemplateCode,
		Locale:         req.Locale,
		Recipient: application.Recipient{
			UserID:       req.Recipient.UserID,
			Email:        req.Recipient.Email,
			Phone:        req.Recipient.Phone,
			DeviceTokens: req.Recipient.DeviceTokens,
			DisplayName:  req.Recipient.DisplayName,
		},
		Channels:    toChannels(req.Channels),
		Variables:   req.Variables,
		Metadata:    req.Metadata,
		ScheduledAt: req.ScheduledAt,
		Priority:    req.Priority,
	}
	result, err := h.cmds.Send(c.Request().Context(), cmd)
	if err != nil {
		return err
	}
	return c.JSON(result.HTTPStatus, adminAcceptedResponse{
		ID:     result.NotificationID,
		Status: result.Status,
	})
}

func (h *AdminHandler) CreateBatch(c echo.Context) error {
	var req adminBatchRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid json")
	}
	if len(req.Recipients) == 0 {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "recipients required")
	}
	batchID := uuid.NewString()
	baseKey := req.IdempotencyKey
	if baseKey == "" {
		baseKey = "admin-batch:" + batchID
	}
	var last application.SendResult
	for i, r := range req.Recipients {
		cmd := application.SendNotificationCommand{
			MessageID:      uuid.NewString(),
			IdempotencyKey: baseKey + ":" + itoa(i),
			SourceService:  "admin-panel",
			TemplateCode:   req.TemplateCode,
			Locale:         req.Locale,
			Recipient: application.Recipient{
				UserID:       r.UserID,
				Email:        r.Email,
				Phone:        r.Phone,
				DeviceTokens: r.DeviceTokens,
				DisplayName:  r.DisplayName,
			},
			Channels:    toChannels(req.Channels),
			Variables:   req.Variables,
			Metadata:    mergeMeta(req.Metadata, map[string]string{"batch_id": batchID}),
			ScheduledAt: req.ScheduledAt,
			Priority:    req.Priority,
		}
		result, err := h.cmds.Send(c.Request().Context(), cmd)
		if err != nil {
			return err
		}
		last = result
	}
	return c.JSON(http.StatusAccepted, adminAcceptedResponse{
		BatchID: batchID,
		ID:      last.NotificationID,
		Status:  "accepted",
	})
}

func toChannels(in []string) []application.Channel {
	out := make([]application.Channel, 0, len(in))
	for _, ch := range in {
		out = append(out, application.Channel(ch))
	}
	return out
}

func mergeMeta(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
