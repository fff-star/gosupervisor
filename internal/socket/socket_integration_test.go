package socket

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// newIntegrationSocketServer creates a SocketServer with temp-dir-based logging
// and two test processes in group "web". Returns the server and the socket path.
func newIntegrationSocketServer(t *testing.T) (*SocketServer, string) {
	t.Helper()
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	pm := process.NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:         "test1",
		Command:      "sleep 60",
		Group:        "web",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		StopSignal:   "SIGTERM",
		StopSecs:     1,
		Environment:  make(map[string]string),
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:         "test2",
		Command:      "sleep 60",
		Group:        "web",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		StopSignal:   "SIGTERM",
		StopSecs:     1,
		Environment:  make(map[string]string),
	})
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	return NewSocketServer(pm), socketPath
}

// socketConn is a helper for sending a command and reading the response.
func socketConn(t *testing.T, socketPath, cmd string) string {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(cmd + "\n"))
	if err != nil {
		t.Fatalf("write command: %v", err)
	}

	buf := make([]byte, 65536)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		// EOF or timeout — return what we have
		return strings.TrimSpace(string(buf[:n]))
	}
	return strings.TrimSpace(string(buf[:n]))
}

func TestSocketRealConn_StatusAll(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	resp := socketConn(t, socketPath, "status")
	if !strings.Contains(resp, "test1") || !strings.Contains(resp, "test2") {
		t.Errorf("expected test1 and test2 in status, got: %s", resp)
	}
}

func TestSocketRealConn_StatusSingle(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	resp := socketConn(t, socketPath, "status test1")
	if !strings.HasPrefix(resp, "OK test1 ") {
		t.Errorf("expected OK test1 ..., got: %s", resp)
	}
}

func TestSocketRealConn_StartStop(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Start test1
	resp := socketConn(t, socketPath, "start test1")
	if !strings.Contains(resp, "OK started") {
		t.Errorf("expected OK started, got: %s", resp)
	}

	// Give the process time to start
	time.Sleep(200 * time.Millisecond)

	// Verify it's running via status
	resp = socketConn(t, socketPath, "status test1")
	if !strings.Contains(resp, string(process.StateRunning)) {
		t.Errorf("expected RUNNING state, got: %s", resp)
	}

	// Stop test1
	resp = socketConn(t, socketPath, "stop test1")
	if !strings.Contains(resp, "OK stopped") {
		t.Errorf("expected OK stopped, got: %s", resp)
	}
}

func TestSocketRealConn_StartMissingName(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	resp := socketConn(t, socketPath, "start")
	if !strings.HasPrefix(resp, "ERR missing process name") {
		t.Errorf("expected ERR missing process name, got: %s", resp)
	}
}

func TestSocketRealConn_InvalidCommand(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	resp := socketConn(t, socketPath, "nonexistentcommand")
	if !strings.HasPrefix(resp, "ERR unknown command") {
		t.Errorf("expected ERR unknown command, got: %s", resp)
	}
}

func TestSocketRealConn_GroupStartStop(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Group start
	resp := socketConn(t, socketPath, "group-start web")
	if !strings.Contains(resp, "OK started") {
		t.Errorf("expected OK started, got: %s", resp)
	}

	// Group stop
	resp = socketConn(t, socketPath, "group-stop web")
	if !strings.Contains(resp, "OK stopped") {
		t.Errorf("expected OK stopped, got: %s", resp)
	}
}

func TestSocketRealConn_Events(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Push some events first
	process.RecordEvent("test1", process.EventStart, 100, 0, "started")
	process.RecordEvent("test2", process.EventStop, 200, 0, "stopped")

	resp := socketConn(t, socketPath, "events")
	if !strings.Contains(resp, "test1") || !strings.Contains(resp, "test2") {
		t.Errorf("expected events for test1 and test2, got: %s", resp)
	}
}

func TestSocketRealConn_Help(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	resp := socketConn(t, socketPath, "help")
	if !strings.Contains(resp, "commands:") {
		t.Errorf("expected help output with 'commands:', got: %s", resp)
	}
	if !strings.Contains(resp, "start") || !strings.Contains(resp, "stop") {
		t.Errorf("expected start/stop in help, got: %s", resp)
	}
}

func TestSocketRealConn_MultiCommand(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}

	_, err = conn.Write([]byte("help\nstatus\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Give the server time to process both commands before we read
	time.Sleep(50 * time.Millisecond)

	// Now close our write side so the server's scanner gets EOF and exits handleConn
	// On Linux, we can use the underlying unix socket's CloseWrite
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}

	// Read all responses
	buf := make([]byte, 65536)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var allResp string
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			allResp += string(buf[:n])
		}
		if err != nil {
			break
		}
	}

	conn.Close()

	if !strings.Contains(allResp, "commands:") {
		t.Errorf("expected 'commands:' from help, got: %s", allResp)
	}
	if !strings.Contains(allResp, "test1") || !strings.Contains(allResp, "test2") {
		t.Errorf("expected test1/test2 from status, got: %s", allResp)
	}
}

func TestSocketRealConn_EmptyLineIgnored(t *testing.T) {
	s, socketPath := newIntegrationSocketServer(t)
	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	// Send an empty line then a valid command
	_, err = conn.Write([]byte("\nstatus\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 65536)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	resp := string(buf[:n])

	// Should get the status response (empty line ignored, not treated as error)
	if !strings.Contains(resp, "test1") {
		t.Errorf("expected status response after empty line, got: %s", resp)
	}
}
