# Prometheus alerts — Notification Service

Rule file: [`alerts/notification.yml`](alerts/notification.yml)

| Alert | Condition |
|-------|-----------|
| `NotificationDeliveryErrorRateWarning` | Delivery error rate > 10% over 5m (≥ 3 events) |
| `NotificationDeliveryErrorRateCritical` | Delivery error rate > 20% over 5m (≥ 3 events) |
| `NotificationErrorsSpike` | `notification_errors_total` ≥ 5 in 5m |
| `NotificationDLQSpike` | `notification_dlq_total` ≥ 5 in 5m |

## Scrape targets

- API: `http://<api-host>:<port>/metrics` (default container port `8080`)
- Worker: `http://<worker-host>:<health_port>/metrics` (default `8081`)

## Validation

```bash
promtool check rules notification-service/infra/prometheus/alerts/notification.yml
```

Mount or include this file in your external Prometheus `rule_files` and route Alertmanager by `service=notification`.
