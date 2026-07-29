package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"notification-service/bootstrap"
	"notification-service/config"
)

func main() {
	queuesFlag := flag.String("queues", "", "comma-separated priorities: high,normal,low")
	flag.Parse()

	fx.New(
		bootstrap.WorkerModule,
		fx.Supply(bootstrap.QueuesFlag(*queuesFlag)),
		fx.Invoke(startWorkerHealthServer),
	).Run()
}

func startWorkerHealthServer(lc fx.Lifecycle, e *echo.Echo, cfg config.Config) {
	addr := "0.0.0.0:" + cfg.Worker.HealthPort

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				log.Printf("notification-worker health on %s", addr)
				if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Fatalf("health http: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Println("worker stopped")
			return e.Shutdown(ctx)
		},
	})
}
