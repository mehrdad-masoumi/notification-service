// Command outbox runs the transactional-outbox publisher: it claims
// notification_outbox rows and publishes them to RabbitMQ with confirms.
// It is a separate process from the API so a broker outage never blocks
// notification acceptance.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mehrdad-masoumi/go-packages/db"
	"github.com/mehrdad-masoumi/go-packages/httpserver"
	"notification-service/config"
	notificationrepo "notification-service/internal/notification/repository"
	"notification-service/internal/outbox"
	"notification-service/internal/queue"
)

func main() {
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}
	cfg := config.Load(cfgPath)

	sqlDB, err := db.Connect(cfg.Postgres.DSN())
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer sqlDB.Close()

	mq, err := queue.NewClient(cfg.Rabbitmq)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer mq.Close()

	repo := notificationrepo.New(sqlDB)
	publisher := outbox.New(repo, mq, outbox.ConfigFromOutbox(cfg.Outbox))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go publisher.Run(ctx)
	go runCleanupLoop(ctx, repo, cfg)

	e := httpserver.NewEchoMinimal()
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
		httpserver.NamedChecker{Name: "rabbitmq", Checker: mq},
	)

	addr := "0.0.0.0:" + cfg.Outbox.HealthPort
	go func() {
		log.Printf("notification-outbox health on %s", addr)
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("health http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = e.Shutdown(shutdownCtx)
	log.Println("outbox publisher stopped")
}

func runCleanupLoop(ctx context.Context, repo *notificationrepo.Repository, cfg config.Config) {
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
