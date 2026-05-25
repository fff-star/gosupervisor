package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// mockFlusherWriter implements http.ResponseWriter and http.Flusher,
// bypassing httptest.ResponseRecorder which does not implement Flusher.
type mockFlusherWriter struct {
	http.ResponseWriter
	flushed bool
	buf     bytes.Buffer
	header  http.Header
}

func newMockFlusherWriter() *mockFlusherWriter {
	return &mockFlusherWriter{
		ResponseWriter: httptest.NewRecorder(),
		header:         http.Header{},
	}
}

func (m *mockFlusherWriter) Write(b []byte) (int, error) {
	return m.buf.Write(b)
}

func (m *mockFlusherWriter) Flush() {
	m.flushed = true
}

func (m *mockFlusherWriter) Header() http.Header {
	return m.header
}

func (m *mockFlusherWriter) WriteHeader(code int) {}

// ---------------------------------------------------------------------------
// handleProcessLogsStream (SSE streaming)
// ---------------------------------------------------------------------------

func TestHandleLogsStream_SSEHeaders(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "test.log")
	if err := os.WriteFile(logFile, []byte("line 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ws := &WebServer{logDir: logDir}

	w := newMockFlusherWriter()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled so handleProcessLogsStream exits its loop immediately
	r := httptest.NewRequest("GET", "/stream/test", nil).WithContext(ctx)

	ws.handleProcessLogsStream(w, r, "test")

	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type: expected text/event-stream, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control: expected no-cache, got %q", got)
	}
	if got := w.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection: expected keep-alive, got %q", got)
	}
}

func TestHandleLogsStream_NoLogFile(t *testing.T) {
	ws := &WebServer{logDir: t.TempDir()}
	w := newMockFlusherWriter()
	r := httptest.NewRequest("GET", "/stream/test", nil)

	ws.handleProcessLogsStream(w, r, "test")

	if !strings.Contains(w.buf.String(), "error") {
		t.Errorf("expected error event in response body, got: %q", w.buf.String())
	}
}

func TestHandleLogsStream_ClientDisconnect(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "test.log")
	if err := os.WriteFile(logFile, []byte("line 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ws := &WebServer{logDir: logDir}
	w := newMockFlusherWriter()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — simulates client disconnect before handler runs
	r := httptest.NewRequest("GET", "/stream/test", nil).WithContext(ctx)

	// Must return without hanging or panicking.
	ws.handleProcessLogsStream(w, r, "test")

	// SSE headers should still be set even with cancelled context.
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type: expected text/event-stream, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// handleProcessLogsTailStream (JSON response)
// ---------------------------------------------------------------------------

func TestHandleLogsTailStream_ReadsLastLines(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "test.log")

	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf("log line %d", i))
	}
	if err := os.WriteFile(logFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// The handler calls ws.processManager.GetProcess(name) so we need a
	// non-nil ProcessManager.  Using an empty one means GetProcess returns
	// nil and the fallback logDir/<name>.log path is used.
	logMgr, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	pm := process.NewProcessManager(logMgr)

	ws := &WebServer{logDir: logDir, processManager: pm}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/test/tail?lines=10", nil)

	ws.handleProcessLogsTailStream(w, r, "test", "stdout")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	content, ok := resp["content"].(string)
	if !ok {
		t.Fatal("content field is not a string")
	}
	// With n=10 we should see the last 10 lines: "log line 10" .. "log line 19"
	if !strings.Contains(content, "log line 10") {
		t.Errorf("content does not contain expected line; got: %s", content)
	}
	if strings.Count(content, "log line") != 10 {
		t.Errorf("expected 10 lines in content, got %d lines", strings.Count(content, "log line"))
	}
}

func TestHandleLogsTailStream_NoLogFile(t *testing.T) {
	logDir := t.TempDir()
	logMgr, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	pm := process.NewProcessManager(logMgr)

	ws := &WebServer{logDir: logDir, processManager: pm}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/test/tail", nil)

	ws.handleProcessLogsTailStream(w, r, "nonexistent", "stdout")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing log file, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["status"] != "error" {
		t.Errorf("expected status error, got %v", resp["status"])
	}
}

func TestHandleLogsTailStream_ClientDisconnect(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "test.log")
	if err := os.WriteFile(logFile, []byte("surviving content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	logMgr, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	pm := process.NewProcessManager(logMgr)

	ws := &WebServer{logDir: logDir, processManager: pm}
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/stream/test/tail", nil).WithContext(ctx)

	// Should return the JSON response even with a cancelled context.
	ws.handleProcessLogsTailStream(w, r, "test", "stdout")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	if !strings.Contains(resp["content"].(string), "surviving content") {
		t.Errorf("unexpected content: %v", resp["content"])
	}
}

// ---------------------------------------------------------------------------
// handleProcessLogsTailStream with existing log file and content written over
// time — simulates a log file that was already populated.
// ---------------------------------------------------------------------------

func TestHandleLogsTailStream_ExistingLogFileWithContent(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "test.log")

	// Write a known amount of content.
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(logFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	logMgr, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	pm := process.NewProcessManager(logMgr)

	ws := &WebServer{logDir: logDir, processManager: pm}

	t.Run("default lines count", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/stream/test/tail", nil)
		ws.handleProcessLogsTailStream(w, r, "test", "stdout")

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json decode: %v", err)
		}
		// Default maxLines is 1000, but the file has only 100 lines.
		content := resp["content"].(string)
		if strings.Count(content, "line ") != 100 {
			t.Errorf("expected 100 lines, got %d", strings.Count(content, "line "))
		}
		if _, ok := resp["fileSize"].(float64); !ok {
			t.Errorf("fileSize missing or not a number: %v", resp["fileSize"])
		}
	})

	t.Run("small line count", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/stream/test/tail?lines=5", nil)
		ws.handleProcessLogsTailStream(w, r, "test", "stdout")

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json decode: %v", err)
		}
		content := resp["content"].(string)
		if strings.Count(content, "line ") != 5 {
			t.Errorf("expected 5 lines, got %d; content: %s", strings.Count(content, "line "), content)
		}
		if !strings.HasPrefix(content, "line 95") {
			t.Errorf("expected content to start with 'line 95', got: %s", content)
		}
	})

	t.Run("maxBytes parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/stream/test/tail?maxBytes=100", nil)
		ws.handleProcessLogsTailStream(w, r, "test", "stdout")

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json decode: %v", err)
		}
		content := resp["content"].(string)
		if len(content) > 150 {
			t.Errorf("content length (%d) exceeds expected bound (~100+overhead)", len(content))
		}
	})
}

// ---------------------------------------------------------------------------
// handleProcessLogsTailStream with a process that has a configured stdout
// path (StdoutLogFile) — exercise the GetProcess branch.
// ---------------------------------------------------------------------------

func TestHandleLogsTailStream_WithProcessConfigStdout(t *testing.T) {
	logDir := t.TempDir()
	altLogFile := filepath.Join(logDir, "custom_stdout.log")
	if err := os.WriteFile(altLogFile, []byte("custom stdout\n"), 0644); err != nil {
		t.Fatal(err)
	}

	logMgr, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	pm := process.NewProcessManager(logMgr)
	pm.AddProcess(&config.ProgramConfig{
		Name:          "test",
		Command:       "true",
		StdoutLogFile: altLogFile,
		AutoStart:     false,
	})

	ws := &WebServer{logDir: logDir, processManager: pm}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/test/tail", nil)

	ws.handleProcessLogsTailStream(w, r, "test", "stdout")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !strings.Contains(resp["content"].(string), "custom stdout") {
		t.Errorf("expected custom stdout content, got: %v", resp["content"])
	}
}

func TestHandleLogsTailStream_WithProcessConfigStderr(t *testing.T) {
	logDir := t.TempDir()
	altLogFile := filepath.Join(logDir, "custom_stderr.log")
	if err := os.WriteFile(altLogFile, []byte("custom stderr\n"), 0644); err != nil {
		t.Fatal(err)
	}

	logMgr, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	pm := process.NewProcessManager(logMgr)
	pm.AddProcess(&config.ProgramConfig{
		Name:          "test",
		Command:       "true",
		StderrLogFile: altLogFile,
		AutoStart:     false,
	})

	ws := &WebServer{logDir: logDir, processManager: pm}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream/test/stderr", nil)

	ws.handleProcessLogsTailStream(w, r, "test", "stderr")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !strings.Contains(resp["content"].(string), "custom stderr") {
		t.Errorf("expected custom stderr content, got: %v", resp["content"])
	}
}

// ---------------------------------------------------------------------------
// handleAPIV1EventsStream (SSE event streaming)
// ---------------------------------------------------------------------------

func TestHandleEventStream_SSEHeaders(t *testing.T) {
	ws := &WebServer{}

	w := newMockFlusherWriter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // exit immediately
	r := httptest.NewRequest("GET", "/api/v1/events/stream", nil).WithContext(ctx)

	ws.handleAPIV1EventsStream(w, r)

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control no-cache")
	}
}

func TestHandleEventStream_Broadcast(t *testing.T) {
	ws := &WebServer{}

	w := newMockFlusherWriter()
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/v1/events/stream", nil).WithContext(ctx)

	// Start streaming in background
	done := make(chan struct{})
	go func() {
		ws.handleAPIV1EventsStream(w, r)
		close(done)
	}()

	// Wait for handler to subscribe
	time.Sleep(10 * time.Millisecond)

	// Broadcast an event
	globalSSEBroker.broadcast(process.Event{
		Name:    "test",
		Type:    process.EventStart,
		PID:     42,
		Message: "started",
	})

	// Give it time to deliver
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := w.buf.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data prefix, got: %s", body)
	}
	if !strings.Contains(body, "test") {
		t.Errorf("expected event name in output, got: %s", body)
	}
	if !strings.Contains(body, "EventStart") && !strings.Contains(body, "start") {
		t.Errorf("expected event type in output, got: %s", body)
	}
}

func TestSSEBroker_SubscribeUnsubscribe(t *testing.T) {
	broker := &sseBroker{clients: make(map[chan []byte]bool)}

	ch := broker.subscribe()
	if len(broker.clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(broker.clients))
	}

	broker.unsubscribe(ch)
	if len(broker.clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(broker.clients))
	}
}

func TestSSEBroker_Broadcast(t *testing.T) {
	broker := &sseBroker{clients: make(map[chan []byte]bool)}

	ch := broker.subscribe()

	broker.broadcast(process.Event{
		Name: "proc1", Type: process.EventStart, PID: 1, Message: "started",
	})

	select {
	case data := <-ch:
		s := string(data)
		if !strings.Contains(s, "proc1") {
			t.Errorf("expected proc1 in broadcast, got: %s", s)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for broadcast")
	}
}

func TestSSEBroker_NonBlockingBroadcast(t *testing.T) {
	broker := &sseBroker{clients: make(map[chan []byte]bool)}

	// Small buffer that will fill quickly
	ch := make(chan []byte, 1)
	broker.clients[ch] = true

	// Fill the buffer.
	broker.broadcast(process.Event{Name: "e1"})

	// These should not block, even though buffer is full (non-blocking send).
	// They will be dropped because the buffer is full.
	broker.broadcast(process.Event{Name: "e2"})
	broker.broadcast(process.Event{Name: "e3"})

	// Only e1 should be in the buffer (e2 and e3 were dropped).
	select {
	case data := <-ch:
		if !strings.Contains(string(data), "e1") {
			t.Errorf("expected e1 in broadcast, got: %s", string(data))
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for broadcast")
	}

	// Channel should now be empty (e2/e3 were dropped, not queued).
	select {
	case data := <-ch:
		t.Errorf("expected no more data, got: %s", string(data))
	default:
		// Expected.
	}
}

// ---------------------------------------------------------------------------
// InitSSEBroker / ResetSSEBroker
// ---------------------------------------------------------------------------

func TestInitSSEBroker_SetsOnEvent(t *testing.T) {
	process.SwapOnEvent(nil)
	initSSEBrokerOnce = sync.Once{} // reset: prior tests may have already called InitSSEBroker
	InitSSEBroker()

	// Subscribe a client to receive the event.
	ch := globalSSEBroker.subscribe()

	// Record an event — must not panic and must be broadcast.
	process.RecordEvent("broker_test", process.EventStart, 99, 0, "init test")

	select {
	case data := <-ch:
		if !strings.Contains(string(data), "broker_test") {
			t.Errorf("expected broker_test in event data, got: %s", string(data))
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event broadcast")
	}
	globalSSEBroker.unsubscribe(ch)
}

func TestResetSSEBroker_ReplacesHandler(t *testing.T) {
	called := false
	process.SetOnEvent(func(name string, typ process.EventType, pid int, exitCode int, message string) {
		called = true
	})

	ResetSSEBroker()

	process.RecordEvent("reset_test", process.EventExit, 100, 0, "reset test")

	if !called {
		t.Error("previous OnEvent handler should still be called after ResetSSEBroker")
	}
}

func TestInitSSEBroker_OnlyOnce(t *testing.T) {
	// Reset to a clean state — ResetSSEBroker always re-wires the handler
	// regardless of sync.Once.
	ResetSSEBroker()

	// Calling InitSSEBroker again should be a no-op (sync.Once already
	// consumed by a previous test or by ResetSSEBroker).
	InitSSEBroker()

	// Subscribe a client and record an event.
	ch := globalSSEBroker.subscribe()
	process.RecordEvent("once_test", process.EventStart, 1, 0, "once")
	globalSSEBroker.unsubscribe(ch)

	// Event should have been broadcast to the client.
	select {
	case data := <-ch:
		if !strings.Contains(string(data), "once_test") {
			t.Errorf("expected event data containing 'once_test', got: %s", string(data))
		}
	default:
		t.Error("expected event to be broadcast, but channel was empty")
	}
}
