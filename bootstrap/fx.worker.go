package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/mehrdad-masoumi/go-packages/httpserver"
	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	"notification-service/internal/notification/entity"
	notificationrepo "notification-service/internal/notification/repository"
	providerregistry "notification-service/internal/provider/registry"
	"notification-service/internal/queue"
	"notification-service/internal/worker"
)

// QueuesFlag holds the optional -queues CLI override (comma-separated priorities).
type QueuesFlag string

// WorkerModule wires the notification delivery worker.
var WorkerModule = fx.Options(
	SharedModule,
	fx.Provide(
		provideRabbitMQ,
		fx.Annotate(
			providerregistry.New,
			fx.As(new(notificationcontract.IFProviderRegistry)),
		),
		provideProcessor,
		provideWorkerEcho,
	),
	fx.Invoke(
		registerWorkerHealth,
		startStuckDeliveryRecovery,
		startConsumers,
	),
)

func provideProcessor(
	repo *notificationrepo.Repository,
	registry notificationcontract.IFProviderRegistry,
	mq *queue.Client,
	cfg config.Config,
) *worker.Processor {
	return worker.NewProcessor(repo, registry, cfg.Worker.MaxRetries, mq)
}

func provideWorkerEcho() *echo.Echo {
	return httpserver.NewEchoMinimal()
}

func registerWorkerHealth(
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

func startStuckDeliveryRecovery(lc fx.Lifecycle, processor *worker.Processor, cfg config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go runStuckDeliveryRecovery(ctx, processor, cfg)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

func startConsumers(
	lc fx.Lifecycle,
	mq *queue.Client,
	processor *worker.Processor,
	cfg config.Config,
	queuesFlag QueuesFlag,
) {
	queueNames := resolveWorkerQueues(cfg, string(queuesFlag))
	concurrency := cfg.Worker.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			for _, q := range queueNames {
				q = strings.TrimSpace(q)
				priority := entity.Priority(q)
				switch priority {
				case entity.PriorityHigh, entity.PriorityNormal, entity.PriorityLow:
				default:
					return fmt.Errorf("invalid queue priority: %s", q)
				}
				p := priority
				for i := 0; i < concurrency; i++ {
					workerIdx := i
					go func() {
						for {
							if ctx.Err() != nil {
								return
							}
							log.Printf("consuming queue %s worker=%d", queue.QueueName(p), workerIdx)
							err := mq.Consume(ctx, p, cfg.Worker.Prefetch, processor.Handle)
							if ctx.Err() != nil {
								return
							}
							log.Printf("consume ended for %s worker=%d: %v; restarting in 5s", p, workerIdx, err)
							time.Sleep(5 * time.Second)
						}
					}()
				}
			}
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

func resolveWorkerQueues(cfg config.Config, queuesFlag string) []string {
	queueNames := cfg.Worker.Queues
	if queuesFlag != "" {
		queueNames = strings.Split(queuesFlag, ",")
	}
	if envQ := os.Getenv("QUEUES"); envQ != "" {
		queueNames = strings.Split(envQ, ",")
	}
	return queueNames
}

func runStuckDeliveryRecovery(ctx context.Context, processor *worker.Processor, cfg config.Config) {
	timeout := time.Duration(cfg.Outbox.LockTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Minute
	}
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := processor.RecoverStuckDeliveries(ctx, timeout); err != nil {
				log.Printf("recover stuck deliveries: %v", err)
			} else if n > 0 {
				log.Printf("recovered %d stuck deliveries", n)
			}
		}
	}
}
