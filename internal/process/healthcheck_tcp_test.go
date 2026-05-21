package process

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

func TestCheckHealth_TCPDialSuccess(t *testing.T) {
	// Start a TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Accept connections so the dial doesn't hang
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().String()
	ok := checkHealth("tcp://"+addr, 1*time.Second)
	if !ok {
		t.Error("TCP health check should succeed when port is listening")
	}
}

func TestCheckHealth_TCPDialRefused(t *testing.T) {
	ok := checkHealth("tcp://127.0.0.1:19999", 500*time.Millisecond)
	if ok {
		t.Error("TCP health check should fail when port is not listening")
	}
}

func TestCheckHealth_TCPTimeout(t *testing.T) {
	// Use a link-local address (169.254.x.x) that is non-routable and should
	// reliably produce a timeout rather than immediate connection refused.
	ok := checkHealth("tcp://169.254.255.255:12345", 100*time.Millisecond)
	if ok {
		t.Error("TCP health check should fail on timeout")
	}
}

func TestCheckHealth_HTTP2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer ts.Close()

	ok := checkHealth(ts.URL, 1*time.Second)
	if !ok {
		t.Error("HTTP 200 should return healthy")
	}
}

func TestCheckHealth_HTTP3xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(302)
	}))
	defer ts.Close()

	ok := checkHealth(ts.URL, 1*time.Second)
	if !ok {
		t.Error("HTTP 302 should return healthy")
	}
}

func TestCheckHealth_HTTP4xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	ok := checkHealth(ts.URL, 1*time.Second)
	if ok {
		t.Error("HTTP 404 should return unhealthy")
	}
}

func TestCheckHealth_HTTP5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	ok := checkHealth(ts.URL, 1*time.Second)
	if ok {
		t.Error("HTTP 500 should return unhealthy")
	}
}

func TestCheckHealth_HTTPTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer ts.Close()

	ok := checkHealth(ts.URL, 100*time.Millisecond)
	if ok {
		t.Error("Slow server should time out and return unhealthy")
	}
}

func TestRunHealthCheck_ThresholdReached(t *testing.T) {
	// Create a server that always returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	logDir := "./test_logs_hc_threshold"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                      "hc_threshold",
		Command:                   "sleep 60",
		AutoStart:                 false,
		AutoRestart:               false,
		HealthCheckURL:            srv.URL,
		HealthCheckInterval:       1,
		HealthCheckTimeout:        5,
		HealthCheckUnhealthyThreshold: 3,
		StartSecs:                 0,
		StartRetries:              3,
		StopSignal:                "SIGTERM",
		StopSecs:                  10,
		Environment:               make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_threshold")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() {
		p.mu.Lock()
		p.healthCheckCancel()
		p.mu.Unlock()
		p.Stop()
	}()

	// Wait long enough for at least 3 health check ticks to accumulate failures
	time.Sleep(3500 * time.Millisecond)

	p.mu.Lock()
	failures := p.healthCheckFailures
	healthy := p.Healthy
	p.mu.Unlock()

	if failures < 3 {
		t.Errorf("expected at least 3 health check failures, got %d", failures)
	}
	if healthy {
		t.Error("process should be marked unhealthy after threshold reached")
	}
}

func TestRunHealthCheck_ThresholdNotReached(t *testing.T) {
	// Create a server that always returns 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logDir := "./test_logs_hc_threshold_not"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                      "hc_threshold_not",
		Command:                   "sleep 60",
		AutoStart:                 false,
		AutoRestart:               false,
		HealthCheckURL:            srv.URL,
		HealthCheckInterval:       1,
		HealthCheckTimeout:        5,
		HealthCheckUnhealthyThreshold: 3,
		StartSecs:                 0,
		StartRetries:              3,
		StopSignal:                "SIGTERM",
		StopSecs:                  10,
		Environment:               make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_threshold_not")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() {
		p.mu.Lock()
		p.healthCheckCancel()
		p.mu.Unlock()
		p.Stop()
	}()

	// Wait long enough for health checks to run but threshold should not be reached
	time.Sleep(1500 * time.Millisecond)

	p.mu.Lock()
	healthy := p.Healthy
	failures := p.healthCheckFailures
	p.mu.Unlock()

	if !healthy {
		t.Error("process should remain healthy when server returns 200")
	}
	if failures != 0 {
		t.Errorf("expected 0 health check failures, got %d", failures)
	}
}

func TestRunHealthCheck_CtxCancelledStopsGoroutine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logDir := "./test_logs_hc_ctxcancel"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                "hc_ctxcancel",
		Command:             "sleep 60",
		AutoStart:           false,
		AutoRestart:         false,
		HealthCheckURL:      srv.URL,
		HealthCheckInterval: 1,
		HealthCheckTimeout:  5,
		StartSecs:           0,
		StartRetries:        3,
		StopSignal:          "SIGTERM",
		StopSecs:            10,
		Environment:         make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_ctxcancel")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Allow one tick
	time.Sleep(100 * time.Millisecond)

	// Cancel the health check context
	p.mu.Lock()
	p.healthCheckCancel()
	p.mu.Unlock()

	// Wait to ensure no extra ticks run after cancel
	time.Sleep(1500 * time.Millisecond)

	p.mu.Lock()
	failures := p.healthCheckFailures
	p.mu.Unlock()

	// The health check goroutine should have stopped promptly;
	// we might get 0 or 1 tick before cancel takes effect, but should not
	// get many more.
	if failures > 2 {
		t.Errorf("health check goroutine continued after context cancel, failures=%d", failures)
	}

	p.Stop()
}

func TestRunHealthCheck_FailureThenRestore(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		c := callCount
		mu.Unlock()
		// First 3 calls return 500 (unhealthy), then switch to 200 (healthy)
		if c <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	logDir := "./test_logs_hc_restore"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                      "hc_restore",
		Command:                   "sleep 60",
		AutoStart:                 false,
		AutoRestart:               false,
		HealthCheckURL:            srv.URL,
		HealthCheckInterval:       1,
		HealthCheckTimeout:        5,
		HealthCheckUnhealthyThreshold: 3,
		StartSecs:                 0,
		StartRetries:              3,
		StopSignal:                "SIGTERM",
		StopSecs:                  10,
		Environment:               make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_restore")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() {
		p.mu.Lock()
		p.healthCheckCancel()
		p.mu.Unlock()
		p.Stop()
	}()

	// Wait for unhealthy -> threshold -> then switch to healthy
	time.Sleep(5 * time.Second)

	p.mu.Lock()
	healthy := p.Healthy
	failures := p.healthCheckFailures
	p.mu.Unlock()

	if !healthy {
		t.Error("process should be restored to healthy after server recovers")
	}
	if failures != 0 {
		t.Errorf("health check failures should be 0 after restoration, got %d", failures)
	}
}
