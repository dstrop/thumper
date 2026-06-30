package amqp

import (
	"context"
	"time"
)

// wait sleeps for d and reports whether the wait ran to completion.
func wait(ctx context.Context, done <-chan struct{}, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	case <-done:
		return false
	}
}

const maxBackoff = 30 * time.Second

// backoff returns an exponential delay for the given 1-based attempt: base
// doubled each attempt, capped at maxBackoff.
func backoff(base time.Duration, attempt int) time.Duration {
	d := base << (attempt - 1) // base * 2^(attempt-1)
	if d < base || d > maxBackoff {
		return maxBackoff // overflowed or exceeded the cap
	}
	return d
}
