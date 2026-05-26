package process

import (
	"sync"
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

// Two independent callback slots avoid the nesting problem that arises from
// chaining callbacks inside a single slot. SSE broadcast and event listeners
// each get their own slot; RecordEvent fans out to both.
var (
	onEventSSE func(name string, typ EventType, pid int, exitCode int, message string)
	onEventEL  func(name string, typ EventType, pid int, exitCode int, message string)
	onEventMu  sync.Mutex
)

// SetOnEventSSE sets the SSE broadcast callback (called from web package).
func SetOnEventSSE(fn func(name string, typ EventType, pid int, exitCode int, message string)) {
	onEventMu.Lock()
	onEventSSE = fn
	onEventMu.Unlock()
}

// SetOnEventEL sets the event listener callback (called from main wiring and reload).
func SetOnEventEL(fn func(name string, typ EventType, pid int, exitCode int, message string)) {
	onEventMu.Lock()
	onEventEL = fn
	onEventMu.Unlock()
}

// RecordEvent pushes an event to the global buffer and fans out to both
// SSE and event listener callbacks independently.
func RecordEvent(name string, typ EventType, pid int, exitCode int, message string) {
	GlobalEventBuffer.Push(Event{
		Timestamp: time.Now(),
		Name:      name,
		Type:      typ,
		PID:       pid,
		ExitCode:  exitCode,
		Message:   message,
	})
	onEventMu.Lock()
	sseFn := onEventSSE
	elFn := onEventEL
	onEventMu.Unlock()
	if sseFn != nil {
		sseFn(name, typ, pid, exitCode, message)
	}
	if elFn != nil {
		elFn(name, typ, pid, exitCode, message)
	}
}
