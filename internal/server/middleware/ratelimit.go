package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a per-client-IP token bucket. Stale buckets are evicted by a
// background sweeper so memory does not grow unbounded with unique clients.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rps,
		burst:   float64(burst),
	}
	go rl.sweep()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b := rl.buckets[ip]
	if b == nil {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[ip] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) sweep() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		rl.mu.Lock()
		for ip, b := range rl.buckets {
			if b.last.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware limits requests per client IP to rps (with the given
// burst). It keys on the direct connection address (RemoteAddr), which is not
// spoofable, unlike X-Forwarded-For.
func RateLimitMiddleware(rps float64, burst int) MiddlewareFunc {
	rl := newRateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
