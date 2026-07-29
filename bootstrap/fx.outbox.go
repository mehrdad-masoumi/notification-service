package bootstrap

import (
	"context"
	"log"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/mehrdad-masoumi/go-packages/httpserver"
	"notification-service/config"
	notificationrepo "notification-service/internal/notification/repository"
	"notification-service/internal/outbox"
	"notification-service/internal/queue"
)

// OutboxModule wires the transactional-outbox publisher process.
var OutboxModule = fx.Options(
	SharedModule,
	fx.Provide(
		provideRabbitMQ,
		provideOutboxPublisher,
		provideOutboxEcho,
	),
	fx.Invoke(
		registerOutboxHealth,
		startOutboxPublisher,
		startOutboxCleanup,
	),
)

func provideOutboxPublisher(
	repo *notificationrepo.Repository,
	mq *queue.Client,
	cfg config.Config,
) *outbox.PublisherService {
	return outbox.New(repo, mq, outbox.ConfigFromOutbox(cfg.Outbox))
}

func provideOutboxEcho() *echo.Echo {
	return httpserver.NewEchoMinimal()
}

func registerOutboxHealth(
	e *echo.Echo,
	repo *notificationrepo.Repository,
	mq *queue.Client,
) {
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
		httpserver.NamedChecker{Name: "rabbitmq", Checker: mq},
	)
}

func startOutboxPublisher(lc fx.Lifecycle, publisher *outbox.PublisherService) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go publisher.Run(ctx)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

func startOutboxCleanup(lc fx.Lifecycle, repo *notificationrepo.Repository, cfg config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go runOutboxCleanupLoop(ctx, repo, cfg)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

func runOutboxCleanupLoop(ctx context.Context, repo *notificationrepo.Repository, cfg config.Config) {
	interval := time.Duration(cfg.Retention.CleanupIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	retention := time.Duration(cfg.Retention.OutboxDays) * 24 * time.Hour
	lockTimeout := time.Duration(cfg.Outbox.LockTimeoutSeconds) * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := repo.CleanupPublishedOutbox(ctx, retention); err != nil {
				log.Printf("outbox cleanup: %v", err)
			} else if n > 0 {
				log.Printf("outbox cleanup: removed %d published rows", n)
			}
			if n, err := repo.RecoverStuckSending(ctx, lockTimeout); err != nil {
				log.Printf("delivery recovery: %v", err)
			} else if n > 0 {
				log.Printf("delivery recovery: reset %d stuck deliveries", n)
			}
		}
	}
}
