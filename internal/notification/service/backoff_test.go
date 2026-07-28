package notificationservice_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	notificationservice "notification-service/internal/notification/service"
)

func TestBackoff_Monotonic(t *testing.T) {
	base := 1 * time.Second
	max := 1 * time.Minute
	for attempt := 1; attempt <= 10; attempt++ {
		d := notificationservice.Backoff(attempt, base, max)
		require.GreaterOrEqual(t, d, time.Duration(0))
		require.LessOrEqual(t, d, max)
	}
}

func TestBackoff_CapsAtMax(t *testing.T) {
	d := notificationservice.Backoff(100, time.Second, 10*time.Second)
	require.LessOrEqual(t, d, 10*time.Second)
}

func TestBackoff_ZeroOrNegativeAttempt(t *testing.T) {
	d := notificationservice.Backoff(0, time.Second, time.Minute)
	require.Greater(t, d, time.Duration(0))
}
