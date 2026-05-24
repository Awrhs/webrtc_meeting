package middleware

import (
	"testing"
	"time"
)

func TestIPLimiterAllowsWithinLimit(t *testing.T) {
	limiter := NewIPLimiter(2, time.Minute)
	defer limiter.Stop()
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected first request to be allowed")
	}
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected second request to be allowed")
	}
}

func TestIPLimiterRejectsOverLimit(t *testing.T) {
	limiter := NewIPLimiter(1, time.Minute)
	defer limiter.Stop()
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected first request to be allowed")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("expected second request to be rejected")
	}
}

func TestIPLimiterResetsAfterWindow(t *testing.T) {
	limiter := NewIPLimiter(1, 10*time.Millisecond)
	defer limiter.Stop()
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected first request to be allowed")
	}
	time.Sleep(15 * time.Millisecond)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected request after window to be allowed")
	}
}

func TestIPLimiterDisabledWhenLimitNonPositive(t *testing.T) {
	limiter := NewIPLimiter(0, time.Minute)
	defer limiter.Stop()
	for i := 0; i < 10; i++ {
		if !limiter.Allow("127.0.0.1") {
			t.Fatalf("expected disabled limiter to allow request %d", i)
		}
	}
}

func TestIPLimiterEvictsStaleEntries(t *testing.T) {
	window := 10 * time.Millisecond
	limiter := NewIPLimiter(10, window)
	defer limiter.Stop()

	limiter.Allow("192.0.2.1")
	limiter.Allow("192.0.2.2")

	time.Sleep(window*3 + 5*time.Millisecond)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if len(limiter.visitors) != 0 {
		t.Fatalf("expected stale visitors to be evicted, got %d", len(limiter.visitors))
	}
}

func TestIPLimiterStopIsIdempotent(t *testing.T) {
	limiter := NewIPLimiter(1, time.Minute)
	limiter.Stop()
	limiter.Stop()

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected limiter to keep allowing first request after stop")
	}
}
