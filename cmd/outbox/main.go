package main

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"notification-service/bootstrap"
	"notification-service/config"
)

func main() {
	fx.New(
		bootstrap.OutboxModule,
		fx.Invoke(startOutboxHealthServer),
	).Run()
}

func startOutboxHealthServer(lc fx.Lifecycle, e *echo.Echo, cfg config.Config) {
	addr := "0.0.0.0:" + cfg.Outbox.HealthPort

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				log.Printf("notification-outbox health on %s", addr)
				if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Fatalf("health http: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Println("outbox publisher stopped")
			return e.Shutdown(ctx)
		},
	})
}
