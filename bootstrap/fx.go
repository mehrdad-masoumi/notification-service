package bootstrap

import (
	"context"
	"fmt"
	"os"

	"github.com/jmoiron/sqlx"
	"go.uber.org/fx"

	"github.com/mehrdad-masoumi/go-packages/db"
	"notification-service/config"
	notificationrepo "notification-service/internal/notification/repository"
	"notification-service/internal/queue"
)

// SharedModule provides config, Postgres, and the notification repository.
var SharedModule = fx.Options(
	fx.Provide(
		provideConfig,
		providePostgres,
		notificationrepo.New,
	),
)

func provideConfig() config.Config {
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}
	return config.Load(cfgPath)
}

func providePostgres(lc fx.Lifecycle, cfg config.Config) *sqlx.DB {
	sqlDB, err := db.Connect(cfg.Postgres.DSN())
	if err != nil {
		panic(fmt.Sprintf("postgres: %v", err))
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return sqlDB.Close()
		},
	})

	return sqlDB
}

func provideRabbitMQ(lc fx.Lifecycle, cfg config.Config) *queue.Client {
	mq, err := queue.NewClient(cfg.Rabbitmq)
	if err != nil {
		panic(fmt.Sprintf("rabbitmq: %v", err))
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return mq.Close()
		},
	})
	return mq
}

func runMigrations(sqlDB *sqlx.DB) error {
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "./migrations/postgres"
	}
	return db.RunMigrations(sqlDB, migrationsDir)
}
