// Package outbox implements the publisher side of the transactional
// outbox pattern: it claims pending notification_outbox rows (written in
// the same DB transaction as notifications/deliveries) and publishes them
// to RabbitMQ with publisher confirms, so the DB write and the broker
// publish never fall out of sync even across process crashes.
package outbox

import (
	"context"
	"log"
	"os"
	"time"

	"notification-service/config"
	"notification-service/internal/notification/entity"
	notificationmetrics "notification-service/internal/notification/metrics"
	notificationrepo "notification-service/internal/notification/repository"
	notificationservice "notification-service/internal/notification/service"
)

// Publisher is the interface satisfied by queue.Client, kept minimal so
// this package does not need to import internal/queue's amqp types.
type Publisher interface {
	PublishWithConfirm(ctx context.Context, routingKey string, body []byte) error
}

type Config struct {
	BatchSize        int
	PollInterval     time.Duration
	LockTimeout      time.Duration
	MaxAttempts      int
	RecoveryInterval time.Duration
	BaseBackoff      time.Duration
	MaxBackoff       time.Duration
}

func ConfigFromOutbox(cfg config.Outbox) Config {
	return Config{
		BatchSize:        cfg.BatchSize,
		PollInterval:     time.Duration(cfg.PollIntervalMS) * time.Millisecond,
		LockTimeout:      time.Duration(cfg.LockTimeoutSeconds) * time.Second,
		MaxAttempts:      cfg.MaxAttempts,
		RecoveryInterval: 30 * time.Second,
		BaseBackoff:      time.Second,
		MaxBackoff:       5 * time.Minute,
	}
}

type PublisherService struct {
	repo     *notificationrepo.Repository
	queue    Publisher
	cfg      Config
	lockedBy string
}

func New(repo *notificationrepo.Repository, queueClient Publisher, cfg Config) *PublisherService {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "outbox"
	}
	return &PublisherService{
		repo:     repo,
		queue:    queueClient,
		cfg:      cfg,
		lockedBy: hostname,
	}
}

// Run polls for claimable outbox rows and publishes them until ctx is
// cancelled. It also periodically recovers rows stuck in 'publishing'
// (e.g. from a crashed replica) and refreshes gauge metrics.
func (p *PublisherService) Run(ctx context.Context) {
	pollTicker := time.NewTicker(p.cfg.PollInterval)
	defer pollTicker.Stop()
	recoveryTicker := time.NewTicker(p.cfg.RecoveryInterval)
	defer recoveryTicker.Stop()

	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			p.tick(ctx)
		case <-recoveryTicker.C:
			p.recover(ctx)
		}
	}
}

func (p *PublisherService) tick(ctx context.Context) {
	events, err := p.repo.ClaimOutboxBatch(ctx, p.cfg.BatchSize, p.lockedBy)
	if err != nil {
		log.Printf("outbox: claim batch: %v", err)
		notificationmetrics.IncError("outbox_claim")
		return
	}

	for _, e := range events {
		if err := p.publishOne(ctx, e); err != nil {
			p.handleFailure(ctx, e, err)
			continue
		}
		if err := p.repo.MarkOutboxPublished(ctx, e.ID); err != nil {
			log.Printf("outbox: mark published %s: %v", e.ID, err)
			notificationmetrics.IncError("outbox_mark_published")
			continue
		}
		notificationmetrics.IncOutboxPublish("published")
	}

	p.refreshGauges(ctx)
}

func (p *PublisherService) publishOne(ctx context.Context, e entity.OutboxEvent) error {
	pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return p.queue.PublishWithConfirm(pubCtx, e.RoutingKey, e.Payload)
}

func (p *PublisherService) handleFailure(ctx context.Context, e entity.OutboxEvent, pubErr error) {
	attempt := e.Attempts + 1
	msg := pubErr.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}

	if p.cfg.MaxAttempts > 0 && attempt >= p.cfg.MaxAttempts {
		// Give up retrying quickly, but keep the row (status 'failed')
		// for operator visibility instead of losing it silently.
		if err := p.repo.MarkOutboxFailed(ctx, e.ID, "max attempts exceeded: "+msg, time.Now().Add(24*time.Hour)); err != nil {
			log.Printf("outbox: mark exhausted %s: %v", e.ID, err)
		}
		notificationmetrics.IncOutboxPublish("exhausted")
		notificationmetrics.IncError("outbox_publish_exhausted")
		return
	}

	delay := notificationservice.Backoff(attempt, p.cfg.BaseBackoff, p.cfg.MaxBackoff)
	if err := p.repo.MarkOutboxFailed(ctx, e.ID, msg, time.Now().Add(delay)); err != nil {
		log.Printf("outbox: mark failed %s: %v", e.ID, err)
	}
	notificationmetrics.IncOutboxPublish("failed")
	notificationmetrics.IncError("outbox_publish")
}

func (p *PublisherService) recover(ctx context.Context) {
	n, err := p.repo.RecoverStuckOutboxLocks(ctx, p.cfg.LockTimeout)
	if err != nil {
		log.Printf("outbox: recover stuck locks: %v", err)
		notificationmetrics.IncError("outbox_recover")
		return
	}
	if n > 0 {
		log.Printf("outbox: recovered %d stuck locks", n)
	}
}

func (p *PublisherService) refreshGauges(ctx context.Context) {
	if pending, err := p.repo.CountPendingOutbox(ctx); err == nil {
		notificationmetrics.SetOutboxPending(float64(pending))
	}
	if age, err := p.repo.OldestPendingOutboxAge(ctx); err == nil {
		notificationmetrics.SetOutboxOldestPendingSeconds(age.Seconds())
	}
}
