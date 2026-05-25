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
// Safe to call multiple times; subsequent calls are no-ops.
func InitSSEBroker() {
	initSSEBrokerOnce.Do(func() {
		oldFn := process.SwapOnEvent(func(name string, typ process.EventType, pid int, exitCode int, message string) {
			globalSSEBroker.broadcast(process.Event{
				Timestamp: time.Now(),
				Name:      name,
				Type:      typ,
				PID:       pid,
				ExitCode:  exitCode,
				Message:   message,
			})
		})
		_ = oldFn
	})
}

// ResetSSEBroker re-binds the SSE broker. Used on config reload
// to re-establish the handler chain after the event listener manager is replaced.
func ResetSSEBroker() {
	sseFn := func(name string, typ process.EventType, pid int, exitCode int, message string) {
		globalSSEBroker.broadcast(process.Event{
			Timestamp: time.Now(),
			Name:      name,
			Type:      typ,
			PID:       pid,
			ExitCode:  exitCode,
			Message:   message,
		})
	}
	oldFn := process.SwapOnEvent(sseFn)
	if oldFn != nil {
		// Chain the old callback after SSE broadcast
		process.SetOnEvent(func(name string, typ process.EventType, pid int, exitCode int, message string) {
			sseFn(name, typ, pid, exitCode, message)
			oldFn(name, typ, pid, exitCode, message)
		})
	}
}
