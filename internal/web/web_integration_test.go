package web

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// newIntegrationPM creates a ProcessManager with test processes and returns
// it along with a cleanup function.
func newIntegrationPM(t *testing.T) (*process.ProcessManager, func()) {
	t.Helper()
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	pm := process.NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:         "test1",
		Command:      "sleep 120",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		StopSignal:   "SIGTERM",
		StopSecs:     1,
		Environment:  make(map[string]string),
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:         "test2",
		Command:      "sleep 120",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		StopSignal:   "SIGTERM",
		StopSecs:     1,
		Environment:  make(map[string]string),
	})
	cleanup := func() {
		// Stop any running processes
		pm.RangeProcesses(func(_ string, p *process.Process) {
			st := p.GetState()
			if st == process.StateRunning || st == process.StateStarting {
				p.Stop()
			}
		})
		logManager.Close()
	}
	return pm, cleanup
}

// startWebServer starts a WebServer on a random port and returns the base URL.
func startWebServer(t *testing.T, ws *WebServer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() { _ = ws.Start(addr) }()

	// Poll until the server is ready to accept connections
	for i := 0; i < 100; i++ {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return "http://" + addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start within timeout")
	return ""
}

func TestWebServerRealHTTP_ProcessesAPI(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	logDir := t.TempDir()
	ws, err := NewWebServer(pm, logDir)
	if err != nil {
		t.Fatalf("NewWebServer: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	resp, err := http.Get(baseURL + "/api/v1/processes")
	if err != nil {
		t.Fatalf("GET processes: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
	if body["processes"] == nil {
		t.Error("expected processes field")
	}
}

func TestWebServerRealHTTP_ProcessAction(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	logDir := t.TempDir()
	ws, err := NewWebServer(pm, logDir)
	if err != nil {
		t.Fatalf("NewWebServer: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	// POST start action — must set Origin header for CSRF check
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/processes/test1/start", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", baseURL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST start: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected ok status, got %v", body)
	}

	// Wait for process to actually start
	time.Sleep(500 * time.Millisecond)

	// GET single process
	resp2, err := http.Get(baseURL + "/api/v1/processes/test1")
	if err != nil {
		t.Fatalf("GET process: %v", err)
	}
	defer resp2.Body.Close()

	var body2 map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&body2)
	if body2["status"] != "ok" {
		t.Errorf("expected ok status, got %v", body2["status"])
	}

	// Stop the process for cleanup
	reqStop, _ := http.NewRequest("POST", baseURL+"/api/v1/processes/test1/stop", nil)
	reqStop.Header.Set("Origin", baseURL)
	http.DefaultClient.Do(reqStop)
}

func TestWebServerRealHTTP_SystemAPI(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	logDir := t.TempDir()
	ws, err := NewWebServer(pm, logDir)
	if err != nil {
		t.Fatalf("NewWebServer: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	resp, err := http.Get(baseURL + "/api/v1/system")
	if err != nil {
		t.Fatalf("GET system: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected ok status, got %v", body["status"])
	}
}

func TestWebServerRealHTTP_EventsAPI(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	// Push some events into the global buffer
	process.RecordEvent("test1", process.EventStart, 100, 0, "started")
	process.RecordEvent("test1", process.EventStop, 100, 0, "stopped")
	process.RecordEvent("test2", process.EventFatal, 200, 1, "max retries")

	logDir := t.TempDir()
	ws, err := NewWebServer(pm, logDir)
	if err != nil {
		t.Fatalf("NewWebServer: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	resp, err := http.Get(baseURL + "/api/v1/events?limit=5")
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected ok status, got %v", body["status"])
	}
	events, ok := body["events"].([]interface{})
	if !ok || len(events) == 0 {
		t.Error("expected non-empty events array")
	}
}

func TestWebServerRealHTTP_AuthRequired(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	ws, err := NewWebServerWithAuth(pm, t.TempDir(), "admin", "secret", true)
	if err != nil {
		t.Fatalf("NewWebServerWithAuth: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	// Request without auth should get 401
	resp, err := http.Get(baseURL + "/api/v1/processes")
	if err != nil {
		t.Fatalf("GET processes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// Request with correct auth should get 200
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/processes", nil)
	req.SetBasicAuth("admin", "secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with auth: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with auth, got %d", resp2.StatusCode)
	}

	// Verify Content-Type header
	ct := resp2.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestWebServerRealHTTP_AuthBypassAPI(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	// apiAuth=false means API routes skip auth
	ws, err := NewWebServerWithAuth(pm, t.TempDir(), "admin", "secret", false)
	if err != nil {
		t.Fatalf("NewWebServerWithAuth: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	// API should work without auth when apiAuth is false
	resp, err := http.Get(baseURL + "/api/v1/processes")
	if err != nil {
		t.Fatalf("GET processes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 without auth (apiAuth=false), got %d", resp.StatusCode)
	}
}

func TestWebServerRealHTTP_CORSHeaders(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	ws, err := NewWebServerFull(pm, t.TempDir(), "", "", false, "http://example.com", 0)
	if err != nil {
		t.Fatalf("NewWebServerFull with CORS: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	// OPTIONS preflight
	req, _ := http.NewRequest("OPTIONS", baseURL+"/api/v1/processes", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer resp.Body.Close()

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "http://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin 'http://example.com', got '%s'", acao)
	}

	// GET should also have CORS header
	resp2, err := http.Get(baseURL + "/api/v1/processes")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()
	acao2 := resp2.Header.Get("Access-Control-Allow-Origin")
	if acao2 != "http://example.com" {
		t.Errorf("expected CORS header on GET, got '%s'", acao2)
	}
}

func TestWebServerRealHTTP_IndexPage(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	ws, err := NewWebServer(pm, t.TempDir())
	if err != nil {
		t.Fatalf("NewWebServer: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET index: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) < 100 {
		t.Error("expected non-trivial HTML body")
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html, got '%s'", ct)
	}
}

func TestWebServerRealHTTP_NotFound(t *testing.T) {
	pm, cleanup := newIntegrationPM(t)
	defer cleanup()

	ws, err := NewWebServer(pm, t.TempDir())
	if err != nil {
		t.Fatalf("NewWebServer: %v", err)
	}
	defer ws.Stop()

	baseURL := startWebServer(t, ws)

	// Non-existent process via API should return 404
	resp, err := http.Get(baseURL + "/api/v1/processes/nonexistent")
	if err != nil {
		t.Fatalf("GET nonexistent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent process, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json for 404, got %s", ct)
	}
}

