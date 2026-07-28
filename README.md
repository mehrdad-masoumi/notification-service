# Notification Service

Stateless microservice for multi-channel notifications (In-App, Email, SMS, WhatsApp; Push stub).

## Architecture

```text
Client / Admin / Internal Services
        ↓
Notification API  (202 Accepted)
        ↓
Postgres + RabbitMQ priority queues
        ↓
Workers (high / normal / low)
        ↓
Providers (email / sms / whatsapp / inapp / push)
```

Queues: `notification.high`, `notification.normal`, `notification.low` (+ matching `.dlq`).

## Layout

```text
cmd/api                 HTTP API + scheduler
cmd/worker              Queue consumers + health/metrics
internal/notification   domain (http/service/repo/validator/dto/entity/metrics)
internal/provider       channel adapters
internal/queue          RabbitMQ (notification topology)
pkg/sharederrors        domain sentinel errors (ErrNotFound, …)
infra/grafana           Grafana dashboard
infra/prometheus        Alert rules
migrations/postgres     sql-migrate
```

Shared cross-service libs live in `../go-packages` (`apperr`, `httpserver`, `auth`, `db`).

## APIs

### Admin (JWT + admin role)

- `POST /admin/notifications` → `202`
- `GET /admin/notifications`
- `GET /admin/notifications/:id`

### User (JWT; `user_id` from token only)

- `GET /notifications`
- `GET /notifications/unread-count`
- `PATCH /notifications/:id/read`
- `POST /notifications/read-all`

### Internal (header `X-Internal-Api-Key`)

- `POST /internal/notifications` (requires `idempotency_key`)

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

- Validator (required fields, channels, idempotency key)
- Service validation paths
- Provider error classification (temporary vs permanent)
- In-App provider send

## Config

Env prefix: `NOTIFICATION_` (koanf). See `config.yml` and `.env.example`.
