# Notification Service

Stateless microservice for multi-channel notifications (In-App, Email, SMS, WhatsApp; Push stub).

## Architecture

Commands are accepted by the API and persisted transactionally (notification + deliveries +
outbox row) — the API never talks to RabbitMQ directly. A separate **outbox publisher**
process claims pending outbox rows (`FOR UPDATE SKIP LOCKED`) and publishes them to RabbitMQ
with publisher confirms, so a crash between DB commit and publish can never lose a message.

```text
Client / Admin / Internal Services
        ↓
Notification API  (validate → idempotency claim → resolve contacts → render template → 202 Accepted)
        ↓
Postgres: notifications + deliveries + notification_outbox   (single transaction)
        ↓
Outbox publisher (cmd/outbox): claim → publish w/ confirms → mark published/failed (backoff)
        ↓
RabbitMQ priority queues
        ↓
Workers (high / normal / low): atomic delivery claim → render per-channel → send
        ↓
Providers (email / sms / whatsapp / inapp / push)
```

Templates (`notification_templates`) hold per-channel/locale subject+body with `{{var}}`
placeholders; content is rendered at accept-time (dry run, to catch missing variables early)
and again per-channel at send-time. A scheduler promotes due `scheduled` notifications to
`pending` and writes their outbox rows atomically.

Queues: `notification.high`, `notification.normal`, `notification.low` (+ matching `.dlq`).

## Layout

```text
cmd/api                     HTTP API + scheduler + cleanup loop
cmd/worker                  Queue consumers + health/metrics
cmd/outbox                  Outbox publisher (claim → publish w/ confirms) + health/metrics
internal/notification       domain (http/service/repo/validator/dto/entity/metrics)
internal/notification/template  safe {{var}} renderer (no code execution)
internal/outbox             outbox publisher service used by cmd/outbox
internal/userclient         HTTP client resolving user contacts (email/phone/locale/prefs)
internal/provider           channel adapters
internal/queue              RabbitMQ (topology, publisher confirms, PublishWithConfirm)
pkg/sharederrors            domain sentinel errors (ErrNotFound, …)
infra/grafana               Grafana dashboard
infra/prometheus            Alert rules
migrations/postgres         sql-migrate
```

Shared cross-service libs live in `../go-packages` (`apperr`, `httpserver`, `auth`, `db`).

## APIs

### Admin (JWT + admin role)

- `POST /admin/notifications` → `202`
- `GET /admin/notifications`
- `GET /admin/notifications/:id`
- `GET /admin/notification-batches/:batch_id` — batch header + member deliveries
- `POST /admin/notification-templates`
- `GET /admin/notification-templates` / `GET /admin/notification-templates/:id`
- `PUT /admin/notification-templates/:id`
- `PATCH /admin/notification-templates/:id/status` — enable/disable

### User (JWT; `user_id` from token only)

- `GET /notifications`
- `GET /notifications/unread-count`
- `PATCH /notifications/:id/read`
- `POST /notifications/read-all`

### Internal (header `X-Internal-Api-Key`)

- `POST /internal/v1/notifications` — template-driven command (`template_code`, resolves
  user contacts/preferences, renders content, `idempotency_key` required)
- `POST /internal/v1/direct-notifications` — single channel + explicit recipient, no
  `user_id`/contacts lookup
- `POST /internal/notifications` — **deprecated**, kept for backward compatibility (still
  requires `idempotency_key`); writes to the outbox like the v1 endpoints instead of
  publishing to RabbitMQ directly

### Health & metrics

- `GET /health-check`
- `GET /ready`
- `GET /metrics` (Prometheus; also on worker health port)

## Observability

- Domain metrics: `notification_*` (accepted, enqueued, delivery latency, retries, DLQ, scheduler, errors)
- Dashboard: [`infra/grafana/dashboards/notification.json`](infra/grafana/dashboards/notification.json)
- Alerts: [`infra/prometheus/alerts/notification.yml`](infra/prometheus/alerts/notification.yml)

See [`infra/grafana/README.md`](infra/grafana/README.md) and [`infra/prometheus/README.md`](infra/prometheus/README.md).

## Sample requests

### Internal create

```http
POST /internal/notifications
X-Internal-Api-Key: change-me-internal-api-key
Content-Type: application/json

{
  "idempotency_key": "withdrawal-approved-123",
  "user_id": "11111111-1111-1111-1111-111111111111",
  "title": "برداشت تأیید شد",
  "message": "درخواست برداشت شما تأیید شد.",
  "type": "transaction",
  "channels": ["in_app", "email"],
  "priority": "normal",
  "action_url": "/withdrawals/123",
  "email": "user@example.com"
}
```

Response `202`:

```json
{
  "id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "status": "accepted"
}
```

### Admin create

```json
{
  "title": "اطلاع‌رسانی سیستم",
  "message": "نسخه جدید پنل منتشر شد.",
  "user_ids": ["11111111-1111-1111-1111-111111111111"],
  "channels": ["in_app", "email"],
  "priority": "normal",
  "action_url": "/dashboard"
}
```

### User list

```json
{
  "data": [
    {
      "id": "uuid",
      "title": "برداشت تأیید شد",
      "message": "درخواست برداشت شما تأیید شد.",
      "type": "transaction",
      "action_url": "/withdrawals/123",
      "is_read": false,
      "created_at": "2026-07-28T12:00:00Z"
    }
  ],
  "meta": { "page": 1, "per_page": 20, "total": 35 }
}
```

## Run locally (Go)

```bash
cd notification-service
cp .env.example .env
go mod tidy
go run ./cmd/api
go run ./cmd/worker -queues=high,normal,low
go run ./cmd/outbox
```

## Docker

Create DB first (example):

```sql
CREATE USER notification WITH PASSWORD 'notification';
CREATE DATABASE notifications OWNER notification;
```

```bash
cp .env.example .env
docker compose -f docker-compose.dev.yml up --build
```

Host ports (default): API `8191→8080` (outside reserved host ports).

Scale a single priority:

```bash
docker compose -f docker-compose.dev.yml up --scale notification-worker-high=3
```

## Tests

```bash
go test ./...
```

Main coverage:

- Validator (required fields, channels, idempotency key, command/direct command)
- Service validation paths
- Template renderer (`{{var}}` substitution, missing variables, no code execution)
- Outbox backoff/retry delay calculation
- Provider error classification (temporary vs permanent, disabled = permanent)
- Provider registry `Ready()` gating (sms/whatsapp/push only registered when configured)
- In-App provider send

## Config

Env prefix: `NOTIFICATION_` (koanf). See `config.yml` and `.env.example`.
