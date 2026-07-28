package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mehrdad-masoumi/go-packages/db"
	"github.com/mehrdad-masoumi/go-packages/httpserver"
	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	"notification-service/internal/notification/entity"
	notificationrepo "notification-service/internal/notification/repository"
	providerregistry "notification-service/internal/provider/registry"
	"notification-service/internal/queue"
	"notification-service/internal/userdirectory"
	"notification-service/internal/worker"
)

func main() {
	queuesFlag := flag.String("queues", "", "comma-separated priorities: high,normal,low")
	flag.Parse()

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}
	cfg := config.Load(cfgPath)

	queueNames := cfg.Worker.Queues
	if *queuesFlag != "" {
		queueNames = strings.Split(*queuesFlag, ",")
	}
	if envQ := os.Getenv("QUEUES"); envQ != "" {
		queueNames = strings.Split(envQ, ",")
	}

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
	registry := providerregistry.New(cfg)
	userResolver := newUserDirectory(cfg)

	processor := worker.NewProcessor(repo, registry, userResolver, mq, cfg.Worker.MaxRetries, mq)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := httpserver.NewEchoMinimal()
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
		httpserver.NamedChecker{Name: "rabbitmq", Checker: mq},
	)

	go func() {
		addr := "0.0.0.0:" + cfg.Worker.HealthPort
		log.Printf("notification-worker health on %s", addr)
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("health http: %v", err)
		}
	}()

	for _, q := range queueNames {
		q = strings.TrimSpace(q)
		priority := entity.Priority(q)
		switch priority {
		case entity.PriorityHigh, entity.PriorityNormal, entity.PriorityLow:
		default:
			log.Fatalf("invalid queue priority: %s", q)
		}
		p := priority
		go func() {
			for {
				if ctx.Err() != nil {
					return
				}
				log.Printf("consuming queue %s", queue.QueueName(p))
				err := mq.Consume(ctx, p, cfg.Worker.Prefetch, processor.Handle)
				if ctx.Err() != nil {
					return
				}
				log.Printf("consume ended for %s: %v; restarting in 5s", p, err)
				time.Sleep(5 * time.Second)
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = e.Shutdown(shutdownCtx)
	log.Println("worker stopped")
}

func newUserDirectory(cfg config.Config) notificationcontract.IFUserDirectory {
	if cfg.UserService.BaseURL == "" {
		return userdirectory.NoopDirectory{}
	}
	return userdirectory.NewHTTP(cfg.UserService)
}
