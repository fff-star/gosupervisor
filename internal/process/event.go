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
	mu       sync.Mutex
	events   []Event
	capacity int
	pos      int
	full     bool
}

// NewEventBuffer creates a new ring buffer with the given capacity.
func NewEventBuffer(capacity int) *EventBuffer {
	return &EventBuffer{
		events:   make([]Event, capacity),
		capacity: capacity,
	}
}

// Push adds an event to the buffer.
func (eb *EventBuffer) Push(e Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.events[eb.pos] = e
	eb.pos = (eb.pos + 1) % eb.capacity
	if eb.pos == 0 {
		eb.full = true
	}
}

// Snapshot returns all events in order (oldest first).
func (eb *EventBuffer) Snapshot(limit int) []Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	size := eb.pos
	if eb.full {
		size = eb.capacity
	}
	if limit > 0 && limit < size {
		// Return most recent `limit` events
		result := make([]Event, limit)
		for i := 0; i < limit; i++ {
			idx := (eb.pos - limit + i + eb.capacity) % eb.capacity
			result[i] = eb.events[idx]
		}
		return result
	}

	result := make([]Event, size)
	if eb.full {
		for i := 0; i < size; i++ {
			result[i] = eb.events[(eb.pos+i)%eb.capacity]
		}
	} else {
		copy(result, eb.events[:eb.pos])
	}
	return result
}

// Len returns the number of events currently in the buffer.
func (eb *EventBuffer) Len() int {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.full {
		return eb.capacity
	}
	return eb.pos
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
