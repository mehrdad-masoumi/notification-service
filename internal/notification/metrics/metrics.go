package notificationmetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	AcceptedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_accepted_total",
			Help: "Total notifications accepted by API",
		},
		[]string{"source", "priority", "result"},
	)

	EnqueuedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_enqueued_total",
			Help: "Total delivery jobs enqueued",
		},
		[]string{"channel", "priority", "result"},
	)

	DeliveryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "notification_delivery_duration_seconds",
			Help:    "Duration of notification delivery attempts",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"channel", "result"},
	)

	RetryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_retry_total",
			Help: "Total temporary delivery retries",
		},
		[]string{"channel", "priority"},
	)

	DLQTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_dlq_total",
			Help: "Total jobs published to DLQ",
		},
		[]string{"priority"},
	)

	SchedulerClaimedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_scheduler_claimed_total",
			Help: "Total scheduled notifications claimed by scheduler",
		},
		[]string{"result"},
	)

	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_errors_total",
			Help: "Total notification operation errors",
		},
		[]string{"operation"},
	)

	// --- Outbox / template / idempotency (redesign) ---

	CommandsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_commands_total",
			Help: "Total v1 command requests (accept-command / accept-direct-command)",
		},
		[]string{"kind", "priority", "result"},
	)

	OutboxPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "notification_outbox_pending",
			Help: "Current number of unpublished outbox rows",
		},
	)

	OutboxPublishTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_outbox_publish_total",
			Help: "Total outbox publish attempts",
		},
		[]string{"result"},
	)

	OutboxOldestPendingSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "notification_outbox_oldest_pending_seconds",
			Help: "Age in seconds of the oldest unpublished outbox row",
		},
	)

	DeliveryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_delivery_total",
			Help: "Total delivery attempts by channel/result",
		},
		[]string{"channel", "result"},
	)

	TemplateRenderErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_template_render_errors_total",
			Help: "Total template render failures (e.g. missing variables)",
		},
		[]string{"channel"},
	)

	IdempotencyConflictsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_idempotency_conflicts_total",
			Help: "Total idempotency key conflicts (hash mismatch or in-progress replay)",
		},
		[]string{"reason"},
	)
)

func init() {
	prometheus.MustRegister(
		AcceptedTotal,
		EnqueuedTotal,
		DeliveryDuration,
		RetryTotal,
		DLQTotal,
		SchedulerClaimedTotal,
		ErrorsTotal,
		CommandsTotal,
		OutboxPending,
		OutboxPublishTotal,
		OutboxOldestPendingSeconds,
		DeliveryTotal,
		TemplateRenderErrorsTotal,
		IdempotencyConflictsTotal,
	)
}

func IncAccepted(source, priority, result string) {
	AcceptedTotal.WithLabelValues(source, priority, result).Inc()
}

func IncEnqueued(channel, priority, result string) {
	EnqueuedTotal.WithLabelValues(channel, priority, result).Inc()
}

func ObserveDelivery(channel, result string, startedAt time.Time) {
	DeliveryDuration.WithLabelValues(channel, result).Observe(time.Since(startedAt).Seconds())
}

func IncRetry(channel, priority string) {
	RetryTotal.WithLabelValues(channel, priority).Inc()
}

func IncDLQ(priority string) {
	DLQTotal.WithLabelValues(priority).Inc()
}

func IncSchedulerClaimed(result string) {
	SchedulerClaimedTotal.WithLabelValues(result).Inc()
}

func IncError(operation string) {
	ErrorsTotal.WithLabelValues(operation).Inc()
}

func IncCommand(kind, priority, result string) {
	CommandsTotal.WithLabelValues(kind, priority, result).Inc()
}

func SetOutboxPending(n float64) {
	OutboxPending.Set(n)
}

func IncOutboxPublish(result string) {
	OutboxPublishTotal.WithLabelValues(result).Inc()
}

func SetOutboxOldestPendingSeconds(s float64) {
	OutboxOldestPendingSeconds.Set(s)
}

func IncDelivery(channel, result string) {
	DeliveryTotal.WithLabelValues(channel, result).Inc()
}

func IncTemplateRenderError(channel string) {
	TemplateRenderErrorsTotal.WithLabelValues(channel).Inc()
}

func IncIdempotencyConflict(reason string) {
	IdempotencyConflictsTotal.WithLabelValues(reason).Inc()
}
