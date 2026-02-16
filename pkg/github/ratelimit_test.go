package github

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRateLimiter_Update(t *testing.T) {
	rl := newRateLimiter()

	h := http.Header{}
	resetTime := time.Now().Add(10 * time.Minute)
	h.Set("X-RateLimit-Remaining", "42")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

	rl.update(h)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.remaining != 42 {
		t.Errorf("remaining = %d, want 42", rl.remaining)
	}
	if rl.resetAt.Unix() != resetTime.Unix() {
		t.Errorf("resetAt = %v, want %v", rl.resetAt, resetTime)
	}
}

func TestRateLimiter_WaitDoesNotBlockWhenAboveThreshold(t *testing.T) {
	rl := newRateLimiter()

	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "100")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	rl.update(h)

	done := make(chan struct{})
	go func() {
		rl.wait()
		close(done)
	}()

	select {
	case <-done:
		// good — didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("wait() blocked despite remaining > 10")
	}
}

func TestRateLimiter_HandleRateLimit_RetryAfter(t *testing.T) {
	rl := newRateLimiter()

	h := http.Header{}
	h.Set("Retry-After", "5")
	rl.handleRateLimit(h)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.remaining != 0 {
		t.Errorf("remaining = %d, want 0", rl.remaining)
	}
	if time.Until(rl.resetAt) < 4*time.Second {
		t.Errorf("resetAt too soon: %v", rl.resetAt)
	}
}

func TestRateLimiter_HandleRateLimit_Fallback(t *testing.T) {
	rl := newRateLimiter()

	h := http.Header{}
	rl.handleRateLimit(h)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.remaining != 0 {
		t.Errorf("remaining = %d, want 0", rl.remaining)
	}
	// Should have set resetAt ~60s from now
	if time.Until(rl.resetAt) < 55*time.Second {
		t.Errorf("expected ~60s fallback, got %v", time.Until(rl.resetAt))
	}
}
