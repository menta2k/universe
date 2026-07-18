package server

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenBlocks(t *testing.T) {
	l := newRateLimiter(1, 3) // 1/s, burst 3
	now := time.Now()
	for i := range 3 {
		if !l.allow("k", now) {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.allow("k", now) {
		t.Error("4th request in the same instant must be blocked")
	}
	// After 2s, 2 tokens have refilled.
	if !l.allow("k", now.Add(2*time.Second)) {
		t.Error("request after refill should be allowed")
	}
}

func TestRateLimiterIsPerKey(t *testing.T) {
	l := newRateLimiter(1, 1)
	now := time.Now()
	if !l.allow("a", now) || !l.allow("b", now) {
		t.Error("distinct keys must not share a bucket")
	}
	if l.allow("a", now) {
		t.Error("second request for key a must be blocked")
	}
}
