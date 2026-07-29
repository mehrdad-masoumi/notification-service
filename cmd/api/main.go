package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mehrdad-masoumi/go-packages/auth"
	"github.com/mehrdad-masoumi/go-packages/db"
	"github.com/mehrdad-masoumi/go-packages/httpserver"
	"notification-service/config"
	adminhandler "notification-service/internal/notification/http/admin"
	internalhandler "notification-service/internal/notification/http/internalapi"
	internalv1handler "notification-service/internal/notification/http/internalapi/v1"
	userhandler "notification-service/internal/notification/http/user"
	notificationrepo "notification-service/internal/notification/repository"
	notificationservice "notification-service/internal/notification/service"
	notificationvalidator "notification-service/internal/notification/validator"
	"notification-service/internal/scheduler"
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

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "./migrations/postgres"
	}
	if err := db.RunMigrations(sqlDB, migrationsDir); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	repo := notificationrepo.New(sqlDB)
	validator := notificationvalidator.New(cfg.EnabledChannels())
	svc := notificationservice.New(repo, validator)

	e := httpserver.NewEcho()
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
	)

	jwtMW := auth.JWTMiddleware(cfg.Auth.AccessSecret)
	adminMW := auth.RequireAdmin(cfg.Auth.AdminRoles)
	internalMW := auth.InternalAPIKey(cfg.InternalAPIKey)

	userhandler.New(svc).Register(e.Group("/notifications", jwtMW))

	admin := adminhandler.New(svc)
	admin.Register(e.Group("/admin/notifications", jwtMW, adminMW))
	admin.RegisterBatches(e.Group("/admin/notification-batches", jwtMW, adminMW))
	adminhandler.NewTemplatesHandler(svc).Register(e.Group("/admin/notification-templates", jwtMW, adminMW))

	// Deprecated: use /internal/v1/notifications instead.
	internalhandler.New(svc).Register(e.Group("/internal/notifications", internalMW))
	internalv1handler.New(svc).Register(e.Group("/internal/v1", internalMW))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched := scheduler.New(svc, cfg.Scheduler.IntervalSeconds, cfg.Scheduler.BatchSize)
	go sched.Run(ctx)
	go runIdempotencyCleanupLoop(ctx, svc, cfg)

	addr := fmt.Sprintf("%s:%s", cfg.Application.HTTPServer.URL, cfg.Application.HTTPServer.Port)
	go func() {
		log.Printf("notification-api listening on %s", addr)
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = e.Shutdown(shutdownCtx)
}

// runIdempotencyCleanupLoop periodically deletes expired idempotency keys
// so the table stays bounded. The outbox process runs the equivalent
// cleanup for published outbox rows and stuck delivery/lock recovery.
func runIdempotencyCleanupLoop(ctx context.Context, svc *notificationservice.Service, cfg config.Config) {
	interval := time.Duration(cfg.Retention.CleanupIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := svc.CleanupExpiredIdempotency(ctx); err != nil {
				log.Printf("idempotency cleanup: %v", err)
			} else if n > 0 {
				log.Printf("idempotency cleanup: removed %d expired keys", n)
			}
		}
	}
}
