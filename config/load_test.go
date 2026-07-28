package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"notification-service/config"
)

func TestEnvKeyMapper(t *testing.T) {
	cases := map[string]string{
		"NOTIFICATION_AUTH__ACCESS_SECRET":              "auth.access_secret",
		"NOTIFICATION_INTERNAL_API_KEY":                 "internal_api_key",
		"NOTIFICATION_WORKER__MAX_RETRIES":              "worker.max_retries",
		"NOTIFICATION_USER_SERVICE__BASE_URL":           "user_service.base_url",
		"NOTIFICATION_APPLICATION__HTTP_SERVER__PORT":   "application.http_server.port",
		"NOTIFICATION_APPLICATION__ENV":                 "application.env",
		"NOTIFICATION_POSTGRES__HOST":                   "postgres.host",
		"NOTIFICATION_OUTBOX__POLL_INTERVAL_MS":         "outbox.poll_interval_ms",
		"NOTIFICATION_DIRECT_NOTIFICATION__RATE_LIMIT_PER_MINUTE": "direct_notification.rate_limit_per_minute",
	}
	for in, want := range cases {
		require.Equal(t, want, config.EnvKeyMapper(in), in)
	}
}

func TestLoadMapsNestedEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
application:
  env: test
  http_server:
    url: 0.0.0.0
    port: "8080"
postgres:
  host: localhost
  port: 5432
  user: u
  password: p
  db: db
  ssl: disable
rabbitmq:
  user: guest
  password: guest
  host: localhost
  port: 5672
  vhost: /
auth:
  access_secret: test-secret
internal_api_key: test-key
`), 0o600))

	t.Setenv("NOTIFICATION_AUTH__ACCESS_SECRET", "from-env-secret")
	t.Setenv("NOTIFICATION_INTERNAL_API_KEY", "from-env-key")
	t.Setenv("NOTIFICATION_WORKER__MAX_RETRIES", "9")
	t.Setenv("NOTIFICATION_USER_SERVICE__BASE_URL", "http://users:8080")
	t.Setenv("NOTIFICATION_APPLICATION__HTTP_SERVER__PORT", "9090")
	t.Setenv("NOTIFICATION_POSTGRES__HOST", "pg-from-env")

	cfg := config.LoadWithoutValidate(cfgPath)
	require.Equal(t, "from-env-secret", cfg.Auth.AccessSecret)
	require.Equal(t, "from-env-key", cfg.InternalAPIKey)
	require.Equal(t, 9, cfg.Worker.MaxRetries)
	require.Equal(t, "http://users:8080", cfg.UserService.BaseURL)
	require.Equal(t, "9090", cfg.Application.HTTPServer.Port)
	require.Equal(t, "pg-from-env", cfg.Postgres.Host)
}

func TestValidateRejectsPlaceholderInProduction(t *testing.T) {
	cfg := config.Config{
		Application:    config.Application{Env: "production"},
		Auth:           config.Auth{AccessSecret: "change-me-access-secret"},
		InternalAPIKey: "change-me-internal-api-key",
		Postgres:       config.Postgres{Host: "h", DB: "d"},
		Rabbitmq:       config.Rabbitmq{Host: "r"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "placeholder")
}

func TestValidateAllowsPlaceholderInDevelopment(t *testing.T) {
	cfg := config.Config{
		Application:    config.Application{Env: "development"},
		Auth:           config.Auth{AccessSecret: "change-me-access-secret"},
		InternalAPIKey: "change-me-internal-api-key",
		Postgres:       config.Postgres{Host: "h", DB: "d"},
		Rabbitmq:       config.Rabbitmq{Host: "r"},
	}
	require.NoError(t, cfg.Validate())
}

func TestSMSReady(t *testing.T) {
	require.False(t, (config.SMS{Enabled: true, Provider: "noop", APIKey: "x"}).Ready())
	require.False(t, (config.SMS{Enabled: false, Provider: "twilio", APIKey: "x"}).Ready())
	require.True(t, (config.SMS{Enabled: true, Provider: "twilio", APIKey: "x"}).Ready())
}
