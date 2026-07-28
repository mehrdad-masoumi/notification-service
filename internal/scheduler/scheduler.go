package scheduler

import (
	"context"
	"log"
	"time"

	notificationmetrics "notification-service/internal/notification/metrics"
	notificationservice "notification-service/internal/notification/service"
)

// Scheduler periodically promotes due ('scheduled' and scheduled_at <=
// NOW()) notifications to 'pending' and writes their outbox rows, all in
// one atomic transaction (see repository.PromoteScheduledBatch). It never
// publishes to RabbitMQ directly: the outbox publisher process does that.
type Scheduler struct {
	svc      *notificationservice.Service
	interval time.Duration
	batch    int
}

func New(svc *notificationservice.Service, intervalSeconds, batchSize int) *Scheduler {
	return &Scheduler{
		svc:      svc,
		interval: time.Duration(intervalSeconds) * time.Second,
		batch:    batchSize,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	promoted, err := s.svc.PromoteScheduled(ctx, s.batch)
	if err != nil {
		log.Printf("scheduler promote: %v", err)
		notificationmetrics.IncError("scheduler_promote")
		notificationmetrics.IncSchedulerClaimed("error")
		return
	}
	for i := 0; i < promoted; i++ {
		notificationmetrics.IncSchedulerClaimed("success")
	}
}
