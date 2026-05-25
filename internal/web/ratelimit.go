package web

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a simple token-bucket rate limiter per IP.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rps      int
	burst    int
	stopOnce sync.Once
	stop     chan struct{}
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter creates a rate limiter with the given requests-per-second and burst size.
func NewRateLimiter(rps, burst int) *RateLimiter {
	if rps <= 0 {
		rps = 100
	}
	if burst <= 0 {
		burst = rps * 2
	}
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rps:     rps,
		burst:   burst,
		stop:    make(chan struct{}),
	}
	go rl.cleanup(5 * time.Minute)
	return rl
}

func (rl *RateLimiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, b := range rl.buckets {
				if now.Sub(b.lastTime) > interval {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

// Allow returns true if the request from the given IP is within the rate limit.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	now := time.Now()
	if !ok {
		rl.buckets[ip] = &bucket{tokens: float64(rl.burst) - 1, lastTime: now}
		return true
	}

	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * float64(rl.rps)
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stop) })
}

// Middleware returns an HTTP middleware that rate limits by client IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !rl.Allow(ip) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"status":"error","message":"Too Many Requests"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
