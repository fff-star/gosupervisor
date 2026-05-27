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

var initSSEBrokerOnce sync.Once

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
	frame := append([]byte("data: "), data...)
	frame = append(frame, '\n', '\n')

	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- frame:
		default:
		}
	}
}

// sseCallback returns the SSE broadcast callback. Factored as a method so the
// broker reference is captured once rather than reading the global on each call.
func sseCallback() func(name string, typ process.EventType, pid int, exitCode int, message string, ts time.Time) {
	return func(name string, typ process.EventType, pid int, exitCode int, message string, ts time.Time) {
		globalSSEBroker.broadcast(process.Event{
			Timestamp: ts,
			Name:      name,
			Type:      typ,
			PID:       pid,
			ExitCode:  exitCode,
			Message:   message,
		})
	}
}

// InitSSEBroker wires the SSE broker into its own independent callback slot.
// Subsequent calls are no-ops. The SSE slot is independent from the event-listener
// slot, so config reloads that replace event listeners no longer need to re-wire SSE.
func InitSSEBroker() {
	initSSEBrokerOnce.Do(func() {
		process.SetOnEventSSE(sseCallback())
	})
}
