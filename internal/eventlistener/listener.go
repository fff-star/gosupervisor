package eventlistener

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/process"
)

// eventQueue is a bounded, blocking queue with drop-oldest-on-full semantics.
type eventQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []process.Event
	cap    int
	closed bool
}

func newEventQueue(capacity int) *eventQueue {
	if capacity < 1 {
		capacity = 1
	}
	q := &eventQueue{
		events: make([]process.Event, 0, capacity),
		cap:    capacity,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push adds an event to the queue. If the queue is full, the oldest event is dropped.
func (q *eventQueue) Push(e process.Event) {
	q.mu.Lock()
	if len(q.events) >= q.cap {
		// Drop oldest
		q.events = append(q.events[1:], e)
	} else {
		q.events = append(q.events, e)
	}
	q.mu.Unlock()
	q.cond.Signal()
}

// Pop blocks until an event is available and returns the first event (without removing it).
// Returns false if the queue has been closed.
func (q *eventQueue) Pop() (process.Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.events) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed && len(q.events) == 0 {
		return process.Event{}, false
	}
	return q.events[0], true
}

// Close unblocks all waiters and prevents further Pop operations from blocking.
func (q *eventQueue) Close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

// RemoveFirst removes the first event from the queue.
func (q *eventQueue) RemoveFirst() {
	q.mu.Lock()
	if len(q.events) > 0 {
		q.events = q.events[1:]
	}
	q.mu.Unlock()
}

// Len returns the current queue length.
func (q *eventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events)
}

// EventListener manages a single event listener child process.
type EventListener struct {
	Name   string
	Config *config.EventListenerConfig

	mu          sync.Mutex
	state       string
	cmd         *exec.Cmd
	stdoutW     io.WriteCloser // write events to child's stdin
	stdinR      io.ReadCloser  // read READY/RESULT from child's stdout
	queue       *eventQueue
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	startDone   chan struct{} // closed when start() completes
	poolSerial  int64
	eventMask   map[process.EventType]bool
}

// newEventListener creates a new EventListener.
func newEventListener(cfg *config.EventListenerConfig) *EventListener {
	l := &EventListener{
		Name:  cfg.Name,
		Config: cfg,
		queue: newEventQueue(cfg.BufferSize),
		done:  make(chan struct{}),
	}
	l.buildEventMask()
	return l
}

func (l *EventListener) buildEventMask() {
	l.eventMask = make(map[process.EventType]bool)
	for _, eventName := range l.Config.Events {
		eventName = strings.TrimSpace(eventName)
		if types, ok := supervisordEventDerives[eventName]; ok {
			for _, typ := range types {
				l.eventMask[typ] = true
			}
		}
	}
}

// wantsEvent returns true if this listener is subscribed to the given event type.
func (l *EventListener) wantsEvent(typ process.EventType) bool {
	return l.eventMask[typ]
}

// start launches the child process and begins the protocol loop.
func (l *EventListener) start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == "RUNNING" || l.state == "STARTING" {
		return nil
	}
	l.state = "STARTING"
	l.startDone = make(chan struct{})

	l.ctx, l.cancel = context.WithCancel(context.Background())
	l.done = make(chan struct{})

	cmd := exec.CommandContext(l.ctx, "/bin/sh", "-c", l.Config.Command)
	if l.Config.Directory != "" {
		cmd.Dir = l.Config.Directory
	}
	if l.Config.User != "" {
		if err := setupUser(cmd, l.Config.User); err != nil {
			l.state = "STOPPED"
			close(l.startDone)
			return fmt.Errorf("setup user: %w", err)
		}
	}
	cmd.Env = os.Environ()
	for k, v := range l.Config.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Wire pipes: stdoutR reads child's stdout (READY/RESULT),
	// stdinW writes events to child's stdin.
	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		l.state = "STOPPED"
		close(l.startDone)
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stdinW, err := cmd.StdinPipe()
	if err != nil {
		stdoutR.Close()
		l.state = "STOPPED"
		close(l.startDone)
		return fmt.Errorf("stdin pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdoutR.Close()
		stdinW.Close()
		l.state = "STOPPED"
		close(l.startDone)
		return fmt.Errorf("start child process: %w", err)
	}

	l.cmd = cmd
	l.stdinR = stdoutR
	l.stdoutW = stdinW
	l.state = "RUNNING"
	close(l.startDone)

	go l.protocolLoop(stdoutR, stdinW)
	return nil
}

// stop terminates the child process and waits for the protocol loop to exit.
func (l *EventListener) stop() {
	l.mu.Lock()
	// If start is still in progress, wait for it to complete.
	if l.state == "STARTING" {
		startDone := l.startDone
		l.mu.Unlock()
		if startDone != nil {
			<-startDone
		}
		l.mu.Lock()
	}
	if l.state != "RUNNING" {
		l.mu.Unlock()
		return
	}
	l.state = "STOPPING"
	cmd := l.cmd
	done := l.done
	stdinR := l.stdinR
	cancel := l.cancel
	l.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Send graceful stop signal first
		sig := parseStopSignal(l.Config.StopSignal)
		_ = cmd.Process.Signal(sig)
		// Wait with timeout then force kill.
		// Use a channel to avoid calling Kill() after Wait() has already
		// reaped the child, which could target a reused PID.
		waitDone := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(waitDone)
		}()
		timer := time.NewTimer(time.Duration(l.Config.StopSecs) * time.Second)
		select {
		case <-waitDone:
			timer.Stop()
		case <-timer.C:
			_ = cmd.Process.Kill()
			<-waitDone
		}
	}

	// Cancel context (sends SIGKILL via CommandContext if process still alive),
	// close queue to unblock protocolLoop if it's waiting on Pop,
	// then close pipes.
	if cancel != nil {
		cancel()
	}
	l.queue.Close()
	if stdinR != nil {
		_ = stdinR.Close()
	}

	if done != nil {
		<-done
	}

	l.mu.Lock()
	l.state = "STOPPED"
	l.mu.Unlock()
}

func parseStopSignal(name string) syscall.Signal {
	switch strings.ToUpper(name) {
	case "SIGTERM":
		return syscall.SIGTERM
	case "SIGKILL":
		return syscall.SIGKILL
	case "SIGINT":
		return syscall.SIGINT
	case "SIGHUP":
		return syscall.SIGHUP
	case "SIGQUIT":
		return syscall.SIGQUIT
	case "SIGUSR1":
		return syscall.SIGUSR1
	case "SIGUSR2":
		return syscall.SIGUSR2
	default:
		return syscall.SIGTERM
	}
}

// protocolLoop implements the supervisord READY/RESULT protocol.
// It reads from child's stdout (stdoutR) and writes events to child's stdin (stdinW).
//
// Protocol flow:
//  1. Read READY from child
//  2. Dequeue event, encode and write to child's stdin
//  3. Read READY (ack from child that it received the event)
//  4. Read RESULT N\n<payload> from child
//  5. If payload == "OK": remove event from queue. If "FAIL": retry next iteration.
//  6. Go to 2
func (l *EventListener) protocolLoop(stdoutR io.Reader, stdinW io.WriteCloser) {
	defer close(l.done)
	defer stdinW.Close()

	reader := bufio.NewReader(stdoutR)
	var eventSent bool

	for {
		// Step 1: Wait for READY
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line != "READY" {
			continue
		}

		// If we previously sent an event, read the result now
		if eventSent {
			eventSent = false
			payload, err := readResult(reader)
			if err != nil {
				return
			}
			if payload == "OK" {
				l.queue.RemoveFirst()
			}
			// FAIL: leave event in queue, will be retried
			continue
		}

		// Step 2: Get next event and send it
		currentEvent, ok := l.queue.Pop() // blocks until event available
		if !ok {
			return // queue closed, shutting down
		}

		serial := atomic.AddInt64(&l.poolSerial, 1)
		encoded := encodeEvent(currentEvent, "gosupervisor", 0, serial, l.Name)
		if _, err := stdinW.Write(encoded); err != nil {
			return
		}
		eventSent = true
	}
}

// readResult reads a RESULT line and its payload from the reader.
// Format: "RESULT N\n<N bytes>"
func readResult(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read result line: %w", err)
	}
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 || parts[0] != "RESULT" {
		return "", fmt.Errorf("invalid result line: %q", line)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 0 {
		return "", fmt.Errorf("invalid result length: %q", parts[1])
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", fmt.Errorf("read result payload: %w", err)
	}
	return string(payload), nil
}

func setupUser(cmd *exec.Cmd, username string) error {
	if username == "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		return nil
	}
	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", username, err)
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)
	gidStrs, _ := u.GroupIds()
	var groups []uint32
	for _, gs := range gidStrs {
		g, _ := strconv.ParseUint(gs, 10, 32)
		groups = append(groups, uint32(g))
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:         uint32(uid),
			Gid:         uint32(gid),
			Groups:      groups,
			NoSetGroups: false,
		},
	}
	return nil
}
