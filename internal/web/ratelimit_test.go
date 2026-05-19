package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewRateLimiterDefaults(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	defer rl.Stop()

	if rl.rps != 100 {
		t.Errorf("expected default rps=100, got %d", rl.rps)
	}
	if rl.burst != 200 {
		t.Errorf("expected default burst=200, got %d", rl.burst)
	}
}

func TestNewRateLimiterCustom(t *testing.T) {
	rl := NewRateLimiter(5, 10)
	defer rl.Stop()

	if rl.rps != 5 {
		t.Errorf("expected rps=5, got %d", rl.rps)
	}
	if rl.burst != 10 {
		t.Errorf("expected burst=10, got %d", rl.burst)
	}
}

func TestRateLimiterAllowFirstRequest(t *testing.T) {
	rl := NewRateLimiter(10, 20)
	defer rl.Stop()

	if !rl.Allow("192.168.1.1") {
		t.Error("first request should be allowed")
	}
}

func TestRateLimiterExhaustTokenBucket(t *testing.T) {
	rl := NewRateLimiter(1, 3) // 3 burst, 1 rps
	defer rl.Stop()

	ip := "10.0.0.1"

	// First 3 requests should be allowed (burst)
	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("request %d within burst should be allowed", i+1)
		}
	}

	// 4th request should be denied (token bucket empty)
	if rl.Allow(ip) {
		t.Error("request after burst exhausted should be denied")
	}
}

func TestRateLimiterTokenRefill(t *testing.T) {
	rl := NewRateLimiter(10, 1) // burst=1, rps=10
	defer rl.Stop()

	ip := "10.0.0.2"

	// Use the single token
	if !rl.Allow(ip) {
		t.Fatal("first request should be allowed")
	}

	// Immediately next should fail
	if rl.Allow(ip) {
		t.Error("second request without refill should be denied")
	}

	// Wait for refill (~100ms for 10 rps gives 1 token)
	time.Sleep(150 * time.Millisecond)

	if !rl.Allow(ip) {
		t.Error("request after refill should be allowed")
	}
}

func TestRateLimiterSeparateIPs(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	// Use up IP 1's token
	if !rl.Allow("10.0.0.1") {
		t.Fatal("first IP should be allowed")
	}
	if rl.Allow("10.0.0.1") {
		t.Error("first IP should be denied (token exhausted)")
	}

	// IP 2 should still have its own token
	if !rl.Allow("10.0.0.2") {
		t.Error("second IP should have independent token bucket")
	}
}

func TestRateLimiterXForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// First request with X-Forwarded-For should pass
	req1 := httptest.NewRequest("GET", "/api/test", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	req1.Header.Set("X-Forwarded-For", "192.168.1.100")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Errorf("first request should pass, got %d", w1.Code)
	}

	// Second request with same X-Forwarded-For should be rate limited
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	req2.RemoteAddr = "10.0.0.2:12345" // different RemoteAddr
	req2.Header.Set("X-Forwarded-For", "192.168.1.100") // same forwarded IP
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request with same forwarded IP should be 429, got %d", w2.Code)
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	rl := NewRateLimiter(100, 100)
	defer rl.Stop()

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rl.Allow("10.0.0.1")
			}
		}()
	}
	wg.Wait()

	// Should not panic under concurrent access
	rl.Allow("10.0.0.1")
}

func TestRateLimiterMiddlewareReturns429(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// First request: allowed
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("first request should be 200, got %d", w1.Code)
	}

	// Second request: rate limited
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "10.0.0.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") != "1" {
		t.Error("expected Retry-After header")
	}
	body := strings.TrimSpace(w2.Body.String())
	if body != `{"status":"error","message":"Too Many Requests"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRateLimiterStop(t *testing.T) {
	rl := NewRateLimiter(10, 20)
	rl.Stop()

	// Second Stop should not panic (sync.Once)
	rl.Stop()
}
