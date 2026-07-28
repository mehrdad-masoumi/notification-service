package config

import (
	"fmt"
	"log"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// EnvKeyMapper maps NOTIFICATION_* env vars to koanf keys.
//
// Convention:
//   - Prefix: NOTIFICATION_
//   - "__" separates nested levels (becomes ".")
//   - "_" remains part of the field name
//
// Examples:
//
//	NOTIFICATION_AUTH__ACCESS_SECRET        → auth.access_secret
//	NOTIFICATION_INTERNAL_API_KEY           → internal_api_key
//	NOTIFICATION_WORKER__MAX_RETRIES        → worker.max_retries
//	NOTIFICATION_USER_SERVICE__BASE_URL     → user_service.base_url
//	NOTIFICATION_APPLICATION__HTTP_SERVER__PORT → application.http_server.port
func EnvKeyMapper(s string) string {
	s = strings.ToLower(strings.TrimPrefix(s, "NOTIFICATION_"))
	parts := strings.Split(s, "__")
	return strings.Join(parts, ".")
}

func Load(configPath string) Config {
	k := koanf.New(".")

	if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
		log.Printf("warning: could not load config file %s: %v", configPath, err)
	}

	if err := k.Load(env.Provider("NOTIFICATION_", ".", EnvKeyMapper), nil); err != nil {
		panic(fmt.Sprintf("failed to load env config: %v", err))
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		panic(err)
	}

	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		panic(err)
	}

	return cfg
}

// LoadWithoutValidate loads config for tests that exercise mapping without full secrets.
func LoadWithoutValidate(configPath string) Config {
	k := koanf.New(".")
	_ = k.Load(file.Provider(configPath), yaml.Parser())
	_ = k.Load(env.Provider("NOTIFICATION_", ".", EnvKeyMapper), nil)
	var cfg Config
	_ = k.Unmarshal("", &cfg)
	applyDefaults(&cfg)
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.Worker.MaxRetries <= 0 {
		cfg.Worker.MaxRetries = 5
	}
	if cfg.Worker.Prefetch <= 0 {
		cfg.Worker.Prefetch = 10
	}
	if cfg.Worker.Concurrency <= 0 {
		cfg.Worker.Concurrency = 4
	}
	if cfg.Worker.HealthPort == "" {
		cfg.Worker.HealthPort = "8081"
	}
	if cfg.Scheduler.IntervalSeconds <= 0 {
		cfg.Scheduler.IntervalSeconds = 30
	}
	if cfg.Scheduler.BatchSize <= 0 {
		cfg.Scheduler.BatchSize = 100
	}
	if cfg.UserService.TimeoutSeconds <= 0 {
		cfg.UserService.TimeoutSeconds = 5
	}
	if cfg.UserService.ContactsPathFormat == "" {
		cfg.UserService.ContactsPathFormat = "/internal/users/%s/notification-contacts"
	}
	if cfg.Outbox.BatchSize <= 0 {
		cfg.Outbox.BatchSize = 50
	}
	if cfg.Outbox.PollIntervalMS <= 0 {
		cfg.Outbox.PollIntervalMS = 500
	}
	if cfg.Outbox.LockTimeoutSeconds <= 0 {
		cfg.Outbox.LockTimeoutSeconds = 60
	}
	if cfg.Outbox.HealthPort == "" {
		cfg.Outbox.HealthPort = "8082"
	}
	if cfg.Outbox.MaxAttempts <= 0 {
		cfg.Outbox.MaxAttempts = 20
	}
	if cfg.Retention.OutboxDays <= 0 {
		cfg.Retention.OutboxDays = 7
	}
	if cfg.Retention.IdempotencyHours <= 0 {
		cfg.Retention.IdempotencyHours = 24
	}
	if cfg.Retention.CleanupIntervalMinutes <= 0 {
		cfg.Retention.CleanupIntervalMinutes = 60
	}
	if cfg.DirectNotification.RateLimitPerMinute <= 0 {
		cfg.DirectNotification.RateLimitPerMinute = 60
	}
	if cfg.Batch.SyncMaxRecipients <= 0 {
		cfg.Batch.SyncMaxRecipients = 100
	}
	if cfg.Email.Timeout <= 0 {
		cfg.Email.Timeout = 10
	}
	if len(cfg.Auth.AdminRoles) == 0 {
		cfg.Auth.AdminRoles = []string{"admin", "super_admin"}
	}
	if cfg.Application.Env == "" {
		cfg.Application.Env = "development"
	}
}
