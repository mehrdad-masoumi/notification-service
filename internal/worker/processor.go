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
	publisher  notificationcontract.IFPublisher
	maxRetries int
	dlq        *queue.Client
}

func NewProcessor(
	repo *notificationrepo.Repository,
	registry notificationcontract.IFProviderRegistry,
	publisher notificationcontract.IFPublisher,
	maxRetries int,
	dlq *queue.Client,
) *Processor {
	return &Processor{
		repo:       repo,
		registry:   registry,
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

	// Atomically transition pending/failed -> sending. If the delivery is
	// already sending/sent/delivered/permanent_failed, ClaimDelivery
	// returns ErrNotFound and we skip: another worker (or a previous
	// attempt) already owns/finished it.
	delivery, err := p.repo.ClaimDelivery(ctx, deliveryID)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return nil
		}
		notificationmetrics.IncError("claim_delivery")
		return err
	}

	if delivery.NotificationID != notificationID {
		log.Printf("delivery %s does not belong to notification %s in job; skipping", deliveryID, notificationID)
		notificationmetrics.IncError("delivery_notification_mismatch")
		return p.failPermanentClaimed(ctx, delivery, "notification_id mismatch")
	}

	n, err := p.repo.GetByID(ctx, notificationID)
	if err != nil {
		if errors.Is(err, sharederrors.ErrNotFound) {
			return nil
		}
		notificationmetrics.IncError("get_notification")
		return err
	}

	if err := p.repo.UpdateNotificationStatus(ctx, n.ID, entity.StatusProcessing); err != nil {
		notificationmetrics.IncError("update_notification_status")
		return err
	}

	provider, err := p.registry.Get(string(delivery.Channel))
	if err != nil {
		notificationmetrics.ObserveDelivery(string(delivery.Channel), "permanent_error", startedAt)
		return p.failPermanent(ctx, n, delivery, err.Error())
	}

	to, resolveErr := p.resolveRecipient(n, delivery.Channel)
	if resolveErr != nil {
		if providerrerrors.IsPermanent(resolveErr) {
			notificationmetrics.ObserveDelivery(string(delivery.Channel), "permanent_error", startedAt)
			return p.failPermanent(ctx, n, delivery, sanitizeErr(resolveErr))
		}
		notificationmetrics.ObserveDelivery(string(delivery.Channel), "temporary_error", startedAt)
		return p.failTemporary(ctx, n, delivery, job, attempt, sanitizeErr(resolveErr))
	}

	delivery.Provider = provider.Name()
	if err := p.repo.UpdateDelivery(ctx, delivery); err != nil {
		notificationmetrics.IncError("update_delivery")
		return err
	}

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
		IdempotencyKey: delivery.ID.String(),
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
	notificationmetrics.IncDelivery(string(delivery.Channel), "success")
	return p.repo.RecomputeNotificationStatus(ctx, n.ID)
}

func (p *Processor) resolveRecipient(n entity.Notification, channel entity.Channel) (string, error) {
	switch channel {
	case entity.ChannelInApp, entity.ChannelPush:
		return n.UserID.String(), nil
	case entity.ChannelEmail:
		if n.Email != nil && *n.Email != "" {
			return *n.Email, nil
		}
		return "", providerrerrors.Permanent("email not available for notification", nil)
	case entity.ChannelSMS, entity.ChannelWhatsApp:
		if n.Phone != nil && *n.Phone != "" {
			return *n.Phone, nil
		}
		return "", providerrerrors.Permanent("phone not available for notification", nil)
	default:
		return "", providerrerrors.Permanent("unsupported channel", nil)
	}
}

func (p *Processor) failPermanent(ctx context.Context, n entity.Notification, delivery entity.Delivery, msg string) error {
	delivery.Status = entity.DeliveryPermanentFailed
	delivery.Error = &msg
	if err := p.repo.UpdateDelivery(ctx, delivery); err != nil {
		notificationmetrics.IncError("update_delivery")
		return err
	}
	if err := p.repo.RecomputeNotificationStatus(ctx, n.ID); err != nil {
		notificationmetrics.IncError("recompute_status")
		return err
	}
	notificationmetrics.IncError("delivery_permanent")
	notificationmetrics.IncDelivery(string(delivery.Channel), "permanent_failed")
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

// failPermanentClaimed handles the (should-be-impossible) case where a
// claimed delivery's notification_id does not match the job: we still
// hold the "sending" claim, so mark it permanently failed directly rather
// than looking up the (wrong) notification.
func (p *Processor) failPermanentClaimed(ctx context.Context, delivery entity.Delivery, msg string) error {
	delivery.Status = entity.DeliveryPermanentFailed
	delivery.Error = &msg
	return p.repo.UpdateDelivery(ctx, delivery)
}

func (p *Processor) failTemporary(ctx context.Context, n entity.Notification, delivery entity.Delivery, job notificationdto.QueueJob, attempt int, msg string) error {
	nextAttempt := attempt + 1
	delivery.Status = entity.DeliveryFailed
	delivery.Error = &msg
	if err := p.repo.UpdateDelivery(ctx, delivery); err != nil {
		notificationmetrics.IncError("update_delivery")
		return err
	}

	if nextAttempt >= p.maxRetries {
		return p.failPermanent(ctx, n, delivery, "max retries exceeded: "+msg)
	}

	job.Attempt = nextAttempt
	notificationmetrics.IncRetry(string(delivery.Channel), string(n.Priority))
	notificationmetrics.IncDelivery(string(delivery.Channel), "temporary_failed")
	if err := p.publisher.Publish(ctx, n.Priority, job); err != nil {
		notificationmetrics.IncError("retry_publish")
		return err
	}
	return p.repo.RecomputeNotificationStatus(ctx, n.ID)
}

// RecoverStuckDeliveries resets deliveries stuck in 'sending' longer than
// timeout back to 'failed' so a future retry/claim can pick them up again
// (e.g. after a worker crash mid-send). Intended to be called
// periodically from cmd/worker.
func (p *Processor) RecoverStuckDeliveries(ctx context.Context, timeout time.Duration) (int64, error) {
	return p.repo.RecoverStuckSending(ctx, timeout)
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
