package process

import (
	"time"
)

// EventType represents the type of a process event.
type EventType string

const (
	EventStart         EventType = "start"
	EventStop          EventType = "stop"
	EventExit          EventType = "exit"
	EventFatal         EventType = "fatal"
	EventHealthFail    EventType = "health_fail"
	EventHealthRestore EventType = "health_restore"
	EventSignal        EventType = "signal"
)

// Event represents a single process lifecycle event.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Name      string    `json:"name"`
	Type      EventType `json:"type"`
	PID       int       `json:"pid"`
	ExitCode  int       `json:"exitCode,omitempty"`
	Message   string    `json:"message,omitempty"`
}

// EventBuffer is a thread-safe ring buffer of events.
type EventBuffer struct {
	*RingBuffer[Event]
}

// NewEventBuffer creates a new ring buffer with the given capacity.
func NewEventBuffer(capacity int) *EventBuffer {
	return &EventBuffer{RingBuffer: NewRingBuffer[Event](capacity)}
}

// Snapshot returns the most recent `limit` events in chronological order.
func (eb *EventBuffer) Snapshot(limit int) []Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if limit <= 0 {
		return eb.snapshotAll()
	}
	return eb.snapshotLast(limit)
}

// GlobalEventBuffer is the singleton event buffer for the supervisor.
var GlobalEventBuffer = NewEventBuffer(500)

// RecordEvent pushes an event to the global buffer.
func RecordEvent(name string, typ EventType, pid int, exitCode int, message string) {
	GlobalEventBuffer.Push(Event{
		Timestamp: time.Now(),
		Name:      name,
		Type:      typ,
		PID:       pid,
		ExitCode:  exitCode,
		Message:   message,
	})
}
