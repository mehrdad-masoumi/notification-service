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
)

func init() {
	prometheus.MustRegister(AcceptedTotal)
	prometheus.MustRegister(EnqueuedTotal)
	prometheus.MustRegister(DeliveryDuration)
	prometheus.MustRegister(RetryTotal)
	prometheus.MustRegister(DLQTotal)
	prometheus.MustRegister(SchedulerClaimedTotal)
	prometheus.MustRegister(ErrorsTotal)
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
