package eventlistener

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/process"
)

func TestEventWireName(t *testing.T) {
	tests := []struct {
		typ      process.EventType
		expected string
	}{
		{process.EventStart, "PROCESS_STATE_STARTING"},
		{process.EventStop, "PROCESS_STATE_STOPPED"},
		{process.EventExit, "PROCESS_STATE_EXITED"},
		{process.EventFatal, "PROCESS_STATE_FATAL"},
		{process.EventHealthFail, "PROCESS_STATE_FATAL"},
		{process.EventHealthRestore, "PROCESS_STATE_RUNNING"},
		{process.EventSignal, "PROCESS_STATE"},
	}
	for _, tt := range tests {
		result := eventWireName(tt.typ)
		if result != tt.expected {
			t.Errorf("eventWireName(%s) = %q, want %q", tt.typ, result, tt.expected)
		}
	}
}

func TestEventWireName_Unknown(t *testing.T) {
	result := eventWireName(process.EventType("nonexistent"))
	if result != "UNKNOWN" {
		t.Errorf("expected UNKNOWN, got %q", result)
	}
}

func TestEncodeEvent(t *testing.T) {
	event := process.Event{
		Name:    "testproc",
		Type:    process.EventStart,
		PID:     42,
		Message: "started",
	}
	encoded := encodeEvent(event, "gosupervisor", 1, 5, "mylistener")
	s := string(encoded)

	if !strings.Contains(s, "ver:3.0") {
		t.Errorf("expected ver:3.0 in encoded event, got: %s", s)
	}
	if !strings.Contains(s, "server:gosupervisor") {
		t.Errorf("expected server:gosupervisor in encoded event, got: %s", s)
	}
	if !strings.Contains(s, "pool:mylistener") {
		t.Errorf("expected pool:mylistener in encoded event, got: %s", s)
	}
	if !strings.Contains(s, "eventname:PROCESS_STATE_STARTING") {
		t.Errorf("expected eventname:PROCESS_STATE_STARTING in encoded event, got: %s", s)
	}
	if !strings.Contains(s, "processname:testproc") {
		t.Errorf("expected processname:testproc in body, got: %s", s)
	}
	if !strings.Contains(s, "pid:42") {
		t.Errorf("expected pid:42 in body, got: %s", s)
	}
}

func TestSupervisordEventDerives(t *testing.T) {
	// PROCESS_STATE should include all lifecycle events
	types, ok := supervisordEventDerives["PROCESS_STATE"]
	if !ok {
		t.Fatal("PROCESS_STATE not in derives map")
	}
	expectedTypes := []process.EventType{
		process.EventStart, process.EventStop, process.EventExit,
		process.EventFatal, process.EventHealthFail, process.EventHealthRestore,
	}
	for _, exp := range expectedTypes {
		found := false
		for _, typ := range types {
			if typ == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("PROCESS_STATE should include %s", exp)
		}
	}
}

func TestWantsEvent(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:   "test",
		Events: []string{"PROCESS_STATE"},
	}
	l := newEventListener(cfg)

	if !l.wantsEvent(process.EventStart) {
		t.Error("PROCESS_STATE listener should want EventStart")
	}
	if !l.wantsEvent(process.EventExit) {
		t.Error("PROCESS_STATE listener should want EventExit")
	}

	// Not in PROCESS_STATE
	if l.wantsEvent(process.EventSignal) {
		t.Error("PROCESS_STATE listener should not want EventSignal")
	}
}

func TestWantsEvent_SpecificType(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:   "test",
		Events: []string{"PROCESS_STATE_STARTING"},
	}
	l := newEventListener(cfg)

	if !l.wantsEvent(process.EventStart) {
		t.Error("PROCESS_STATE_STARTING listener should want EventStart")
	}
	if l.wantsEvent(process.EventExit) {
		t.Error("PROCESS_STATE_STARTING listener should not want EventExit")
	}
}

func TestWantsEvent_EmptyEvents(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:   "test",
		Events: []string{},
	}
	l := newEventListener(cfg)

	if l.wantsEvent(process.EventStart) {
		t.Error("empty events listener should not want any event")
	}
}

func TestEventQueue_FIFO(t *testing.T) {
	q := newEventQueue(10)

	e1 := process.Event{Name: "p1", Type: process.EventStart, PID: 1}
	e2 := process.Event{Name: "p2", Type: process.EventExit, PID: 2}
	e3 := process.Event{Name: "p3", Type: process.EventStop, PID: 3}

	q.Push(e1)
	q.Push(e2)
	q.Push(e3)

	if q.Len() != 3 {
		t.Fatalf("expected 3 events, got %d", q.Len())
	}

	first, ok := q.Pop()
	if !ok || first.Name != "p1" {
		t.Errorf("expected p1, got %s (ok=%v)", first.Name, ok)
	}
	q.RemoveFirst()

	second, ok := q.Pop()
	if !ok || second.Name != "p2" {
		t.Errorf("expected p2, got %s (ok=%v)", second.Name, ok)
	}
	q.RemoveFirst()

	third, ok := q.Pop()
	if !ok || third.Name != "p3" {
		t.Errorf("expected p3, got %s", third.Name)
	}
	q.RemoveFirst()

	if q.Len() != 0 {
		t.Errorf("expected empty queue, got %d", q.Len())
	}
}

func TestEventQueue_Overflow(t *testing.T) {
	q := newEventQueue(3)

	for i := 0; i < 5; i++ {
		q.Push(process.Event{Name: "p", PID: i})
	}

	if q.Len() != 3 {
		t.Fatalf("expected 3 events after overflow, got %d", q.Len())
	}

	// Oldest should be dropped (PID 0 and 1), first remaining is PID 2
	first, ok := q.Pop()
	if !ok || first.PID != 2 {
		t.Errorf("expected PID 2 after overflow, got %d", first.PID)
	}
}

func TestEventQueue_ClampCapacity(t *testing.T) {
	q := newEventQueue(0)
	q.Push(process.Event{PID: 1})
	if q.Len() != 1 {
		t.Errorf("expected capacity clamped to 1, got len %d", q.Len())
	}
}

func TestProtocolLoop_READYSendRESULTOK(t *testing.T) {
	// Simulate child process via pipes
	childStdoutR, childStdoutW := io.Pipe()
	childStdinR, childStdinW := io.Pipe()

	cfg := &config.EventListenerConfig{
		Name:   "test",
		Events: []string{"PROCESS_STATE"},
	}
	l := newEventListener(cfg)

	// Push an event
	l.queue.Push(process.Event{Name: "proc1", Type: process.EventStart, PID: 10})

	// Goroutine simulating the child listener process
	go func() {
		// 1. READY
		childStdoutW.Write([]byte("READY\n"))
		// 2. Read event from stdin
		buf := make([]byte, 4096)
		_, _ = childStdinR.Read(buf)
		// 3. READY (ack)
		childStdoutW.Write([]byte("READY\n"))
		// 4. RESULT OK
		childStdoutW.Write([]byte("RESULT 2\nOK"))
		// Close to signal protocol loop to exit
		childStdoutW.Close()
	}()

	l.protocolLoop(childStdoutR, childStdinW)

	// After OK result, the event should be consumed
	if l.queue.Len() != 0 {
		t.Errorf("expected empty queue after OK, got %d", l.queue.Len())
	}
}

func TestProtocolLoop_RESULTFAIL(t *testing.T) {
	childStdoutR, childStdoutW := io.Pipe()
	childStdinR, childStdinW := io.Pipe()

	cfg := &config.EventListenerConfig{
		Name:   "test",
		Events: []string{"PROCESS_STATE"},
	}
	l := newEventListener(cfg)

	l.queue.Push(process.Event{Name: "proc1", Type: process.EventStart, PID: 10})

	go func() {
		// First attempt: FAIL
		childStdoutW.Write([]byte("READY\n"))
		buf := make([]byte, 4096)
		_, _ = childStdinR.Read(buf)
		childStdoutW.Write([]byte("READY\n"))
		childStdoutW.Write([]byte("RESULT 4\nFAIL"))

		// Second attempt: OK
		childStdoutW.Write([]byte("READY\n"))
		_, _ = childStdinR.Read(buf)
		childStdoutW.Write([]byte("READY\n"))
		childStdoutW.Write([]byte("RESULT 2\nOK"))

		childStdoutW.Close()
	}()

	l.protocolLoop(childStdoutR, childStdinW)

	if l.queue.Len() != 0 {
		t.Errorf("expected empty queue after retry+OK, got %d", l.queue.Len())
	}
}

func TestParseStopSignal(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"SIGTERM", 15},
		{"SIGKILL", 9},
		{"SIGINT", 2},
		{"SIGHUP", 1},
		{"BOGUS", 15}, // defaults to SIGTERM
	}
	for _, tt := range tests {
		sig := parseStopSignal(tt.name)
		if int(sig) != tt.expected {
			t.Errorf("parseStopSignal(%q) = %d, want %d", tt.name, int(sig), tt.expected)
		}
	}
}

func TestNewManager(t *testing.T) {
	cfg := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"mylistener": {
				Name:       "mylistener",
				Command:    "echo ready",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 50,
				AutoStart:  false,
			},
		},
	}
	m := NewManager(cfg, nil)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if len(m.listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(m.listeners))
	}
	l, ok := m.listeners["mylistener"]
	if !ok {
		t.Fatal("listener 'mylistener' not found")
	}
	if l.Config.BufferSize != 50 {
		t.Errorf("expected BufferSize 50, got %d", l.Config.BufferSize)
	}
}

func TestEmitEvent(t *testing.T) {
	cfg := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"l1": {
				Name:       "l1",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
			},
			"l2": {
				Name:       "l2",
				Events:     []string{"PROCESS_STATE_STARTING"},
				BufferSize: 10,
			},
		},
	}
	m := NewManager(cfg, nil)

	// Emit EventStart: both l1 and l2 should receive it
	m.EmitEvent("proc1", process.EventStart, 1, 0, "started")

	if m.listeners["l1"].queue.Len() != 1 {
		t.Errorf("l1 should have 1 event, got %d", m.listeners["l1"].queue.Len())
	}
	if m.listeners["l2"].queue.Len() != 1 {
		t.Errorf("l2 should have 1 event, got %d", m.listeners["l2"].queue.Len())
	}

	// Emit EventExit: only l1 should receive it (PROCESS_STATE includes exit)
	m.EmitEvent("proc1", process.EventExit, 1, 1, "exited")

	if m.listeners["l1"].queue.Len() != 2 {
		t.Errorf("l1 should have 2 events, got %d", m.listeners["l1"].queue.Len())
	}
	if m.listeners["l2"].queue.Len() != 1 {
		t.Errorf("l2 should still have 1 event, got %d", m.listeners["l2"].queue.Len())
	}
}

func TestReadResult_Valid(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("RESULT 2\nOK"))
	payload, err := readResult(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload != "OK" {
		t.Errorf("expected OK, got %q", payload)
	}
}

func TestReadResult_Invalid(t *testing.T) {
	tests := []string{
		"NOTRESULT 2\nOK",
		"RESULT\n",
		"RESULT -1\n",
	}
	for _, input := range tests {
		reader := bufio.NewReader(strings.NewReader(input))
		_, err := readResult(reader)
		if err == nil {
			t.Errorf("expected error for input %q", input)
		}
	}
}

func TestStart_RunningStateSkips(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:    "test",
		Command: "sleep 999",
	}
	l := newEventListener(cfg)
	l.mu.Lock()
	l.state = "RUNNING"
	l.mu.Unlock()

	err := l.start()
	if err != nil {
		t.Errorf("start() on RUNNING listener should return nil, got: %v", err)
	}
}

func TestStart_StartingStateSkips(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:    "test",
		Command: "sleep 999",
	}
	l := newEventListener(cfg)
	l.mu.Lock()
	l.state = "STARTING"
	l.mu.Unlock()

	err := l.start()
	if err != nil {
		t.Errorf("start() on STARTING listener should return nil, got: %v", err)
	}
}

func TestStart_Success(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:       "test",
		Command:    "sleep 10",
		BufferSize: 10,
	}
	l := newEventListener(cfg)

	err := l.start()
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}
	defer l.stop()

	l.mu.Lock()
	state := l.state
	l.mu.Unlock()
	if state != "RUNNING" {
		t.Errorf("expected RUNNING state, got %q", state)
	}
	if l.cmd == nil {
		t.Error("expected cmd to be non-nil")
	}
	if l.stdinR == nil {
		t.Error("expected stdinR to be non-nil (child stdout)")
	}
	if l.stdoutW == nil {
		t.Error("expected stdoutW to be non-nil (child stdin)")
	}
	if l.ctx == nil {
		t.Error("expected ctx to be non-nil")
	}
	if l.done == nil {
		t.Error("expected done channel to be non-nil")
	}
}

func TestStart_CmdStartFailure(t *testing.T) {
	// start() wraps commands with /bin/sh -c, so /bin/sh starts
	// successfully even when the wrapped command doesn't exist.
	cfg := &config.EventListenerConfig{
		Name:    "test",
		Command: "/nonexistent/binary/that/cannot/be/found",
	}
	l := newEventListener(cfg)

	err := l.start()
	if err != nil {
		t.Fatalf("start() should not fail (shell wraps command): %v", err)
	}
	defer l.stop()

	l.mu.Lock()
	state := l.state
	l.mu.Unlock()
	if state != "RUNNING" {
		t.Errorf("expected RUNNING state after start(), got %q", state)
	}
}

func TestStart_Directory(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.EventListenerConfig{
		Name:      "test",
		Command:   "pwd",
		Directory: dir,
	}
	l := newEventListener(cfg)

	err := l.start()
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}
	defer l.stop()

	if l.cmd.Dir != dir {
		t.Errorf("expected Dir=%q, got %q", dir, l.cmd.Dir)
	}
}

func TestStop_NotRunning(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:    "test",
		Command: "sleep 1",
	}
	l := newEventListener(cfg)
	l.mu.Lock()
	l.state = "STOPPED"
	l.mu.Unlock()

	// Must not panic
	l.stop()

	l.mu.Lock()
	if l.state != "STOPPED" {
		t.Errorf("expected STOPPED, got %q", l.state)
	}
	l.mu.Unlock()
}

func TestStop_WhileStarting(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:       "test",
		Command:    "sleep 10",
		BufferSize: 10,
	}
	l := newEventListener(cfg)
	l.mu.Lock()
	l.state = "STARTING"
	l.startDone = make(chan struct{})
	// nil out done since no protocolLoop runs to close it
	l.done = nil
	l.mu.Unlock()

	// Release start after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		l.mu.Lock()
		l.state = "RUNNING"
		close(l.startDone)
		l.mu.Unlock()
	}()

	// stop() should wait for startDone, then proceed
	l.stop()

	l.mu.Lock()
	if l.state != "STOPPED" {
		t.Errorf("expected STOPPED after stop-during-start, got %q", l.state)
	}
	l.mu.Unlock()
}

func TestStop_RunningProcess(t *testing.T) {
	cfg := &config.EventListenerConfig{
		Name:       "test",
		Command:    "sleep 60",
		BufferSize: 10,
		StopSecs:   1,
	}
	l := newEventListener(cfg)

	if err := l.start(); err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		l.stop()
		close(done)
	}()

	select {
	case <-done:
		// Expected: stop returned
	case <-time.After(5 * time.Second):
		t.Fatal("stop() timed out")
	}

	l.mu.Lock()
	if l.state != "STOPPED" {
		t.Errorf("expected STOPPED, got %q", l.state)
	}
	l.mu.Unlock()
}

func TestEventQueue_Close_UnblocksPop(t *testing.T) {
	q := newEventQueue(10)

	done := make(chan struct{})
	go func() {
		_, ok := q.Pop()
		if ok {
			t.Error("Pop() should return false after Close()")
		}
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	q.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Pop() did not unblock after Close()")
	}
}

func TestEventQueue_Close_WithRemaining(t *testing.T) {
	q := newEventQueue(10)
	q.Push(process.Event{Name: "e1"})
	q.Push(process.Event{Name: "e2"})

	q.Close()

	// Events pushed before close should still be retrievable
	e1, ok1 := q.Pop()
	if !ok1 || e1.Name != "e1" {
		t.Errorf("expected e1, got %v (ok=%v)", e1, ok1)
	}
	q.RemoveFirst()

	e2, ok2 := q.Pop()
	if !ok2 || e2.Name != "e2" {
		t.Errorf("expected e2, got %v (ok=%v)", e2, ok2)
	}
	q.RemoveFirst()

	// After draining, Pop should return false
	_, ok3 := q.Pop()
	if ok3 {
		t.Error("Pop() should return false after draining closed queue")
	}
}

func TestManager_Start_StartsAutoStartListeners(t *testing.T) {
	cfg := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"auto": {
				Name:       "auto",
				Command:    "sleep 10",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
				AutoStart:  true,
			},
			"manual": {
				Name:       "manual",
				Command:    "sleep 10",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
				AutoStart:  false,
			},
		},
	}
	m := NewManager(cfg, nil)
	m.Start()
	defer m.Stop()

	// Auto-start listener should be RUNNING
	if l, ok := m.listeners["auto"]; ok {
		l.mu.Lock()
		if l.state != "RUNNING" {
			t.Errorf("auto listener: expected RUNNING, got %q", l.state)
		}
		l.mu.Unlock()
	} else {
		t.Fatal("auto listener not found")
	}

	// Manual listener should not be started yet
	if l, ok := m.listeners["manual"]; ok {
		l.mu.Lock()
		state := l.state
		l.mu.Unlock()
		if state != "" && state != "STOPPED" {
			t.Errorf("manual listener: expected initial/STOPPED state, got %q", state)
		}
	}
}

func TestManager_Start_BadCommandRunsViaShell(t *testing.T) {
	// start() wraps commands with /bin/sh -c, so even a nonexistent binary
	// starts /bin/sh successfully (the child exits immediately but start()
	// returns nil). The listener transitions to RUNNING before the child exits.
	cfg := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"bad": {
				Name:      "bad",
				Command:   "/nonexistent/binary/xyz",
				Events:    []string{"PROCESS_STATE"},
				AutoStart: true,
			},
		},
	}
	m := NewManager(cfg, nil)
	m.Start()
	defer m.Stop()

	if l, ok := m.listeners["bad"]; ok {
		l.mu.Lock()
		state := l.state
		l.mu.Unlock()
		if state != "RUNNING" {
			t.Errorf("bad command listener: expected RUNNING (shell wraps), got %q", state)
		}
	} else {
		t.Fatal("bad listener not found")
	}
}

func TestManager_Stop(t *testing.T) {
	cfg := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"l1": {
				Name:       "l1",
				Command:    "sleep 10",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
				AutoStart:  true,
			},
			"l2": {
				Name:       "l2",
				Command:    "sleep 10",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
				AutoStart:  true,
			},
		},
	}
	m := NewManager(cfg, nil)
	m.Start()
	m.Stop()

	for name, l := range m.listeners {
		l.mu.Lock()
		if l.state != "STOPPED" {
			t.Errorf("listener %s: expected STOPPED after Manager.Stop(), got %q", name, l.state)
		}
		l.mu.Unlock()
	}
}

func TestManager_Reload(t *testing.T) {
	cfg1 := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"old": {
				Name:       "old",
				Command:    "sleep 10",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
				AutoStart:  true,
			},
		},
	}
	m := NewManager(cfg1, nil)
	m.Start()
	defer m.Stop()

	cfg2 := &config.Config{
		EventListeners: map[string]*config.EventListenerConfig{
			"new": {
				Name:       "new",
				Command:    "sleep 10",
				Events:     []string{"PROCESS_STATE"},
				BufferSize: 10,
				AutoStart:  true,
			},
		},
	}
	m.Reload(cfg2)

	if _, ok := m.listeners["old"]; ok {
		t.Error("old listener should be removed after reload")
	}
	if l, ok := m.listeners["new"]; ok {
		l.mu.Lock()
		if l.state != "RUNNING" {
			t.Errorf("new listener: expected RUNNING after reload, got %q", l.state)
		}
		l.mu.Unlock()
	} else {
		t.Fatal("new listener not found after reload")
	}
}
