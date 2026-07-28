package config

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func Load(configPath string) Config {
	k := koanf.New(".")

	if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
		log.Printf("warning: could not load config file %s: %v", configPath, err)
	}

	if err := k.Load(env.Provider("NOTIFICATION_", ".", func(s string) string {
		str := strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "NOTIFICATION_")), "_", ".")
		return strings.ReplaceAll(str, "..", "_")
	}), nil); err != nil {
		panic(fmt.Sprintf("failed to load env config: %v", err))
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		panic(err)
	}

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
	if cfg.UserService.Timeout == 0 {
		cfg.UserService.Timeout = 5 * time.Second
	}
	if len(cfg.Auth.AdminRoles) == 0 {
		cfg.Auth.AdminRoles = []string{"admin", "super_admin"}
	}

	return cfg
}
