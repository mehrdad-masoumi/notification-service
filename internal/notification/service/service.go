package notificationservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
	notificationmetrics "notification-service/internal/notification/metrics"
	notificationrepo "notification-service/internal/notification/repository"
	notificationtemplate "notification-service/internal/notification/template"
	notificationvalidator "notification-service/internal/notification/validator"
	"notification-service/pkg/sharederrors"

	"github.com/mehrdad-masoumi/go-packages/apperr"
)

// channelPriorityOrder decides which channel's template is used to fill
// the notification's legacy single title/message columns (used for
// admin/user display); per-channel bodies are re-rendered by the worker at
// send time from template_code + locale + variables.
var channelPriorityOrder = []entity.Channel{
	entity.ChannelInApp,
	entity.ChannelEmail,
	entity.ChannelSMS,
	entity.ChannelWhatsApp,
	entity.ChannelPush,
}

type Service struct {
	repo                  *notificationrepo.Repository
	validator             notificationvalidator.Validator
	directRateLimitPerMin int
}

func New(
	repo *notificationrepo.Repository,
	validator notificationvalidator.Validator,
	directRateLimitPerMin int,
) *Service {
	return &Service{
		repo:                  repo,
		validator:             validator,
		directRateLimitPerMin: directRateLimitPerMin,
	}
}

func (s *Service) Repo() *notificationrepo.Repository {
	return s.repo
}

// ---------------------------------------------------------------------
// v1 template-driven commands
// ---------------------------------------------------------------------

// AcceptCommand implements the v1 template-driven notification command.
// It never publishes to RabbitMQ directly: notification + deliveries +
// outbox rows are written in one DB transaction, and a separate outbox
// publisher process delivers them to the broker.
func (s *Service) AcceptCommand(ctx context.Context, req notificationdto.CommandRequest) (notificationdto.AcceptedResponse, int, error) {
	const op = "notification_service.AcceptCommand"

	if fields, err := s.validator.ValidateCommand(req); err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: fields}
	}

	locale := req.Locale
	if locale == "" && req.Contacts != nil && req.Contacts.Locale != "" {
		locale = req.Contacts.Locale
	}
	if locale == "" {
		locale = entity.DefaultLocale
	}

	hash := hashJSON(req)
	rec, outcome, err := s.repo.ClaimIdempotency(ctx, req.IdempotencyKey, op, hash)
	if err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	if resp, code, done, err := s.handleClaimOutcome(rec, outcome, op); done {
		return resp, code, err
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusBadRequest, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user_id")
	}

	contacts := *req.Contacts

	channels := toChannels(req.Channels)
	if len(channels) == 0 {
		codes, err := s.repo.ListEnabledChannelsForCode(ctx, req.TemplateCode)
		if err != nil {
			_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
			return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
		}
		channels = toChannels(codes)
	}
	if len(channels) == 0 {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: map[string]string{"template_code": "validation.notfound.template_code"}}
	}

	channels = filterChannelsByContacts(channels, contacts)
	if len(channels) == 0 {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: map[string]string{"channels": "validation.norecipient.channels"}}
	}

	templates, channels, err := s.loadTemplatesForChannels(ctx, req.TemplateCode, locale, channels)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	if len(channels) == 0 {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: map[string]string{"template_code": "validation.notfound.template"}}
	}

	if fields := validateTemplateVariables(templates, req.Variables); len(fields) > 0 {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: fields}
	}

	priority := resolvePriority(req.Priority, templates)
	title, message := primaryTitleMessage(templates, req.Variables, req.TemplateCode)

	variablesJSON, err := json.Marshal(req.Variables)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusBadRequest, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid variables")
	}

	var actionURL *string
	if req.ActionURL != "" {
		actionURL = &req.ActionURL
	}
	templateCode := req.TemplateCode

	now := time.Now().UTC()
	status := entity.StatusPending
	scheduled := false
	if req.ScheduledAt != nil && req.ScheduledAt.After(now) {
		status = entity.StatusScheduled
		scheduled = true
	}

	n := entity.Notification{
		ID:           uuid.New(),
		UserID:       userID,
		Title:        title,
		Message:      message,
		Type:         "template",
		Priority:     priority,
		Payload:      json.RawMessage(`{}`),
		ActionURL:    actionURL,
		Status:       status,
		Channels:     channels,
		ScheduledAt:  req.ScheduledAt,
		TemplateCode: &templateCode,
		Locale:       locale,
		Variables:    variablesJSON,
	}
	if contacts.Email != "" {
		email := contacts.Email
		n.Email = &email
	}
	if contacts.Phone != "" {
		phone := contacts.Phone
		n.Phone = &phone
	}
	key := req.IdempotencyKey
	n.IdempotencyKey = &key

	deliveries := makeDeliveries(channels)
	var outboxEvents []entity.OutboxEvent
	if !scheduled {
		outboxEvents, err = buildOutboxEvents(n.ID, priority, deliveries)
		if err != nil {
			_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
			return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
		}
	}

	resp := notificationdto.AcceptedResponse{ID: n.ID.String(), Status: "accepted"}
	result := "accepted"
	if scheduled {
		resp.Status = "scheduled"
		result = "scheduled"
	}
	code := http.StatusAccepted
	created, _, err := s.repo.CreateNotificationBundle(ctx, n, deliveries, outboxEvents, &notificationrepo.IdempotencyCompletion{
		Key:  req.IdempotencyKey,
		Code: code,
		Body: resp,
	})
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		notificationmetrics.IncError("accept_command")
		notificationmetrics.IncCommand("command", string(priority), "error")
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to create notification")
	}

	notificationmetrics.IncCommand("command", string(priority), result)
	return notificationdto.AcceptedResponse{ID: created.ID.String(), Status: resp.Status}, code, nil
}

// AcceptDirectCommand sends a single-channel notification to an explicit
// recipient (OTP / pre-user flows).
func (s *Service) AcceptDirectCommand(ctx context.Context, req notificationdto.DirectCommandRequest) (notificationdto.AcceptedResponse, int, error) {
	const op = "notification_service.AcceptDirectCommand"

	if fields, err := s.validator.ValidateDirectCommand(req); err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: fields}
	}

	locale := req.Locale
	if locale == "" {
		locale = entity.DefaultLocale
	}

	hash := hashJSON(req)
	rec, outcome, err := s.repo.ClaimIdempotency(ctx, req.IdempotencyKey, op, hash)
	if err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	if resp, code, done, err := s.handleClaimOutcome(rec, outcome, op); done {
		return resp, code, err
	}

	if err := s.checkDirectRateLimit(ctx, req.Recipient); err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusTooManyRequests, err
	}

	tmpl, err := s.repo.GetEnabledTemplate(ctx, req.TemplateCode, locale, req.Channel)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		if errors.Is(err, sharederrors.ErrNotFound) {
			return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: map[string]string{"template_code": "validation.notfound.template"}}
		}
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}

	subject := ""
	if tmpl.Subject != nil {
		subject = *tmpl.Subject
	}
	if fields := validateTemplateVariables([]entity.Template{tmpl}, req.Variables); len(fields) > 0 {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		notificationmetrics.IncTemplateRenderError(req.Channel)
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: fields}
	}
	renderedSubject, renderedBody, _ := notificationtemplate.RenderPair(subject, tmpl.Body, req.Variables)

	priority := entity.PriorityNormal
	if req.Priority != "" {
		priority = entity.Priority(req.Priority)
	} else if tmpl.DefaultPriority != "" {
		priority = tmpl.DefaultPriority
	}

	variablesJSON, err := json.Marshal(req.Variables)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusBadRequest, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid variables")
	}

	channel := entity.Channel(req.Channel)
	templateCode := req.TemplateCode
	key := req.IdempotencyKey

	n := entity.Notification{
		ID:             uuid.New(),
		UserID:         uuid.Nil,
		Title:          renderedSubject,
		Message:        renderedBody,
		Type:           "template_direct",
		Priority:       priority,
		Payload:        json.RawMessage(`{}`),
		Status:         entity.StatusPending,
		IdempotencyKey: &key,
		Channels:       []entity.Channel{channel},
		TemplateCode:   &templateCode,
		Locale:         locale,
		Variables:      variablesJSON,
	}
	switch channel {
	case entity.ChannelEmail:
		n.Email = &req.Recipient
	case entity.ChannelSMS, entity.ChannelWhatsApp:
		n.Phone = &req.Recipient
	}

	deliveries := makeDeliveries(n.Channels)
	outboxEvents, err := buildOutboxEvents(n.ID, priority, deliveries)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}

	resp := notificationdto.AcceptedResponse{ID: n.ID.String(), Status: "accepted"}
	code := http.StatusAccepted
	created, _, err := s.repo.CreateNotificationBundle(ctx, n, deliveries, outboxEvents, &notificationrepo.IdempotencyCompletion{
		Key:  req.IdempotencyKey,
		Code: code,
		Body: resp,
	})
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		notificationmetrics.IncError("accept_direct_command")
		notificationmetrics.IncCommand("direct_command", string(priority), "error")
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to create notification")
	}

	notificationmetrics.IncCommand("direct_command", string(priority), "accepted")
	return notificationdto.AcceptedResponse{ID: created.ID.String(), Status: "accepted"}, code, nil
}

// handleClaimOutcome interprets a ClaimIdempotency result. done=true means
// the caller should return (resp, code, err) immediately without further
// processing (replay / conflict / in-progress); done=false means the
// caller acquired the claim and should proceed.
func (s *Service) handleClaimOutcome(rec notificationrepo.IdempotencyRecord, outcome notificationrepo.ClaimOutcome, op string) (notificationdto.AcceptedResponse, int, bool, error) {
	switch outcome {
	case notificationrepo.ClaimAcquired:
		return notificationdto.AcceptedResponse{}, 0, false, nil
	case notificationrepo.ClaimReplay:
		var resp notificationdto.AcceptedResponse
		_ = json.Unmarshal(rec.ResponseBody, &resp)
		code := rec.ResponseCode
		if code == 0 {
			code = http.StatusAccepted
		}
		return resp, code, true, nil
	case notificationrepo.ClaimConflict:
		notificationmetrics.IncIdempotencyConflict("hash_mismatch")
		return notificationdto.AcceptedResponse{}, http.StatusConflict, true, apperr.New(op).
			WithKind(apperr.KindInvalid).
			WithMessage("idempotency key reused with a different request payload")
	default: // ClaimInProgress
		notificationmetrics.IncIdempotencyConflict("in_progress")
		return notificationdto.AcceptedResponse{}, http.StatusConflict, true, apperr.New(op).
			WithKind(apperr.KindInvalid).
			WithMessage("request already processing")
	}
}

// ---------------------------------------------------------------------
// Deprecated free-text endpoints (kept working; now outbox-backed)
// ---------------------------------------------------------------------

func (s *Service) AdminCreate(ctx context.Context, req notificationdto.AdminCreateRequest, createdBy string) (notificationdto.AcceptedResponse, error) {
	const op = "notification_service.AdminCreate"

	if fields, err := s.validator.ValidateAdminCreate(req); err != nil {
		return notificationdto.AcceptedResponse{}, &apperr.Error{Fields: fields}
	}

	priority := entity.PriorityNormal
	if req.Priority != "" {
		priority = entity.Priority(req.Priority)
	}
	notifType := req.Type
	if notifType == "" {
		notifType = "system"
	}

	channels := toChannels(req.Channels)
	payload, _ := json.Marshal(req.Payload)
	if req.Payload == nil {
		payload = json.RawMessage(`{}`)
	}

	var createdByUUID *uuid.UUID
	if createdBy != "" {
		if id, err := uuid.Parse(createdBy); err == nil {
			createdByUUID = &id
		}
	}

	var actionURL *string
	if req.ActionURL != "" {
		actionURL = &req.ActionURL
	}
	var email, phone *string
	if req.Email != "" {
		email = &req.Email
	}
	if req.Phone != "" {
		phone = &req.Phone
	}

	now := time.Now().UTC()
	status := entity.StatusPending
	scheduled := false
	if req.ScheduledAt != nil && req.ScheduledAt.After(now) {
		status = entity.StatusScheduled
		scheduled = true
	}

	batchID := uuid.New()
	var firstID uuid.UUID

	for _, uidStr := range req.UserIDs {
		userID, err := uuid.Parse(uidStr)
		if err != nil {
			return notificationdto.AcceptedResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user_id")
		}

		n := entity.Notification{
			ID:          uuid.New(),
			UserID:      userID,
			BatchID:     &batchID,
			Title:       req.Title,
			Message:     req.Message,
			Type:        notifType,
			Priority:    priority,
			Payload:     payload,
			ActionURL:   actionURL,
			Status:      status,
			Channels:    channels,
			ScheduledAt: req.ScheduledAt,
			CreatedBy:   createdByUUID,
			Email:       email,
			Phone:       phone,
		}
		if firstID == uuid.Nil {
			firstID = n.ID
		}

		deliveries := makeDeliveries(channels)
		var outboxEvents []entity.OutboxEvent
		if !scheduled {
			outboxEvents, err = buildOutboxEvents(n.ID, priority, deliveries)
			if err != nil {
				notificationmetrics.IncError("admin_create")
				return notificationdto.AcceptedResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
			}
		}

		_, _, err = s.repo.CreateNotificationBundle(ctx, n, deliveries, outboxEvents, nil)
		if err != nil {
			notificationmetrics.IncError("admin_create")
			notificationmetrics.IncAccepted("admin", string(priority), "error")
			return notificationdto.AcceptedResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to create notification")
		}
	}

	result := "success"
	resp := notificationdto.AcceptedResponse{
		BatchID: batchID.String(),
		ID:      firstID.String(),
		Status:  "accepted",
	}
	if scheduled {
		resp.Status = "scheduled"
		result = "scheduled"
	}
	notificationmetrics.IncAccepted("admin", string(priority), result)
	return resp, nil
}

// InternalCreate is deprecated: prefer AcceptCommand (POST
// /internal/v1/notifications). Kept working for backward compatibility;
// writes to the transactional outbox instead of publishing directly.
func (s *Service) InternalCreate(ctx context.Context, req notificationdto.InternalCreateRequest) (notificationdto.AcceptedResponse, int, error) {
	const op = "notification_service.InternalCreate"

	if fields, err := s.validator.ValidateInternalCreate(req); err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: fields}
	}

	hash := hashJSON(req)
	rec, outcome, err := s.repo.ClaimIdempotency(ctx, req.IdempotencyKey, op, hash)
	if err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	if resp, code, done, err := s.handleClaimOutcome(rec, outcome, op); done {
		return resp, code, err
	}

	priority := entity.PriorityNormal
	if req.Priority != "" {
		priority = entity.Priority(req.Priority)
	}
	notifType := req.Type
	if notifType == "" {
		notifType = "system"
	}
	channels := toChannels(req.Channels)
	payload, _ := json.Marshal(req.Payload)
	if req.Payload == nil {
		payload = json.RawMessage(`{}`)
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusBadRequest, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user_id")
	}

	var actionURL *string
	if req.ActionURL != "" {
		actionURL = &req.ActionURL
	}
	var email, phone *string
	if req.Email != "" {
		email = &req.Email
	}
	if req.Phone != "" {
		phone = &req.Phone
	}
	key := req.IdempotencyKey

	now := time.Now().UTC()
	status := entity.StatusPending
	scheduled := false
	if req.ScheduledAt != nil && req.ScheduledAt.After(now) {
		status = entity.StatusScheduled
		scheduled = true
	}

	n := entity.Notification{
		ID:             uuid.New(),
		UserID:         userID,
		Title:          req.Title,
		Message:        req.Message,
		Type:           notifType,
		Priority:       priority,
		Payload:        payload,
		ActionURL:      actionURL,
		Status:         status,
		IdempotencyKey: &key,
		Channels:       channels,
		ScheduledAt:    req.ScheduledAt,
		Email:          email,
		Phone:          phone,
	}

	deliveries := makeDeliveries(channels)
	var outboxEvents []entity.OutboxEvent
	if !scheduled {
		outboxEvents, err = buildOutboxEvents(n.ID, priority, deliveries)
		if err != nil {
			_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
			return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
		}
	}

	result := "success"
	resp := notificationdto.AcceptedResponse{ID: n.ID.String(), Status: "accepted"}
	if scheduled {
		resp.Status = "scheduled"
		result = "scheduled"
	}
	created, _, err := s.repo.CreateNotificationBundle(ctx, n, deliveries, outboxEvents, &notificationrepo.IdempotencyCompletion{
		Key:  req.IdempotencyKey,
		Code: http.StatusAccepted,
		Body: resp,
	})
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		notificationmetrics.IncError("internal_create")
		notificationmetrics.IncAccepted("internal", string(priority), "error")
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to create notification")
	}

	notificationmetrics.IncAccepted("internal", string(priority), result)
	return notificationdto.AcceptedResponse{ID: created.ID.String(), Status: resp.Status}, http.StatusAccepted, nil
}

// EnqueueExisting writes any missing outbox rows for a notification's
// deliveries (idempotent: existing outbox rows are left untouched) and
// marks the notification queued. It never publishes to RabbitMQ directly.
func (s *Service) EnqueueExisting(ctx context.Context, n entity.Notification) error {
	deliveries, err := s.repo.ListDeliveries(ctx, n.ID)
	if err != nil {
		return err
	}

	var events []entity.OutboxEvent
	for _, d := range deliveries {
		if d.Status == entity.DeliverySent || d.Status == entity.DeliveryDelivered || d.Status == entity.DeliveryPermanentFailed {
			continue
		}
		has, err := s.repo.HasOutboxForDelivery(ctx, d.ID)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		job := notificationdto.QueueJob{
			NotificationID: n.ID.String(),
			DeliveryID:     d.ID.String(),
			Channel:        string(d.Channel),
			Attempt:        0,
		}
		payloadJSON, err := json.Marshal(job)
		if err != nil {
			return err
		}
		events = append(events, entity.OutboxEvent{
			AggregateID: n.ID,
			DeliveryID:  d.ID,
			EventType:   "notification.delivery.created",
			RoutingKey:  "notification." + string(n.Priority),
			Payload:     payloadJSON,
			Status:      entity.OutboxPending,
		})
	}
	if len(events) == 0 {
		return nil
	}
	if err := s.repo.InsertOutboxEvents(ctx, events); err != nil {
		return err
	}
	return s.repo.UpdateNotificationStatus(ctx, n.ID, entity.StatusQueued)
}

// PromoteScheduled claims due scheduled notifications and writes their
// outbox rows atomically. Called periodically by the scheduler.
func (s *Service) PromoteScheduled(ctx context.Context, limit int) (int, error) {
	return s.repo.PromoteScheduledBatch(ctx, limit)
}

// ---------------------------------------------------------------------
// Cleanup loop helpers (called from api + outbox processes)
// ---------------------------------------------------------------------

func (s *Service) CleanupExpiredIdempotency(ctx context.Context) (int64, error) {
	return s.repo.CleanupExpired(ctx)
}

func (s *Service) CleanupPublishedOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.repo.CleanupPublishedOutbox(ctx, olderThan)
}

func (s *Service) RecoverStuckOutboxLocks(ctx context.Context, timeout time.Duration) (int64, error) {
	return s.repo.RecoverStuckOutboxLocks(ctx, timeout)
}

func (s *Service) RecoverStuckDeliveries(ctx context.Context, timeout time.Duration) (int64, error) {
	return s.repo.RecoverStuckSending(ctx, timeout)
}

// ---------------------------------------------------------------------
// User-facing read APIs
// ---------------------------------------------------------------------

func (s *Service) ListUser(ctx context.Context, userID string, page, perPage int) (notificationdto.UserListResponse, error) {
	const op = "notification_service.ListUser"
	uid, err := uuid.Parse(userID)
	if err != nil {
		return notificationdto.UserListResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user")
	}
	items, total, err := s.repo.ListForUser(ctx, uid, page, perPage)
	if err != nil {
		return notificationdto.UserListResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	data := make([]notificationdto.UserNotificationItem, 0, len(items))
	for _, n := range items {
		action := ""
		if n.ActionURL != nil {
			action = *n.ActionURL
		}
		data = append(data, notificationdto.UserNotificationItem{
			ID:        n.ID.String(),
			Title:     n.Title,
			Message:   n.Message,
			Type:      n.Type,
			ActionURL: action,
			IsRead:    n.ReadAt != nil,
			CreatedAt: n.CreatedAt,
		})
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	return notificationdto.UserListResponse{
		Data: data,
		Meta: notificationdto.ListMeta{Page: page, PerPage: perPage, Total: total},
	}, nil
}

func (s *Service) UnreadCount(ctx context.Context, userID string) (notificationdto.UnreadCountResponse, error) {
	const op = "notification_service.UnreadCount"
	uid, err := uuid.Parse(userID)
	if err != nil {
		return notificationdto.UnreadCountResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user")
	}
	count, err := s.repo.UnreadCount(ctx, uid)
	if err != nil {
		return notificationdto.UnreadCountResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return notificationdto.UnreadCountResponse{Count: count}, nil
}

func (s *Service) MarkRead(ctx context.Context, userID, id string) error {
	const op = "notification_service.MarkRead"
	uid, err := uuid.Parse(userID)
	if err != nil {
		return apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user")
	}
	nid, err := uuid.Parse(id)
	if err != nil {
		return apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid id")
	}
	if err := s.repo.MarkRead(ctx, uid, nid); err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return apperr.New(op).WithKind(apperr.KindNotFound).WithMessage("notification not found")
		}
		return apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return nil
}

func (s *Service) MarkAllRead(ctx context.Context, userID string) error {
	const op = "notification_service.MarkAllRead"
	uid, err := uuid.Parse(userID)
	if err != nil {
		return apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid user")
	}
	if _, err := s.repo.MarkAllRead(ctx, uid); err != nil {
		return apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return nil
}

// ---------------------------------------------------------------------
// Admin read APIs
// ---------------------------------------------------------------------

func (s *Service) AdminList(ctx context.Context, page, perPage int) (notificationdto.AdminListResponse, error) {
	const op = "notification_service.AdminList"
	items, total, err := s.repo.ListAdminBatches(ctx, page, perPage)
	if err != nil {
		return notificationdto.AdminListResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	data := make([]notificationdto.AdminListItem, 0, len(items))
	for _, b := range items {
		channels := make([]string, 0, len(b.Channels))
		for _, c := range b.Channels {
			channels = append(channels, string(c))
		}
		item := notificationdto.AdminListItem{
			ID:              b.ID.String(),
			Title:           b.Title,
			Message:         b.Message,
			Type:            b.Type,
			Priority:        string(b.Priority),
			Channels:        channels,
			Status:          string(b.Status),
			RecipientsCount: b.RecipientsCount,
			SuccessCount:    b.SuccessCount,
			FailedCount:     b.FailedCount,
			CreatedAt:       b.CreatedAt,
			ScheduledAt:     b.ScheduledAt,
		}
		if b.BatchID != nil {
			item.BatchID = b.BatchID.String()
		}
		if b.CreatedBy != nil {
			item.CreatedBy = b.CreatedBy.String()
		}
		data = append(data, item)
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	return notificationdto.AdminListResponse{
		Data: data,
		Meta: notificationdto.ListMeta{Page: page, PerPage: perPage, Total: total},
	}, nil
}

func (s *Service) AdminGet(ctx context.Context, id string) (notificationdto.AdminDetailResponse, error) {
	const op = "notification_service.AdminGet"
	nid, err := uuid.Parse(id)
	if err != nil {
		return notificationdto.AdminDetailResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid id")
	}
	n, err := s.repo.GetByID(ctx, nid)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return notificationdto.AdminDetailResponse{}, apperr.New(op).WithKind(apperr.KindNotFound).WithMessage("notification not found")
		}
		return notificationdto.AdminDetailResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	deliveries, err := s.repo.ListDeliveries(ctx, n.ID)
	if err != nil {
		return notificationdto.AdminDetailResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}

	resp := notificationdto.AdminDetailResponse{
		ID:          n.ID.String(),
		UserID:      n.UserID.String(),
		Title:       n.Title,
		Message:     n.Message,
		Type:        n.Type,
		Priority:    string(n.Priority),
		Channels:    channelStrings(n.Channels),
		Status:      string(n.Status),
		Payload:     n.Payload,
		Locale:      n.Locale,
		CreatedAt:   n.CreatedAt,
		ScheduledAt: n.ScheduledAt,
		Deliveries:  deliveryItems(deliveries),
	}
	if n.TemplateCode != nil {
		resp.TemplateCode = *n.TemplateCode
	}
	if n.BatchID != nil {
		resp.BatchID = n.BatchID.String()
	}
	if n.ActionURL != nil {
		resp.ActionURL = *n.ActionURL
	}
	if n.CreatedBy != nil {
		resp.CreatedBy = n.CreatedBy.String()
	}
	return resp, nil
}

// AdminGetBatch returns every notification belonging to a batch, along
// with its per-recipient deliveries.
func (s *Service) AdminGetBatch(ctx context.Context, batchID string) (notificationdto.AdminBatchDetail, error) {
	const op = "notification_service.AdminGetBatch"
	bid, err := uuid.Parse(batchID)
	if err != nil {
		return notificationdto.AdminBatchDetail{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid batch_id")
	}
	members, err := s.repo.GetBatch(ctx, bid)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return notificationdto.AdminBatchDetail{}, apperr.New(op).WithKind(apperr.KindNotFound).WithMessage("batch not found")
		}
		return notificationdto.AdminBatchDetail{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}

	detail := notificationdto.AdminBatchDetail{
		BatchID: batchID,
	}
	if len(members) > 0 {
		first := members[0]
		detail.Title = first.Title
		detail.Message = first.Message
		detail.Priority = string(first.Priority)
		detail.Channels = channelStrings(first.Channels)
		detail.CreatedAt = first.CreatedAt
		detail.ScheduledAt = first.ScheduledAt
	}

	for _, n := range members {
		detail.RecipientsCount++
		switch n.Status {
		case entity.StatusSent:
			detail.SuccessCount++
		case entity.StatusFailed, entity.StatusPartiallyFailed:
			detail.FailedCount++
		}
		deliveries, err := s.repo.ListDeliveries(ctx, n.ID)
		if err != nil {
			return notificationdto.AdminBatchDetail{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
		}
		detail.Members = append(detail.Members, notificationdto.AdminBatchMember{
			ID:         n.ID.String(),
			UserID:     n.UserID.String(),
			Status:     string(n.Status),
			CreatedAt:  n.CreatedAt,
			ReadAt:     n.ReadAt,
			Deliveries: deliveryItems(deliveries),
		})
	}
	return detail, nil
}

// ---------------------------------------------------------------------
// Admin template CRUD
// ---------------------------------------------------------------------

func (s *Service) AdminCreateTemplate(ctx context.Context, req notificationdto.TemplateCreateRequest) (notificationdto.TemplateResponse, error) {
	const op = "notification_service.AdminCreateTemplate"

	fields := map[string]string{}
	if req.Code == "" {
		fields["code"] = "validation.required.code"
	}
	if req.Channel == "" {
		fields["channel"] = "validation.required.channel"
	}
	if req.Body == "" {
		fields["body"] = "validation.required.body"
	}
	if len(fields) > 0 {
		return notificationdto.TemplateResponse{}, &apperr.Error{Fields: fields}
	}

	locale := req.Locale
	if locale == "" {
		locale = entity.DefaultLocale
	}
	priority := entity.Priority(req.DefaultPriority)
	if priority == "" {
		priority = entity.PriorityNormal
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var subject *string
	if req.Subject != "" {
		subject = &req.Subject
	}

	t, err := s.repo.CreateTemplate(ctx, entity.Template{
		Code:            req.Code,
		Locale:          locale,
		Channel:         entity.Channel(req.Channel),
		Subject:         subject,
		Body:            req.Body,
		DefaultPriority: priority,
		Enabled:         enabled,
	})
	if err != nil {
		if errors.Is(err, sharederrors.ErrAlreadyExists) {
			return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("template already exists for code/locale/channel")
		}
		return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return toTemplateResponse(t), nil
}

func (s *Service) AdminListTemplates(ctx context.Context, filter notificationdto.TemplateListFilter) (notificationdto.TemplateListResponse, error) {
	const op = "notification_service.AdminListTemplates"
	items, total, err := s.repo.ListTemplates(ctx, filter.Code, filter.Locale, filter.Channel, filter.Enabled, filter.Page, filter.PerPage)
	if err != nil {
		return notificationdto.TemplateListResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	data := make([]notificationdto.TemplateResponse, 0, len(items))
	for _, t := range items {
		data = append(data, toTemplateResponse(t))
	}
	page, perPage := filter.Page, filter.PerPage
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	return notificationdto.TemplateListResponse{
		Data: data,
		Meta: notificationdto.ListMeta{Page: page, PerPage: perPage, Total: total},
	}, nil
}

func (s *Service) AdminGetTemplate(ctx context.Context, id string) (notificationdto.TemplateResponse, error) {
	const op = "notification_service.AdminGetTemplate"
	tid, err := uuid.Parse(id)
	if err != nil {
		return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid id")
	}
	t, err := s.repo.GetTemplateByID(ctx, tid)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindNotFound).WithMessage("template not found")
		}
		return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return toTemplateResponse(t), nil
}

func (s *Service) AdminUpdateTemplate(ctx context.Context, id string, req notificationdto.TemplateUpdateRequest) (notificationdto.TemplateResponse, error) {
	const op = "notification_service.AdminUpdateTemplate"
	tid, err := uuid.Parse(id)
	if err != nil {
		return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid id")
	}
	t, err := s.repo.UpdateTemplate(ctx, tid, req.Subject, req.Body, req.DefaultPriority)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindNotFound).WithMessage("template not found")
		}
		return notificationdto.TemplateResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return toTemplateResponse(t), nil
}

func (s *Service) AdminSetTemplateStatus(ctx context.Context, id string, enabled bool) error {
	const op = "notification_service.AdminSetTemplateStatus"
	tid, err := uuid.Parse(id)
	if err != nil {
		return apperr.New(op).WithKind(apperr.KindInvalid).WithMessage("invalid id")
	}
	if err := s.repo.SetTemplateEnabled(ctx, tid, enabled); err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return apperr.New(op).WithKind(apperr.KindNotFound).WithMessage("template not found")
		}
		return apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	return nil
}

// ---------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------

func (s *Service) checkDirectRateLimit(ctx context.Context, recipient string) error {
	const op = "notification_service.checkDirectRateLimit"
	limit := s.directRateLimitPerMin
	if limit <= 0 || recipient == "" {
		return nil
	}
	count, err := s.repo.CountRecentDirectByRecipient(ctx, recipient, time.Minute)
	if err != nil {
		return apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	if count >= int64(limit) {
		return apperr.New(op).
			WithKind(apperr.KindTooManyRequests).
			WithMessage("direct notification rate limit exceeded")
	}
	return nil
}

func toChannels(in []string) []entity.Channel {
	out := make([]entity.Channel, 0, len(in))
	for _, c := range in {
		out = append(out, entity.Channel(c))
	}
	return out
}

func channelStrings(channels []entity.Channel) []string {
	out := make([]string, 0, len(channels))
	for _, c := range channels {
		out = append(out, string(c))
	}
	return out
}

func deliveryItems(deliveries []entity.Delivery) []notificationdto.DeliveryItem {
	out := make([]notificationdto.DeliveryItem, 0, len(deliveries))
	for _, d := range deliveries {
		errMsg := ""
		if d.Error != nil {
			errMsg = *d.Error
		}
		out = append(out, notificationdto.DeliveryItem{
			ID:        d.ID.String(),
			Channel:   string(d.Channel),
			Provider:  d.Provider,
			Status:    string(d.Status),
			Attempts:  d.Attempts,
			Error:     errMsg,
			SentAt:    d.SentAt,
			CreatedAt: d.CreatedAt,
		})
	}
	return out
}

func makeDeliveries(channels []entity.Channel) []entity.Delivery {
	out := make([]entity.Delivery, 0, len(channels))
	for _, ch := range channels {
		out = append(out, entity.Delivery{
			ID:       uuid.New(),
			Channel:  ch,
			Provider: "",
			Status:   entity.DeliveryPending,
			Attempts: 0,
		})
	}
	return out
}

// buildOutboxEvents creates one pending outbox row per delivery, routed by
// notification priority (matches queue.QueueName's "notification.<priority>"
// convention).
func buildOutboxEvents(notificationID uuid.UUID, priority entity.Priority, deliveries []entity.Delivery) ([]entity.OutboxEvent, error) {
	out := make([]entity.OutboxEvent, 0, len(deliveries))
	for _, d := range deliveries {
		job := notificationdto.QueueJob{
			NotificationID: notificationID.String(),
			DeliveryID:     d.ID.String(),
			Channel:        string(d.Channel),
			Attempt:        0,
		}
		payload, err := json.Marshal(job)
		if err != nil {
			return nil, err
		}
		out = append(out, entity.OutboxEvent{
			AggregateID: notificationID,
			DeliveryID:  d.ID,
			EventType:   "notification.delivery.created",
			RoutingKey:  "notification." + string(priority),
			Payload:     payload,
			Status:      entity.OutboxPending,
		})
	}
	return out, nil
}

// filterChannelsByContacts drops channels the user cannot receive: opted
// out via preferences, or missing/unverified contact info for
// email/sms/whatsapp. in_app and push always pass through.
func filterChannelsByContacts(channels []entity.Channel, contacts notificationdto.Contacts) []entity.Channel {
	out := make([]entity.Channel, 0, len(channels))
	for _, ch := range channels {
		if pref, ok := contacts.Preferences[string(ch)]; ok && !pref {
			continue
		}
		switch ch {
		case entity.ChannelEmail:
			if contacts.Email == "" || !contacts.EmailVerified {
				continue
			}
		case entity.ChannelSMS, entity.ChannelWhatsApp:
			if contacts.Phone == "" || !contacts.PhoneVerified {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

// loadTemplatesForChannels loads the best enabled template per channel,
// silently dropping channels without a matching template. It returns the
// loaded templates alongside the surviving channel list (same order).
func (s *Service) loadTemplatesForChannels(ctx context.Context, code, locale string, channels []entity.Channel) ([]entity.Template, []entity.Channel, error) {
	templates := make([]entity.Template, 0, len(channels))
	kept := make([]entity.Channel, 0, len(channels))
	for _, ch := range channels {
		t, err := s.repo.GetEnabledTemplate(ctx, code, locale, string(ch))
		if err != nil {
			if errors.Is(err, sharederrors.ErrNotFound) {
				continue
			}
			return nil, nil, err
		}
		templates = append(templates, t)
		kept = append(kept, ch)
	}
	return templates, kept, nil
}

// validateTemplateVariables dry-renders every template's subject+body with
// the supplied variables, returning field errors keyed by channel for any
// missing variables. It does not persist rendered output — final,
// per-channel rendering happens in the worker at send time.
func validateTemplateVariables(templates []entity.Template, vars map[string]any) map[string]string {
	fields := map[string]string{}
	for _, t := range templates {
		subject := ""
		if t.Subject != nil {
			subject = *t.Subject
		}
		if _, _, err := notificationtemplate.RenderPair(subject, t.Body, vars); err != nil {
			notificationmetrics.IncTemplateRenderError(string(t.Channel))
			var missErr *notificationtemplate.MissingVariableError
			if errors.As(err, &missErr) {
				fields["variables["+string(t.Channel)+"]"] = "validation.missing." + joinComma(missErr.Variables)
			} else {
				fields["variables["+string(t.Channel)+"]"] = "validation.invalid.variables"
			}
		}
	}
	return fields
}

func resolvePriority(requested string, templates []entity.Template) entity.Priority {
	if requested != "" {
		return entity.Priority(requested)
	}
	for _, want := range channelPriorityOrder {
		for _, t := range templates {
			if t.Channel == want && t.DefaultPriority != "" {
				return t.DefaultPriority
			}
		}
	}
	if len(templates) > 0 && templates[0].DefaultPriority != "" {
		return templates[0].DefaultPriority
	}
	return entity.PriorityNormal
}

// primaryTitleMessage renders the highest-priority channel's template to
// fill the notification's legacy single title/message columns (used for
// admin/user display only; the worker re-renders per channel at send
// time).
func primaryTitleMessage(templates []entity.Template, vars map[string]any, fallback string) (string, string) {
	byChannel := make(map[entity.Channel]entity.Template, len(templates))
	for _, t := range templates {
		byChannel[t.Channel] = t
	}
	for _, ch := range channelPriorityOrder {
		t, ok := byChannel[ch]
		if !ok {
			continue
		}
		subject := ""
		if t.Subject != nil {
			subject = *t.Subject
		}
		renderedSubject, renderedBody, err := notificationtemplate.RenderPair(subject, t.Body, vars)
		if err != nil {
			continue
		}
		if renderedSubject == "" {
			renderedSubject = fallback
		}
		return renderedSubject, renderedBody
	}
	return fallback, ""
}

func joinComma(in []string) string {
	out := ""
	for i, v := range in {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

func toTemplateResponse(t entity.Template) notificationdto.TemplateResponse {
	subject := ""
	if t.Subject != nil {
		subject = *t.Subject
	}
	return notificationdto.TemplateResponse{
		ID:              t.ID.String(),
		Code:            t.Code,
		Locale:          t.Locale,
		Channel:         string(t.Channel),
		Subject:         subject,
		Body:            t.Body,
		DefaultPriority: string(t.DefaultPriority),
		Enabled:         t.Enabled,
		Version:         t.Version,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}

func hashJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
