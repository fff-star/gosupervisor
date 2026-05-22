package eventlistener

import (
	"sync"
	"sync/atomic"

	"gosupervisor/internal/config"
	"gosupervisor/internal/process"
)

// EventListenerManager manages all event listeners.
type EventListenerManager struct {
	mu        sync.RWMutex
	listeners map[string]*EventListener
	serial    int64

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
func (m *EventListenerManager) Reload(cfg *config.Config) {
	m.Stop()

	m.mu.Lock()
	m.listeners = make(map[string]*EventListener)
	for name, elCfg := range cfg.EventListeners {
		l := newEventListener(elCfg)
		m.listeners[name] = l
	}
	m.mu.Unlock()

	m.Start()
}
