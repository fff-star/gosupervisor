package eventlistener

import (
	"sync"
	"sync/atomic"

	"gosupervisor/internal/config"
	"gosupervisor/internal/process"
)

// EventListenerManager manages all event listeners.
type EventListenerManager struct {
	mu          sync.RWMutex
	startStopMu sync.Mutex // serializes Start/Stop to prevent concurrent start+stop races
	listeners   map[string]*EventListener
	serial      int64

	logf func(format string, args ...interface{})
}

// NewManager creates a new EventListenerManager from the given config.
func NewManager(cfg *config.Config, logf func(string, ...interface{})) *EventListenerManager {
	m := &EventListenerManager{
		listeners: make(map[string]*EventListener),
		logf:      logf,
	}
	for name, elCfg := range cfg.EventListeners {
		l := newEventListener(elCfg)
		m.listeners[name] = l
	}
	return m
}

// Start launches all auto-start listeners.
func (m *EventListenerManager) Start() {
	m.startStopMu.Lock()
	defer m.startStopMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, l := range m.listeners {
		if l.Config.AutoStart {
			if err := l.start(); err != nil {
				m.logf("事件监听器 %s 启动失败: %v", name, err)
			}
		}
	}
}

// Stop terminates all running listeners.
func (m *EventListenerManager) Stop() {
	m.startStopMu.Lock()
	defer m.startStopMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, l := range m.listeners {
		l.stop()
	}
}

// EmitEvent dispatches an event to all subscribed listeners.
// It is safe to call from any goroutine.
func (m *EventListenerManager) EmitEvent(name string, typ process.EventType, pid int, exitCode int, message string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	event := process.Event{
		Name:     name,
		Type:     typ,
		PID:      pid,
		ExitCode: exitCode,
		Message:  message,
	}
	atomic.AddInt64(&m.serial, 1)

	for _, l := range m.listeners {
		if l.wantsEvent(typ) {
			l.queue.Push(event)
		}
	}
}

// Reload stops all current listeners and starts listeners from the new config.
// The swap is atomic with respect to EmitEvent so no events are lost.
func (m *EventListenerManager) Reload(cfg *config.Config) {
	// Build new listener map before taking the lock.
	newListeners := make(map[string]*EventListener)
	for name, elCfg := range cfg.EventListeners {
		l := newEventListener(elCfg)
		newListeners[name] = l
	}

	// Atomically swap in the new map. EmitEvent only needs mu.RLock,
	// so it will see either the old or new map — never an empty one.
	m.mu.Lock()
	oldListeners := m.listeners
	m.listeners = newListeners
	m.mu.Unlock()

	// Stop old listeners now that they are no longer reachable.
	for _, l := range oldListeners {
		l.stop()
	}

	// Start new listeners.
	m.mu.RLock()
	for name, l := range m.listeners {
		if l.Config.AutoStart {
			if err := l.start(); err != nil {
				m.logf("事件监听器 %s 启动失败: %v", name, err)
			}
		}
	}
	m.mu.RUnlock()
}
