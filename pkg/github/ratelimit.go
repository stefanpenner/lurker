package github

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateLimiter tracks GitHub API rate limits from response headers.
type rateLimiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{remaining: -1} // -1 = unknown, don't block
}

// update reads X-RateLimit-Remaining and X-RateLimit-Reset from response headers.
func (rl *rateLimiter) update(h http.Header) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if v := h.Get("X-RateLimit-Remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rl.remaining = n
		}
	}
	if v := h.Get("X-RateLimit-Reset"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			rl.resetAt = time.Unix(epoch, 0)
		}
	}
}

// wait blocks if remaining requests are below the safety threshold.
func (rl *rateLimiter) wait() {
	rl.mu.Lock()
	remaining := rl.remaining
	resetAt := rl.resetAt
	rl.mu.Unlock()

	if remaining >= 0 && remaining < 10 && time.Now().Before(resetAt) {
		delay := time.Until(resetAt) + time.Second // +1s buffer
		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

// handleRateLimit processes a 429/403-rate-limit response.
func (rl *rateLimiter) handleRateLimit(h http.Header) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.remaining = 0

	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			rl.resetAt = time.Now().Add(time.Duration(secs) * time.Second)
			return
		}
	}

	if v := h.Get("X-RateLimit-Reset"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			rl.resetAt = time.Unix(epoch, 0)
			return
		}
	}

	// Fallback: wait 60 seconds
	rl.resetAt = time.Now().Add(60 * time.Second)
}
