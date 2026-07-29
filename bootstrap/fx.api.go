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
	application "notification-service/internal/application/notification"
	adminhandler "notification-service/internal/notification/http/admin"
	userhandler "notification-service/internal/notification/http/user"
	notificationrepo "notification-service/internal/notification/repository"
	notificationservice "notification-service/internal/notification/service"
	notificationvalidator "notification-service/internal/notification/validator"
	"notification-service/internal/scheduler"
	grpctransport "notification-service/internal/transport/grpc"
	httptransport "notification-service/internal/transport/http"
	rabbitmqtransport "notification-service/internal/transport/rabbitmq"
)

// APIModule wires the notification HTTP API (plus in-process scheduler, cleanup,
// optional gRPC server, and optional RabbitMQ command consumer).
var APIModule = fx.Options(
	SharedModule,
	fx.Provide(
		provideValidator,
		provideNotificationService,
		provideCommandService,
		provideAPIEcho,
		userhandler.New,
		adminhandler.New,
		adminhandler.NewTemplatesHandler,
		httptransport.NewAdminHandler,
		grpctransport.NewServer,
		provideScheduler,
	),
	fx.Invoke(
		runMigrations,
		registerAPIRoutes,
		startScheduler,
		startIdempotencyCleanup,
		startGRPCServer,
		startRabbitConsumer,
	),
)

func provideValidator(cfg config.Config) notificationvalidator.Validator {
	return notificationvalidator.New(cfg.EnabledChannels())
}

func provideNotificationService(
	repo *notificationrepo.Repository,
	validator notificationvalidator.Validator,
) *notificationservice.Service {
	return notificationservice.New(repo, validator)
}

func provideCommandService(svc *notificationservice.Service) *application.CommandService {
	return application.NewCommandService(svc)
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
	adminV1 *httptransport.AdminHandler,
) {
	httpserver.RegisterMetrics(e)
	httpserver.RegisterHealth(e,
		httpserver.NamedChecker{Name: "postgres", Checker: repo},
	)

	jwtMW := auth.JWTMiddleware(cfg.Auth.AccessSecret)
	adminMW := auth.RequireAdmin(cfg.Auth.AdminRoles)

	userH.Register(e.Group("/notifications", jwtMW))

	if cfg.Transport.HTTP.Enabled {
		adminV1.Register(e.Group("/admin/v1", jwtMW, adminMW))
		adminH.Register(e.Group("/admin/v1/notifications", jwtMW, adminMW))
		adminH.RegisterBatches(e.Group("/admin/v1/notification-batches", jwtMW, adminMW))
		templatesH.Register(e.Group("/admin/v1/notification-templates", jwtMW, adminMW))
	}
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

func startGRPCServer(lc fx.Lifecycle, cfg config.Config, svc *grpctransport.Server) {
	if !cfg.Transport.GRPC.Enabled {
		return
	}
	runner := grpctransport.NewRunner(cfg.Transport.GRPC.Address, grpctransport.IsDevEnv(cfg.Application.Env), svc)
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				if err := runner.Start(ctx); err != nil && ctx.Err() == nil {
					log.Printf("grpc server stopped: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			cancel()
			return runner.Stop(stopCtx)
		},
	})
}

func startRabbitConsumer(lc fx.Lifecycle, cfg config.Config, cmds *application.CommandService) {
	if !cfg.Transport.RabbitMQ.Enabled {
		return
	}
	consumer := rabbitmqtransport.NewConsumer(rabbitmqtransport.Config{
		URI:        cfg.Rabbitmq.URI(),
		Exchange:   cfg.Transport.RabbitMQ.Exchange,
		RoutingKey: cfg.Transport.RabbitMQ.RoutingKey,
		Queue:      cfg.Transport.RabbitMQ.Queue,
		DLX:        cfg.Transport.RabbitMQ.DLX,
		DLQ:        cfg.Transport.RabbitMQ.DLQ,
		Prefetch:   cfg.Transport.RabbitMQ.Prefetch,
	}, cmds)
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				if err := consumer.Start(ctx); err != nil && ctx.Err() == nil {
					log.Printf("rabbitmq consumer stopped: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			cancel()
			return consumer.Stop(stopCtx)
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
