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

	notificationcontract "notification-service/internal/notification/contract"
	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
	notificationmetrics "notification-service/internal/notification/metrics"
	notificationrepo "notification-service/internal/notification/repository"
	notificationvalidator "notification-service/internal/notification/validator"
	"notification-service/pkg/sharederrors"

	"github.com/mehrdad-masoumi/go-packages/apperr"
)

type Service struct {
	repo      *notificationrepo.Repository
	validator notificationvalidator.Validator
	publisher notificationcontract.IFPublisher
}

func New(
	repo *notificationrepo.Repository,
	validator notificationvalidator.Validator,
	publisher notificationcontract.IFPublisher,
) *Service {
	return &Service{repo: repo, validator: validator, publisher: publisher}
}

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
		created, outDeliveries, err := s.repo.CreateNotificationWithDeliveries(ctx, n, deliveries)
		if err != nil {
			notificationmetrics.IncError("admin_create")
			notificationmetrics.IncAccepted("admin", string(priority), "error")
			return notificationdto.AcceptedResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to create notification")
		}

		if !scheduled {
			if err := s.enqueue(ctx, created, outDeliveries); err != nil {
				notificationmetrics.IncError("admin_enqueue")
				notificationmetrics.IncAccepted("admin", string(priority), "error")
				return notificationdto.AcceptedResponse{}, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to enqueue")
			}
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

func (s *Service) InternalCreate(ctx context.Context, req notificationdto.InternalCreateRequest) (notificationdto.AcceptedResponse, int, error) {
	const op = "notification_service.InternalCreate"

	if fields, err := s.validator.ValidateInternalCreate(req); err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusUnprocessableEntity, &apperr.Error{Fields: fields}
	}

	hash := hashRequest(req)
	rec, existed, err := s.repo.BeginIdempotency(ctx, req.IdempotencyKey, op, hash)
	if err != nil {
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
	}
	if existed {
		if rec.Status == "succeeded" && len(rec.ResponseBody) > 0 {
			var resp notificationdto.AcceptedResponse
			_ = json.Unmarshal(rec.ResponseBody, &resp)
			code := rec.ResponseCode
			if code == 0 {
				code = http.StatusAccepted
			}
			return resp, code, nil
		}
		if rec.Status == "processing" {
			return notificationdto.AcceptedResponse{}, http.StatusConflict, apperr.New(op).
				WithKind(apperr.KindInvalid).
				WithMessage("request already processing")
		}
	}

	// Also check notification table for same key (unique).
	if existing, err := s.repo.GetByIdempotencyKey(ctx, req.IdempotencyKey); err == nil {
		resp := notificationdto.AcceptedResponse{ID: existing.ID.String(), Status: "accepted"}
		_ = s.repo.CompleteIdempotency(ctx, req.IdempotencyKey, http.StatusAccepted, resp)
		return resp, http.StatusAccepted, nil
	} else if !errors.Is(err, sharederrors.ErrNotFound) {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err)
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
	created, outDeliveries, err := s.repo.CreateNotificationWithDeliveries(ctx, n, deliveries)
	if err != nil {
		_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
		notificationmetrics.IncError("internal_create")
		notificationmetrics.IncAccepted("internal", string(priority), "error")
		return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to create notification")
	}

	if !scheduled {
		if err := s.enqueue(ctx, created, outDeliveries); err != nil {
			_ = s.repo.FailIdempotency(ctx, req.IdempotencyKey)
			notificationmetrics.IncError("internal_enqueue")
			notificationmetrics.IncAccepted("internal", string(priority), "error")
			return notificationdto.AcceptedResponse{}, http.StatusInternalServerError, apperr.New(op).WithKind(apperr.KindUnexpected).WithErr(err).WithMessage("failed to enqueue")
		}
	}

	result := "success"
	resp := notificationdto.AcceptedResponse{ID: created.ID.String(), Status: "accepted"}
	if scheduled {
		resp.Status = "scheduled"
		result = "scheduled"
	}
	notificationmetrics.IncAccepted("internal", string(priority), result)
	_ = s.repo.CompleteIdempotency(ctx, req.IdempotencyKey, http.StatusAccepted, resp)
	return resp, http.StatusAccepted, nil
}

func (s *Service) enqueue(ctx context.Context, n entity.Notification, deliveries []entity.Delivery) error {
	for _, d := range deliveries {
		job := notificationdto.QueueJob{
			NotificationID: n.ID.String(),
			DeliveryID:     d.ID.String(),
			Channel:        string(d.Channel),
			Attempt:        0,
		}
		if err := s.publisher.Publish(ctx, n.Priority, job); err != nil {
			notificationmetrics.IncEnqueued(string(d.Channel), string(n.Priority), "error")
			notificationmetrics.IncError("enqueue")
			return err
		}
		notificationmetrics.IncEnqueued(string(d.Channel), string(n.Priority), "success")
	}
	return s.repo.UpdateNotificationStatus(ctx, n.ID, entity.StatusQueued)
}

func (s *Service) EnqueueExisting(ctx context.Context, n entity.Notification) error {
	deliveries, err := s.repo.ListDeliveries(ctx, n.ID)
	if err != nil {
		return err
	}
	return s.enqueue(ctx, n, deliveries)
}

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
			item.ID = b.BatchID.String()
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

	channels := make([]string, 0, len(n.Channels))
	for _, c := range n.Channels {
		channels = append(channels, string(c))
	}
	dItems := make([]notificationdto.DeliveryItem, 0, len(deliveries))
	for _, d := range deliveries {
		errMsg := ""
		if d.Error != nil {
			errMsg = *d.Error
		}
		dItems = append(dItems, notificationdto.DeliveryItem{
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

	resp := notificationdto.AdminDetailResponse{
		ID:          n.ID.String(),
		UserID:      n.UserID.String(),
		Title:       n.Title,
		Message:     n.Message,
		Type:        n.Type,
		Priority:    string(n.Priority),
		Channels:    channels,
		Status:      string(n.Status),
		Payload:     n.Payload,
		CreatedAt:   n.CreatedAt,
		ScheduledAt: n.ScheduledAt,
		Deliveries:  dItems,
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

func (s *Service) Repo() *notificationrepo.Repository {
	return s.repo
}

func toChannels(in []string) []entity.Channel {
	out := make([]entity.Channel, 0, len(in))
	for _, c := range in {
		out = append(out, entity.Channel(c))
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

func hashRequest(req notificationdto.InternalCreateRequest) string {
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
