package application

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mehrdad-masoumi/go-packages/apperr"

	notificationdto "notification-service/internal/notification/dto"
	notificationservice "notification-service/internal/notification/service"
)

// CommandService is the shared application entry for all transports.
type CommandService struct {
	inner *notificationservice.Service
}

func NewCommandService(inner *notificationservice.Service) *CommandService {
	return &CommandService{inner: inner}
}

// Send validates (via inner AcceptCommand path) and durably accepts a notification.
func (s *CommandService) Send(ctx context.Context, cmd SendNotificationCommand) (SendResult, error) {
	const op = "notification_application.Send"

	if strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return SendResult{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("idempotency_key is required")
	}
	if strings.TrimSpace(cmd.TemplateCode) == "" {
		return SendResult{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("template_code is required")
	}

	req := toCommandRequest(cmd)
	resp, code, err := s.inner.AcceptCommand(ctx, req)
	if err != nil {
		return SendResult{}, err
	}

	duplicate := false
	status := resp.Status
	if code == http.StatusOK && resp.ID != "" {
		duplicate = true
		if status == "" || status == "accepted" || status == "scheduled" {
			status = "duplicate"
		}
	}

	return SendResult{
		NotificationID: resp.ID,
		Status:         status,
		Duplicate:      duplicate,
		AcceptedAt:     time.Now().UTC(),
		HTTPStatus:     code,
	}, nil
}

func toCommandRequest(cmd SendNotificationCommand) notificationdto.CommandRequest {
	channels := make([]string, 0, len(cmd.Channels))
	for _, ch := range cmd.Channels {
		channels = append(channels, string(ch))
	}

	// Callers supply contacts; treat provided email/phone as ready to send
	// (no User Service lookup / verified-flag filtering on contract path).
	contacts := &notificationdto.Contacts{
		Email:         cmd.Recipient.Email,
		Phone:         cmd.Recipient.Phone,
		Locale:        cmd.Locale,
		EmailVerified: cmd.Recipient.Email != "",
		PhoneVerified: cmd.Recipient.Phone != "",
		Preferences:   map[string]bool{},
	}

	vars := map[string]any{}
	for k, v := range cmd.Variables {
		vars[k] = v
	}

	userID := strings.TrimSpace(cmd.Recipient.UserID)
	if userID == "" {
		userID = uuid.Nil.String()
	}

	return notificationdto.CommandRequest{
		IdempotencyKey: cmd.IdempotencyKey,
		UserID:         userID,
		TemplateCode:   cmd.TemplateCode,
		Locale:         cmd.Locale,
		Channels:       channels,
		Priority:       cmd.Priority,
		Variables:      vars,
		ActionURL:      cmd.ActionURL,
		ScheduledAt:    cmd.ScheduledAt,
		Contacts:       contacts,
		MessageID:      cmd.MessageID,
		SourceService:  cmd.SourceService,
		CorrelationID:  cmd.CorrelationID,
		TraceID:        cmd.TraceID,
		Metadata:       cmd.Metadata,
		DeviceTokens:   append([]string(nil), cmd.Recipient.DeviceTokens...),
		DisplayName:    cmd.Recipient.DisplayName,
	}
}
