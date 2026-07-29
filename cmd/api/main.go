package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"notification-service/bootstrap"
	"notification-service/config"
)

func main() {
	fx.New(
		bootstrap.APIModule,
		fx.Invoke(startAPIServer),
	).Run()
}

func startAPIServer(lc fx.Lifecycle, e *echo.Echo, cfg config.Config) {
	addr := fmt.Sprintf("%s:%s", cfg.Application.HTTPServer.URL, cfg.Application.HTTPServer.Port)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				log.Printf("notification-api listening on %s", addr)
				if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Fatalf("http: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return e.Shutdown(ctx)
		},
	})
}
