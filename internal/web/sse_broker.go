package web

import (
	"encoding/json"
	"sync"
	"time"

	"gosupervisor/internal/process"
)

// sseBroker broadcasts process events to all connected SSE clients.
type sseBroker struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
}

var globalSSEBroker = &sseBroker{
	clients: make(map[chan []byte]bool),
}

// subscribe registers a new SSE client and returns its event channel.
func (b *sseBroker) subscribe() chan []byte {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch
}

// unsubscribe removes a client channel.
func (b *sseBroker) unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

// broadcast sends an event to all connected clients (non-blocking).
func (b *sseBroker) broadcast(event process.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	// Build SSE frame once
	frame := append([]byte("data: "), data...)
	frame = append(frame, '\n', '\n')

	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- frame:
		default:
			// Client too slow, drop
		}
	}
}

// InitSSEBroker wires the global SSE broker to process.OnEvent.
// Called from main.go after OnEvent is set.
func InitSSEBroker() {
	// Atomically clear and capture the previous handler chain.
	// oldFn is an ordinary local — immutable after this assignment,
	// so the closure below captures it without any data race.
	oldFn := process.SwapOnEvent(nil)

	process.SetOnEvent(func(name string, typ process.EventType, pid int, exitCode int, message string) {
		if oldFn != nil {
			oldFn(name, typ, pid, exitCode, message)
		}
		globalSSEBroker.broadcast(process.Event{
			Timestamp: time.Now(),
			Name:      name,
			Type:      typ,
			PID:       pid,
			ExitCode:  exitCode,
			Message:   message,
		})
	})
}
