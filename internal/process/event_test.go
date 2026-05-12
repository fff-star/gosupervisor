package process

import (
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
