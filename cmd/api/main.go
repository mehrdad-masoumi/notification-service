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
	userhandler "notification-service/internal/notification/http/user"
	notificationrepo "notification-service/internal/notification/repository"
	notificationservice "notification-service/internal/notification/service"
	notificationvalidator "notification-service/internal/notification/validator"
	"notification-service/internal/queue"
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

	mq, err := queue.NewClient(cfg.Rabbitmq)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer mq.Close()

	repo := notificationrepo.New(sqlDB)
	svc := notificationservice.New(repo, notificationvalidator.New(), mq)

	e := httpserver.NewEcho()
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
		httpserver.NamedChecker{Name: "rabbitmq", Checker: mq},
	)

	jwtMW := auth.JWTMiddleware(cfg.Auth.AccessSecret)
	adminMW := auth.RequireAdmin(cfg.Auth.AdminRoles)
	internalMW := auth.InternalAPIKey(cfg.InternalAPIKey)

	userhandler.New(svc).Register(e.Group("/notifications", jwtMW))
	adminhandler.New(svc).Register(e.Group("/admin/notifications", jwtMW, adminMW))
	internalhandler.New(svc).Register(e.Group("/internal/notifications", internalMW))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched := scheduler.New(svc, cfg.Scheduler.IntervalSeconds, cfg.Scheduler.BatchSize)
	go sched.Run(ctx)

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
