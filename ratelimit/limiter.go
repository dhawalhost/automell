package ratelimit

import (
	"context"
	"sync"
	"time"
)

// SlidingWindowLimiter implements a sliding window rate limiter
type SlidingWindowLimiter struct {
	requests    []time.Time
	maxRequests int
	window      time.Duration
	mu          sync.Mutex
}

// NewSlidingWindowLimiter creates a new sliding window rate limiter
func NewSlidingWindowLimiter(maxRequests int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		requests:    make([]time.Time, 0),
		maxRequests: maxRequests,
		window:      window,
	}
}

// Wait blocks until a request is allowed under the rate limit.
// Deprecated: prefer WaitContext so callers can cancel on disconnect.
func (l *SlidingWindowLimiter) Wait() {
	_ = l.WaitContext(context.Background())
}

// WaitContext blocks until a request slot is available or ctx is cancelled.
// Returns ctx.Err() if the context is cancelled while waiting.
func (l *SlidingWindowLimiter) WaitContext(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		now := time.Now()
		cutoff := now.Add(-l.window)

		// Remove requests outside the window
		valid := 0
		for _, req := range l.requests {
			if req.After(cutoff) {
				l.requests[valid] = req
				valid++
			}
		}
		l.requests = l.requests[:valid]

		if len(l.requests) < l.maxRequests {
			// Slot available — record and return
			l.requests = append(l.requests, now)
			return nil
		}

		// Wait until the oldest request falls out of the window
		waitTime := l.requests[0].Add(l.window).Sub(now)
		if waitTime <= 0 {
			// Oldest already expired; loop again to re-prune
			continue
		}

		l.mu.Unlock()
		select {
		case <-time.After(waitTime):
		case <-ctx.Done():
			l.mu.Lock()
			return ctx.Err()
		}
		l.mu.Lock()
	}
}

// ConcurrencyLimiter limits the number of concurrent requests
type ConcurrencyLimiter struct {
	semaphore chan struct{}
}

// NewConcurrencyLimiter creates a new concurrency limiter
func NewConcurrencyLimiter(maxConcurrent int) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

// Acquire acquires a permit, blocking if none are available
func (l *ConcurrencyLimiter) Acquire() {
	l.semaphore <- struct{}{}
}

// Release releases a permit
func (l *ConcurrencyLimiter) Release() {
	<-l.semaphore
}
