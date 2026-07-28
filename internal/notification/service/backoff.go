package notificationservice

import (
	"math"
	"math/rand"
	"time"
)

// Backoff computes an exponential backoff delay with jitter for the given
// 1-indexed attempt count, capped at maxDelay. It is used by the outbox
// publisher to schedule the next retry (available_at) after a failed
// publish attempt.
//
// The result is randomized to 50%-100% of the theoretical exponential
// delay to avoid thundering-herd retries across many failed rows/replicas.
func Backoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if baseDelay <= 0 {
		baseDelay = time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 5 * time.Minute
	}

	exp := float64(baseDelay) * math.Pow(2, float64(attempt-1))
	if exp > float64(maxDelay) || exp < 0 {
		exp = float64(maxDelay)
	}

	jitterFactor := 0.5 + rand.Float64()*0.5
	delay := time.Duration(exp * jitterFactor)
	if delay > maxDelay {
		delay = maxDelay
	}
	if delay < baseDelay/2 {
		delay = baseDelay / 2
	}
	return delay
}
