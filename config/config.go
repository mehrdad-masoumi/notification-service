package config

import (
	"fmt"
	"strings"
)

type Config struct {
	Application        Application        `koanf:"application"`
	Postgres           Postgres           `koanf:"postgres"`
	Rabbitmq           Rabbitmq           `koanf:"rabbitmq"`
	Auth               Auth               `koanf:"auth"`
	InternalAPIKey     string             `koanf:"internal_api_key"`
	Email              Email              `koanf:"email"`
	SMS                SMS                `koanf:"sms"`
	WhatsApp           WhatsApp           `koanf:"whatsapp"`
	Push               Push               `koanf:"push"`
	Worker             Worker             `koanf:"worker"`
	Scheduler          Scheduler          `koanf:"scheduler"`
	Outbox             Outbox             `koanf:"outbox"`
	Retention          Retention          `koanf:"retention"`
	DirectNotification DirectNotification `koanf:"direct_notification"`
	Batch              Batch              `koanf:"batch"`
}

type Application struct {
	Env        string     `koanf:"env"`
	HTTPServer HTTPServer `koanf:"http_server"`
}

type HTTPServer struct {
	URL  string `koanf:"url"`
	Port string `koanf:"port"`
}

type Postgres struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	User     string `koanf:"user"`
	Password string `koanf:"password"`
	DB       string `koanf:"db"`
	SSL      string `koanf:"ssl"`
}

func (p Postgres) DSN() string {
	ssl := p.SSL
	if ssl == "" {
		ssl = "disable"
	}
	return "host=" + p.Host +
		" port=" + itoa(p.Port) +
		" user=" + p.User +
		" password=" + p.Password +
		" dbname=" + p.DB +
		" sslmode=" + ssl
}

type Rabbitmq struct {
	User     string `koanf:"user"`
	Password string `koanf:"password"`
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	Vhost    string `koanf:"vhost"`
}

func (r Rabbitmq) URI() string {
	vhost := r.Vhost
	if vhost == "" {
		vhost = "/"
	}
	return "amqp://" + r.User + ":" + r.Password + "@" + r.Host + ":" + itoa(r.Port) + vhost
}

type Auth struct {
	AccessSecret string   `koanf:"access_secret"`
	AdminRoles   []string `koanf:"admin_roles"`
}

type Email struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
	From     string `koanf:"from"`
	Timeout  int    `koanf:"timeout_seconds"`
}

func (e Email) Enabled() bool {
	return strings.TrimSpace(e.Host) != ""
}

type SMS struct {
	Provider string `koanf:"provider"`
	APIKey   string `koanf:"api_key"`
	From     string `koanf:"from"`
	Enabled  bool   `koanf:"enabled"`
}

func (s SMS) Ready() bool {
	return s.Enabled && strings.TrimSpace(s.APIKey) != "" && s.Provider != "" && s.Provider != "noop"
}

type WhatsApp struct {
	Provider string `koanf:"provider"`
	APIKey   string `koanf:"api_key"`
	From     string `koanf:"from"`
	Enabled  bool   `koanf:"enabled"`
}

func (w WhatsApp) Ready() bool {
	return w.Enabled && strings.TrimSpace(w.APIKey) != "" && w.Provider != "" && w.Provider != "noop"
}

type Push struct {
	Enabled  bool   `koanf:"enabled"`
	Provider string `koanf:"provider"`
}

func (p Push) Ready() bool {
	return p.Enabled && p.Provider != "" && p.Provider != "noop" && p.Provider != "stub"
}

type Worker struct {
	Queues      []string `koanf:"queues"`
	Concurrency int      `koanf:"concurrency"`
	Prefetch    int      `koanf:"prefetch"`
	MaxRetries  int      `koanf:"max_retries"`
	HealthPort  string   `koanf:"health_port"`
}

type Scheduler struct {
	IntervalSeconds int `koanf:"interval_seconds"`
	BatchSize       int `koanf:"batch_size"`
}

type Outbox struct {
	BatchSize          int    `koanf:"batch_size"`
	PollIntervalMS     int    `koanf:"poll_interval_ms"`
	LockTimeoutSeconds int    `koanf:"lock_timeout_seconds"`
	HealthPort         string `koanf:"health_port"`
	MaxAttempts        int    `koanf:"max_attempts"`
}

type Retention struct {
	OutboxDays             int `koanf:"outbox_days"`
	IdempotencyHours       int `koanf:"idempotency_hours"`
	CleanupIntervalMinutes int `koanf:"cleanup_interval_minutes"`
}

type DirectNotification struct {
	RateLimitPerMinute int `koanf:"rate_limit_per_minute"`
}

type Batch struct {
	SyncMaxRecipients int `koanf:"sync_max_recipients"`
}

// EnabledChannels returns channels that may be selected in validation.
func (c Config) EnabledChannels() map[string]bool {
	out := map[string]bool{
		"in_app": true,
		"email":  c.Email.Enabled(),
	}
	if c.SMS.Ready() {
		out["sms"] = true
	}
	if c.WhatsApp.Ready() {
		out["whatsapp"] = true
	}
	if c.Push.Ready() {
		out["push"] = true
	}
	// Email channel is always selectable; send may fail if SMTP not configured.
	// Prefer allowing email so templates can target it; provider returns permanent if unset.
	out["email"] = true
	return out
}

func (c Config) IsProduction() bool {
	env := strings.ToLower(strings.TrimSpace(c.Application.Env))
	return env == "production" || env == "prod"
}

// Validate checks required configuration. Secrets are never logged.
func (c Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.Auth.AccessSecret) == "" {
		missing = append(missing, "auth.access_secret")
	}
	if strings.TrimSpace(c.InternalAPIKey) == "" {
		missing = append(missing, "internal_api_key")
	}
	if strings.TrimSpace(c.Postgres.Host) == "" {
		missing = append(missing, "postgres.host")
	}
	if strings.TrimSpace(c.Postgres.DB) == "" {
		missing = append(missing, "postgres.db")
	}
	if strings.TrimSpace(c.Rabbitmq.Host) == "" {
		missing = append(missing, "rabbitmq.host")
	}
	if c.IsProduction() {
		if isPlaceholderSecret(c.Auth.AccessSecret) {
			missing = append(missing, "auth.access_secret(placeholder)")
		}
		if isPlaceholderSecret(c.InternalAPIKey) {
			missing = append(missing, "internal_api_key(placeholder)")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("invalid config: missing or invalid fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func isPlaceholderSecret(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || strings.HasPrefix(v, "change-me")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
