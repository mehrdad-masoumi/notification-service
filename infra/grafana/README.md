# Grafana — Notification Service

Dashboard: [`dashboards/notification.json`](dashboards/notification.json)

| Panel | PromQL focus |
|-------|----------------|
| Accepted / Enqueued | `notification_accepted_total`, `notification_enqueued_total` |
| Delivery error rate | `notification_delivery_duration_seconds_count{result=~"...error"}` |
| Latency p50/p95 | `notification_delivery_duration_seconds_bucket` |
| Retries / DLQ | `notification_retry_total`, `notification_dlq_total` |
| Scheduler | `notification_scheduler_claimed_total` |

## Import

1. Scrape `/metrics` from notification API (and optionally workers).
2. Grafana → **Dashboards → New → Import**
3. Upload `dashboards/notification.json`
4. Select Prometheus datasource

## Provisioning (optional)

```yaml
apiVersion: 1
providers:
  - name: notification
    folder: Notification
    type: file
    options:
      path: /etc/grafana/provisioning/dashboards/notification
```
