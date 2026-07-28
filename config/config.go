package config

import "time"

type Config struct {
	Application    Application    `koanf:"application"`
	Postgres       Postgres       `koanf:"postgres"`
	Rabbitmq       Rabbitmq       `koanf:"rabbitmq"`
	Auth           Auth           `koanf:"auth"`
	InternalAPIKey string         `koanf:"internal_api_key"`
	Email          Email          `koanf:"email"`
	SMS            SMS            `koanf:"sms"`
	WhatsApp       WhatsApp       `koanf:"whatsapp"`
	Worker         Worker         `koanf:"worker"`
	UserService    UserService    `koanf:"user_service"`
	Scheduler      Scheduler      `koanf:"scheduler"`
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
}

type SMS struct {
	Provider string `koanf:"provider"`
	APIKey   string `koanf:"api_key"`
	From     string `koanf:"from"`
	Enabled  bool   `koanf:"enabled"`
}

type WhatsApp struct {
	Provider string `koanf:"provider"`
	APIKey   string `koanf:"api_key"`
	From     string `koanf:"from"`
	Enabled  bool   `koanf:"enabled"`
}

type Worker struct {
	Queues      []string `koanf:"queues"`
	Concurrency int      `koanf:"concurrency"`
	Prefetch    int      `koanf:"prefetch"`
	MaxRetries  int      `koanf:"max_retries"`
	HealthPort  string   `koanf:"health_port"`
}

type UserService struct {
	BaseURL string `koanf:"base_url"`
	APIKey  string `koanf:"api_key"`
	Timeout time.Duration `koanf:"timeout"`
}

type Scheduler struct {
	IntervalSeconds int `koanf:"interval_seconds"`
	BatchSize       int `koanf:"batch_size"`
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
