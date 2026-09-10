package connect

import (
	"context"
	"sync"
	"time"
)

// tokenBucket is a small context-aware token-bucket limiter implemented with
// the standard library. It permits short bursts while enforcing the configured
// average request rate across concurrent workers.
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(requestsPerSecond float64, burst int) *tokenBucket {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 10
	}
	if burst <= 0 {
		burst = 10
	}
	now := time.Now()
	return &tokenBucket{
		rate:   requestsPerSecond,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   now,
	}
}

func (l *tokenBucket) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.last).Seconds()
		if elapsed > 0 {
			l.tokens += elapsed * l.rate
			if l.tokens > l.burst {
				l.tokens = l.burst
			}
			l.last = now
		}
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}
