package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	notificationcontract "notification-service/internal/notification/contract"
	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
	notificationmetrics "notification-service/internal/notification/metrics"
	notificationrepo "notification-service/internal/notification/repository"
	providerrerrors "notification-service/internal/provider"
	"notification-service/internal/queue"
	"notification-service/pkg/sharederrors"
)

type Processor struct {
	repo       *notificationrepo.Repository
	registry   notificationcontract.IFProviderRegistry
	users      notificationcontract.IFUserDirectory
	publisher  notificationcontract.IFPublisher
	maxRetries int
	dlq        *queue.Client
}

func NewProcessor(
	repo *notificationrepo.Repository,
	registry notificationcontract.IFProviderRegistry,
	users notificationcontract.IFUserDirectory,
	publisher notificationcontract.IFPublisher,
	maxRetries int,
	dlq *queue.Client,
) *Processor {
	return &Processor{
		repo:       repo,
		registry:   registry,
		users:      users,
		publisher:  publisher,
		maxRetries: maxRetries,
		dlq:        dlq,
	}
}

func (p *Processor) Handle(ctx context.Context, job notificationdto.QueueJob, attempt int) error {
	startedAt := time.Now()
	deliveryID, err := uuid.Parse(job.DeliveryID)
	if err != nil {
		log.Printf("invalid delivery_id in job")
		return nil
	}
	notificationID, err := uuid.Parse(job.NotificationID)
	if err != nil {
		log.Printf("invalid notification_id in job")
		return nil
	}

	delivery, err := p.repo.GetDelivery(ctx, deliveryID)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return nil
		}
		notificationmetrics.IncError("get_delivery")
		return err
	}

	if delivery.Status == entity.DeliverySent || delivery.Status == entity.DeliveryDelivered {
		return nil
	}
	if delivery.Status == entity.DeliveryPermanentFailed {
		return nil
	}

	n, err := p.repo.GetByID(ctx, notificationID)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return nil
		}
		notificationmetrics.IncError("get_notification")
		return err
	}

	_ = p.repo.UpdateNotificationStatus(ctx, n.ID, entity.StatusProcessing)

	provider, err := p.registry.Get(string(delivery.Channel))
	if err != nil {
		notificationmetrics.ObserveDelivery(string(delivery.Channel), "permanent_error", startedAt)
		return p.failPermanent(ctx, n, delivery, err.Error())
	}

	to, resolveErr := p.resolveRecipient(ctx, n, delivery.Channel)
	if resolveErr != nil {
		if providerrerrors.IsPermanent(resolveErr) {
			notificationmetrics.ObserveDelivery(string(delivery.Channel), "permanent_error", startedAt)
			return p.failPermanent(ctx, n, delivery, sanitizeErr(resolveErr))
		}
		notificationmetrics.ObserveDelivery(string(delivery.Channel), "temporary_error", startedAt)
		return p.failTemporary(ctx, n, delivery, job, attempt, sanitizeErr(resolveErr))
	}

	delivery.Status = entity.DeliverySending
	delivery.Attempts = attempt + 1
	delivery.Provider = provider.Name()
	_ = p.repo.UpdateDelivery(ctx, delivery)

	actionURL := ""
	if n.ActionURL != nil {
		actionURL = *n.ActionURL
	}
	var payload map[string]any
	if len(n.Payload) > 0 {
		_ = json.Unmarshal(n.Payload, &payload)
	}

	_, sendErr := provider.Send(ctx, notificationcontract.SendRequest{
		NotificationID: n.ID.String(),
		DeliveryID:     delivery.ID.String(),
		UserID:         n.UserID.String(),
		Channel:        string(delivery.Channel),
		To:             to,
		Title:          n.Title,
		Message:        n.Message,
		ActionURL:      actionURL,
		Payload:        payload,
	})
	if sendErr != nil {
		if providerrerrors.IsPermanent(sendErr) || !providerrerrors.IsTemporary(sendErr) {
			notificationmetrics.ObserveDelivery(string(delivery.Channel), "permanent_error", startedAt)
			return p.failPermanent(ctx, n, delivery, sanitizeErr(sendErr))
		}
		notificationmetrics.ObserveDelivery(string(delivery.Channel), "temporary_error", startedAt)
		return p.failTemporary(ctx, n, delivery, job, attempt, sanitizeErr(sendErr))
	}

	now := time.Now().UTC()
	delivery.Status = entity.DeliverySent
	delivery.SentAt = &now
	delivery.Error = nil
	if err := p.repo.UpdateDelivery(ctx, delivery); err != nil {
		notificationmetrics.IncError("update_delivery")
		return err
	}
	notificationmetrics.ObserveDelivery(string(delivery.Channel), "success", startedAt)
	return p.repo.RecomputeNotificationStatus(ctx, n.ID)
}

func (p *Processor) resolveRecipient(ctx context.Context, n entity.Notification, channel entity.Channel) (string, error) {
	switch channel {
	case entity.ChannelInApp, entity.ChannelPush:
		return n.UserID.String(), nil
	case entity.ChannelEmail:
		if n.Email != nil && *n.Email != "" {
			return *n.Email, nil
		}
		contacts, err := p.users.Resolve(ctx, n.UserID.String())
		if err != nil {
			return "", err
		}
		if contacts.Email == "" {
			return "", providerrerrors.Permanent("email not available for user", nil)
		}
		return contacts.Email, nil
	case entity.ChannelSMS, entity.ChannelWhatsApp:
		if n.Phone != nil && *n.Phone != "" {
			return *n.Phone, nil
		}
		contacts, err := p.users.Resolve(ctx, n.UserID.String())
		if err != nil {
			return "", err
		}
		if contacts.Phone == "" {
			return "", providerrerrors.Permanent("phone not available for user", nil)
		}
		return contacts.Phone, nil
	default:
		return "", providerrerrors.Permanent("unsupported channel", nil)
	}
}

func (p *Processor) failPermanent(ctx context.Context, n entity.Notification, delivery entity.Delivery, msg string) error {
	delivery.Status = entity.DeliveryPermanentFailed
	delivery.Error = &msg
	if err := p.repo.UpdateDelivery(ctx, delivery); err != nil {
		return err
	}
	_ = p.repo.RecomputeNotificationStatus(ctx, n.ID)
	notificationmetrics.IncError("delivery_permanent")
	if p.dlq != nil {
		_ = p.dlq.PublishToDLQ(ctx, n.Priority, notificationdto.QueueJob{
			NotificationID: n.ID.String(),
			DeliveryID:     delivery.ID.String(),
			Channel:        string(delivery.Channel),
			Attempt:        delivery.Attempts,
		})
		notificationmetrics.IncDLQ(string(n.Priority))
	}
	return nil
}

func (p *Processor) failTemporary(ctx context.Context, n entity.Notification, delivery entity.Delivery, job notificationdto.QueueJob, attempt int, msg string) error {
	nextAttempt := attempt + 1
	delivery.Status = entity.DeliveryFailed
	delivery.Error = &msg
	delivery.Attempts = nextAttempt
	_ = p.repo.UpdateDelivery(ctx, delivery)

	if nextAttempt >= p.maxRetries {
		return p.failPermanent(ctx, n, delivery, "max retries exceeded: "+msg)
	}

	job.Attempt = nextAttempt
	notificationmetrics.IncRetry(string(delivery.Channel), string(n.Priority))
	if err := p.publisher.Publish(ctx, n.Priority, job); err != nil {
		notificationmetrics.IncError("retry_publish")
		return err
	}
	_ = p.repo.RecomputeNotificationStatus(ctx, n.ID)
	return nil
}

func sanitizeErr(err error) string {
	if err == nil {
		return "unknown error"
	}
	msg := err.Error()
	if len(msg) > 500 {
		return msg[:500]
	}
	return msg
}
