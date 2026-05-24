package process

import (
	"sync"
	"testing"
	"time"
)

func TestNewEventBuffer(t *testing.T) {
	eb := NewEventBuffer(10)
	if eb == nil {
		t.Fatal("NewEventBuffer returned nil")
	}
	if eb.Len() != 0 {
		t.Errorf("expected length 0, got %d", eb.Len())
	}
}

func TestEventBufferPushSnapshot(t *testing.T) {
	eb := NewEventBuffer(10)

	e1 := Event{Timestamp: time.Now(), Name: "p1", Type: EventStart, PID: 1}
	e2 := Event{Timestamp: time.Now(), Name: "p1", Type: EventStop, PID: 1}
	e3 := Event{Timestamp: time.Now(), Name: "p1", Type: EventExit, PID: 1, ExitCode: 1}

	eb.Push(e1)
	eb.Push(e2)
	eb.Push(e3)

	if eb.Len() != 3 {
		t.Errorf("expected length 3, got %d", eb.Len())
	}

	snapshot := eb.Snapshot(0)
	if len(snapshot) != 3 {
		t.Errorf("expected 3 events, got %d", len(snapshot))
	}
	if snapshot[0].Type != EventStart {
		t.Errorf("expected first event Start, got %s", snapshot[0].Type)
	}
	if snapshot[2].Type != EventExit {
		t.Errorf("expected last event Exit, got %s", snapshot[2].Type)
	}
}

func TestEventBufferOverflow(t *testing.T) {
	eb := NewEventBuffer(5)

	for i := 0; i < 10; i++ {
		eb.Push(Event{Timestamp: time.Now(), Name: "p1", Type: EventStart, PID: i})
	}

	if eb.Len() != 5 {
		t.Errorf("expected length 5 after overflow, got %d", eb.Len())
	}

	snapshot := eb.Snapshot(0)
	if len(snapshot) != 5 {
		t.Errorf("expected 5 events, got %d", len(snapshot))
	}
	// First event should be PID 5 (oldest in buffer after overflow)
	if snapshot[0].PID != 5 {
		t.Errorf("expected first PID 5 (oldest), got %d", snapshot[0].PID)
	}
	// Last event should be PID 9
	if snapshot[4].PID != 9 {
		t.Errorf("expected last PID 9, got %d", snapshot[4].PID)
	}
}

func TestEventBufferSnapshotLimit(t *testing.T) {
	eb := NewEventBuffer(100)
	for i := 0; i < 50; i++ {
		eb.Push(Event{Timestamp: time.Now(), Name: "p1", Type: EventStart, PID: i})
	}

	snapshot := eb.Snapshot(10)
	if len(snapshot) != 10 {
		t.Errorf("expected 10 events with limit, got %d", len(snapshot))
	}
	// Should have the last 10 events (PID 40-49)
	if snapshot[0].PID != 40 {
		t.Errorf("expected first PID 40, got %d", snapshot[0].PID)
	}
	if snapshot[9].PID != 49 {
		t.Errorf("expected last PID 49, got %d", snapshot[9].PID)
	}
}

func TestParseSignal(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"SIGTERM", true},
		{"SIGKILL", true},
		{"SIGHUP", true},
		{"SIGUSR1", true},
		{"SIGUSR2", true},
		{"SIGINT", true},
		{"SIGQUIT", true},
		{"INVALID", false},
		{"", false},
	}

	for _, tt := range tests {
		_, ok := ParseSignal(tt.name)
		if ok != tt.expected {
			t.Errorf("ParseSignal(%q) = %v, want %v", tt.name, ok, tt.expected)
		}
	}
}

func TestRecordEvent(t *testing.T) {
	// Reset global buffer
	oldBuffer := GlobalEventBuffer
	GlobalEventBuffer = NewEventBuffer(500)
	defer func() { GlobalEventBuffer = oldBuffer }()

	RecordEvent("test", EventStart, 123, 0, "started")
	RecordEvent("test", EventStop, 123, 0, "stopped")

	snapshot := GlobalEventBuffer.Snapshot(0)
	if len(snapshot) != 2 {
		t.Errorf("expected 2 events, got %d", len(snapshot))
	}
	if snapshot[0].Name != "test" {
		t.Errorf("expected name 'test', got %s", snapshot[0].Name)
	}
}

func TestRecordEventFatal(t *testing.T) {
	oldBuffer := GlobalEventBuffer
	GlobalEventBuffer = NewEventBuffer(500)
	defer func() { GlobalEventBuffer = oldBuffer }()

	RecordEvent("proc1", EventFatal, 42, 1, "max retries")

	snapshot := GlobalEventBuffer.Snapshot(0)
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 event, got %d", len(snapshot))
	}
	e := snapshot[0]
	if e.Type != EventFatal {
		t.Errorf("expected EventFatal, got %s", e.Type)
	}
	if e.Name != "proc1" {
		t.Errorf("expected name 'proc1', got %s", e.Name)
	}
	if e.PID != 42 {
		t.Errorf("expected PID 42, got %d", e.PID)
	}
	if e.ExitCode != 1 {
		t.Errorf("expected ExitCode 1, got %d", e.ExitCode)
	}
	if e.Message != "max retries" {
		t.Errorf("expected message 'max retries', got %s", e.Message)
	}
}

func TestRecordEventHealthFail(t *testing.T) {
	oldBuffer := GlobalEventBuffer
	GlobalEventBuffer = NewEventBuffer(500)
	defer func() { GlobalEventBuffer = oldBuffer }()

	RecordEvent("proc2", EventHealthFail, 99, 0, "health check failed")

	snapshot := GlobalEventBuffer.Snapshot(0)
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 event, got %d", len(snapshot))
	}
	e := snapshot[0]
	if e.Type != EventHealthFail {
		t.Errorf("expected EventHealthFail, got %s", e.Type)
	}
	if e.Name != "proc2" {
		t.Errorf("expected name 'proc2', got %s", e.Name)
	}
	if e.PID != 99 {
		t.Errorf("expected PID 99, got %d", e.PID)
	}
	if e.ExitCode != 0 {
		t.Errorf("expected ExitCode 0, got %d", e.ExitCode)
	}
	if e.Message != "health check failed" {
		t.Errorf("expected message 'health check failed', got %s", e.Message)
	}
}

func TestRecordEventHealthRestore(t *testing.T) {
	oldBuffer := GlobalEventBuffer
	GlobalEventBuffer = NewEventBuffer(500)
	defer func() { GlobalEventBuffer = oldBuffer }()

	RecordEvent("proc3", EventHealthRestore, 77, 0, "health restored")

	snapshot := GlobalEventBuffer.Snapshot(0)
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 event, got %d", len(snapshot))
	}
	e := snapshot[0]
	if e.Type != EventHealthRestore {
		t.Errorf("expected EventHealthRestore, got %s", e.Type)
	}
	if e.Name != "proc3" {
		t.Errorf("expected name 'proc3', got %s", e.Name)
	}
	if e.PID != 77 {
		t.Errorf("expected PID 77, got %d", e.PID)
	}
	if e.ExitCode != 0 {
		t.Errorf("expected ExitCode 0, got %d", e.ExitCode)
	}
	if e.Message != "health restored" {
		t.Errorf("expected message 'health restored', got %s", e.Message)
	}
}

func TestRecordEventSignal(t *testing.T) {
	oldBuffer := GlobalEventBuffer
	GlobalEventBuffer = NewEventBuffer(500)
	defer func() { GlobalEventBuffer = oldBuffer }()

	RecordEvent("proc4", EventSignal, 55, 0, "signal SIGTERM")

	snapshot := GlobalEventBuffer.Snapshot(0)
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 event, got %d", len(snapshot))
	}
	e := snapshot[0]
	if e.Type != EventSignal {
		t.Errorf("expected EventSignal, got %s", e.Type)
	}
	if e.Name != "proc4" {
		t.Errorf("expected name 'proc4', got %s", e.Name)
	}
	if e.PID != 55 {
		t.Errorf("expected PID 55, got %d", e.PID)
	}
	if e.ExitCode != 0 {
		t.Errorf("expected ExitCode 0, got %d", e.ExitCode)
	}
	if e.Message != "signal SIGTERM" {
		t.Errorf("expected message 'signal SIGTERM', got %s", e.Message)
	}
}

func TestSetOnEvent_CalledByRecordEvent(t *testing.T) {
	var calledName string
	var calledType EventType
	var mu sync.Mutex

	SetOnEvent(func(name string, typ EventType, pid int, exitCode int, message string) {
		mu.Lock()
		calledName = name
		calledType = typ
		mu.Unlock()
	})

	RecordEvent("hook_test", EventStart, 42, 0, "test message")

	mu.Lock()
	if calledName != "hook_test" {
		t.Errorf("expected calledName='hook_test', got %q", calledName)
	}
	if calledType != EventStart {
		t.Errorf("expected calledType=EventStart, got %q", calledType)
	}
	mu.Unlock()

	// Clean up
	SetOnEvent(nil)
}

func TestSwapOnEvent_ReturnsPrevious(t *testing.T) {
	SetOnEvent(nil)

	var fn1Called, fn2Called bool
	fn1 := func(name string, typ EventType, pid int, exitCode int, message string) {
		fn1Called = true
	}
	fn2 := func(name string, typ EventType, pid int, exitCode int, message string) {
		fn2Called = true
	}

	prev1 := SwapOnEvent(fn1)
	if prev1 != nil {
		t.Error("SwapOnEvent on nil should return nil")
	}
	RecordEvent("test", EventStart, 1, 0, "")
	if !fn1Called {
		t.Error("fn1 should be called after SwapOnEvent(fn1)")
	}

	prev2 := SwapOnEvent(fn2)
	if prev2 == nil {
		t.Error("SwapOnEvent should return previous function")
	}
	RecordEvent("test", EventExit, 2, 0, "")
	if !fn2Called {
		t.Error("fn2 should be called after SwapOnEvent(fn2)")
	}
	// fn1 should NOT be called again — it was swapped out
	fn1CalledAfter := fn1Called
	RecordEvent("test", EventStop, 3, 0, "")
	// fn1Called won't change since fn1 was swapped out; fn2Called should stay true
	_ = fn1CalledAfter

	// Clean up
	SwapOnEvent(nil)
}

func TestRecordEvent_OnEventNotCalledWhenNil(t *testing.T) {
	SetOnEvent(nil)
	// Must not panic and must record the event anyway.
	RecordEvent("safe_test", EventStop, 1, 0, "no handler")

	// Event should still be in the global buffer even without OnEvent handler.
	snapshot := GlobalEventBuffer.Snapshot(0)
	found := false
	for _, e := range snapshot {
		if e.Name == "safe_test" && e.Type == EventStop {
			found = true
			break
		}
	}
	if !found {
		t.Error("event should be recorded in GlobalEventBuffer even when OnEvent is nil")
	}
}
