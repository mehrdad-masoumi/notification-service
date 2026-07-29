package bootstrap

import (
	"context"
	"log"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/mehrdad-masoumi/go-packages/auth"
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

// APIModule wires the notification HTTP API (plus in-process scheduler and cleanup).
var APIModule = fx.Options(
	SharedModule,
	fx.Provide(
		provideValidator,
		provideNotificationService,
		provideAPIEcho,
		userhandler.New,
		adminhandler.New,
		adminhandler.NewTemplatesHandler,
		internalhandler.New,
		internalv1handler.New,
		provideScheduler,
	),
	fx.Invoke(
		runMigrations,
		registerAPIRoutes,
		startScheduler,
		startIdempotencyCleanup,
	),
)

func provideValidator(cfg config.Config) notificationvalidator.Validator {
	return notificationvalidator.New(cfg.EnabledChannels())
}

func provideNotificationService(
	repo *notificationrepo.Repository,
	validator notificationvalidator.Validator,
	cfg config.Config,
) *notificationservice.Service {
	return notificationservice.New(repo, validator, cfg.DirectNotification.RateLimitPerMinute)
}

func provideAPIEcho() *echo.Echo {
	return httpserver.NewEcho()
}

func provideScheduler(svc *notificationservice.Service, cfg config.Config) *scheduler.Scheduler {
	return scheduler.New(svc, cfg.Scheduler.IntervalSeconds, cfg.Scheduler.BatchSize)
}

func registerAPIRoutes(
	e *echo.Echo,
	cfg config.Config,
	repo *notificationrepo.Repository,
	userH *userhandler.Handler,
	adminH *adminhandler.Handler,
	templatesH *adminhandler.TemplatesHandler,
	internalH *internalhandler.Handler,
	internalV1H *internalv1handler.Handler,
) {
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
	)

	jwtMW := auth.JWTMiddleware(cfg.Auth.AccessSecret)
	adminMW := auth.RequireAdmin(cfg.Auth.AdminRoles)
	internalMW := auth.InternalAPIKey(cfg.InternalAPIKey)

	userH.Register(e.Group("/notifications", jwtMW))

	adminH.Register(e.Group("/admin/notifications", jwtMW, adminMW))
	adminH.RegisterBatches(e.Group("/admin/notification-batches", jwtMW, adminMW))
	templatesH.Register(e.Group("/admin/notification-templates", jwtMW, adminMW))

	// Deprecated: use /internal/v1/notifications instead.
	internalH.Register(e.Group("/internal/notifications", internalMW))
	internalV1H.Register(e.Group("/internal/v1", internalMW))
}

func startScheduler(lc fx.Lifecycle, sched *scheduler.Scheduler) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go sched.Run(ctx)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

func startIdempotencyCleanup(lc fx.Lifecycle, svc *notificationservice.Service, cfg config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go runIdempotencyCleanupLoop(ctx, svc, cfg)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

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
