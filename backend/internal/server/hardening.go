package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
)

// SecurityHeaders sets baseline security response headers on the admin API.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// style-src 'unsafe-inline' and img-src data: are required by the
		// embedded Vuetify SPA (runtime-injected theme styles, inline icons).
		h.Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// rateLimiter is a simple per-key token bucket for abuse-sensitive operations
// (login, boot callbacks). It is not a distributed limiter — sufficient for a
// single-instance deployment.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	capacity float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perSecond, burst float64) *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, rate: perSecond, capacity: burst}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.capacity - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimitLogin throttles authentication attempts per client to blunt
// credential-stuffing. Keyed by the transport's client address.
func RateLimitLogin(perMinute int) middleware.Middleware {
	limiter := newRateLimiter(float64(perMinute)/60.0, float64(perMinute))
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			tr, ok := transport.FromServerContext(ctx)
			if ok && tr.Operation() == "/netboot.v1.AuthService/Login" {
				if !limiter.allow(clientKey(ctx), time.Now()) {
					return nil, ErrTooManyRequests()
				}
			}
			return next(ctx, req)
		}
	}
}

func clientKey(ctx context.Context) string {
	if tr, ok := transport.FromServerContext(ctx); ok {
		if v := tr.RequestHeader().Get("X-Forwarded-For"); v != "" {
			return v
		}
	}
	return "global"
}
