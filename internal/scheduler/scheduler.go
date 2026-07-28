package scheduler

import (
	"context"
	"log"
	"time"

	"notification-service/internal/notification/entity"
	notificationmetrics "notification-service/internal/notification/metrics"
	notificationservice "notification-service/internal/notification/service"
)

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
	repo := s.svc.Repo()
	due, err := repo.ListDueScheduled(ctx, s.batch)
	if err != nil {
		log.Printf("scheduler list due: %v", err)
		notificationmetrics.IncError("scheduler_list")
		notificationmetrics.IncSchedulerClaimed("error")
		return
	}
	for _, n := range due {
		claimed, err := repo.ClaimScheduled(ctx, n.ID)
		if err != nil {
			log.Printf("scheduler claim %s: %v", n.ID, err)
			notificationmetrics.IncError("scheduler_claim")
			notificationmetrics.IncSchedulerClaimed("error")
			continue
		}
		if !claimed {
			continue
		}
		n.Status = entity.StatusQueued
		if err := s.svc.EnqueueExisting(ctx, n); err != nil {
			log.Printf("scheduler enqueue %s: %v", n.ID, err)
			notificationmetrics.IncError("scheduler_enqueue")
			notificationmetrics.IncSchedulerClaimed("error")
			continue
		}
		notificationmetrics.IncSchedulerClaimed("success")
	}
}
