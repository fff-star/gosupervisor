package eventlistener

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/process"
)

func writeEchoListenerScript(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
echo "READY"
read -r line
echo "READY"
printf "RESULT 2\nOK"
`
	path := filepath.Join(dir, "listener.sh")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestEventListenerE2E_SingleEvent(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeEchoListenerScript(t, dir)

	cfg := &config.EventListenerConfig{
		Name:       "e2e_listener",
		Command:    scriptPath,
		Events:     []string{"PROCESS_STATE"},
		BufferSize: 10,
		StopSecs:   1,
	}
	l := newEventListener(cfg)

	l.queue.Push(process.Event{Name: "proc1", Type: process.EventStart, PID: 10, Message: "started"})

	if err := l.start(); err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if l.queue.Len() != 0 {
		t.Errorf("expected empty queue after processing, got %d events", l.queue.Len())
	}

	l.stop()
}

func TestEventListenerE2E_RetryOnFail(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo "READY"
read -r line
echo "READY"
printf "RESULT 4\nFAIL"
echo "READY"
read -r line
echo "READY"
printf "RESULT 2\nOK"
`
	scriptPath := filepath.Join(dir, "retry_listener.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cfg := &config.EventListenerConfig{
		Name:       "retry_listener",
		Command:    scriptPath,
		Events:     []string{"PROCESS_STATE"},
		BufferSize: 10,
		StopSecs:   1,
	}
	l := newEventListener(cfg)

	l.queue.Push(process.Event{Name: "proc1", Type: process.EventStart, PID: 11})

	if err := l.start(); err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Wait for protocol loop to process the event (first FAIL, then retry OK)
	deadline := time.After(5 * time.Second)
	for l.queue.Len() > 0 {
		select {
		case <-deadline:
			t.Fatalf("queue not empty after retry+OK: still %d events", l.queue.Len())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	l.stop()
}

func TestEventListenerE2E_ChildCrash(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "crash_listener.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cfg := &config.EventListenerConfig{
		Name:       "crash_listener",
		Command:    scriptPath,
		Events:     []string{"PROCESS_STATE"},
		BufferSize: 10,
		StopSecs:   1,
	}
	l := newEventListener(cfg)

	if err := l.start(); err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	l.stop()

	// After a crashing child, stop() should clean up and leave state STOPPED.
	l.mu.Lock()
	state := l.state
	l.mu.Unlock()
	if state != "STOPPED" {
		t.Errorf("expected STOPPED after crash+stop, got %q", state)
	}
}
