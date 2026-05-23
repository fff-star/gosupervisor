package socket

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

func newTestSocketServer(t *testing.T) *SocketServer {
	t.Helper()
	logManager, err := logger.NewDefaultLogger(t.TempDir())
	if err != nil {
		t.Fatalf("创建 logger 失败: %v", err)
	}
	pm := process.NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:      "test1",
		Command:   "sleep 60",
		Group:     "web",
		AutoStart: true,
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:      "test2",
		Command:   "sleep 60",
		Group:     "web",
		AutoStart: true,
	})
	return NewSocketServer(pm)
}

func cleanupSocket(path string) {
	os.Remove(path)
}

func TestNewSocketServer(t *testing.T) {
	logManager, err := logger.NewDefaultLogger(t.TempDir())
	if err != nil {
		t.Fatalf("创建 logger 失败: %v", err)
	}
	pm := process.NewProcessManager(logManager)
	s := NewSocketServer(pm)
	if s == nil {
		t.Fatal("NewSocketServer 返回 nil")
	}
	if s.pm != pm {
		t.Error("pm 字段未正确设置")
	}
}

func TestHandleCommandStatusAll(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("status")
	if !strings.HasPrefix(resp, "OK 2 processes") {
		t.Errorf("status 应返回进程列表, 实际: %s", resp)
	}
	if !strings.Contains(resp, "test1") || !strings.Contains(resp, "test2") {
		t.Errorf("status 应包含 test1 和 test2: %s", resp)
	}
}

func TestHandleCommandStatusSingle(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("status test1")
	if !strings.HasPrefix(resp, "OK test1 ") {
		t.Errorf("status test1 应返回进程详情, 实际: %s", resp)
	}
}

func TestHandleCommandStatusNotFound(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("status nonexistent")
	if resp != "ERR process not found" {
		t.Errorf("不存在的进程应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandStart(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("start test1")
	if resp != "OK started" {
		t.Errorf("start test1 应成功, 实际: %s", resp)
	}
}

func TestHandleCommandStartMissingName(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("start")
	if resp != "ERR missing process name" {
		t.Errorf("缺少进程名应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandStartNotFound(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("start nonexistent")
	if resp != "ERR process not found" {
		t.Errorf("不存在的进程应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandStop(t *testing.T) {
	s := newTestSocketServer(t)
	s.handleCommand("start test1")
	resp := s.handleCommand("stop test1")
	if !strings.HasPrefix(resp, "OK") {
		t.Errorf("stop test1 应成功, 实际: %s", resp)
	}
}

func TestHandleCommandStopMissingName(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("stop")
	if resp != "ERR missing process name" {
		t.Errorf("缺少进程名应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandStopNotFound(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("stop nonexistent")
	if resp != "ERR process not found" {
		t.Errorf("不存在的进程应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandRestart(t *testing.T) {
	s := newTestSocketServer(t)
	s.handleCommand("start test1")
	resp := s.handleCommand("restart test1")
	if resp != "OK restarted" {
		t.Errorf("restart test1 应成功, 实际: %s", resp)
	}
}

func TestHandleCommandRestartMissingName(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("restart")
	if resp != "ERR missing process name" {
		t.Errorf("缺少进程名应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandRestartNotFound(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("restart nonexistent")
	if resp != "ERR process not found" {
		t.Errorf("不存在的进程应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandGroupStart(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-start web")
	if !strings.HasPrefix(resp, "OK started 2:") {
		t.Errorf("group-start web 应启动2个进程, 实际: %s", resp)
	}
	if !strings.Contains(resp, "test1") || !strings.Contains(resp, "test2") {
		t.Errorf("应包含 test1 和 test2: %s", resp)
	}
}

func TestHandleCommandGroupStartMissingName(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-start")
	if resp != "ERR missing group name" {
		t.Errorf("缺少组名应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandGroupStartEmptyGroup(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-start nonexistent")
	if !strings.HasPrefix(resp, "OK started 0:") {
		t.Errorf("空组应返回0, 实际: %s", resp)
	}
}

func TestHandleCommandGroupStop(t *testing.T) {
	s := newTestSocketServer(t)
	s.handleCommand("group-start web")
	resp := s.handleCommand("group-stop web")
	if !strings.HasPrefix(resp, "OK stopped") {
		t.Errorf("group-stop web 应成功, 实际: %s", resp)
	}
}

func TestHandleCommandGroupStopMissingName(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-stop")
	if resp != "ERR missing group name" {
		t.Errorf("缺少组名应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandGroupRestart(t *testing.T) {
	s := newTestSocketServer(t)
	s.handleCommand("group-start web")
	resp := s.handleCommand("group-restart web")
	if !strings.HasPrefix(resp, "OK restarted") {
		t.Errorf("group-restart web 应成功, 实际: %s", resp)
	}
}

func TestHandleCommandGroupRestartMissingName(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-restart")
	if resp != "ERR missing group name" {
		t.Errorf("缺少组名应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandHelp(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("help")
	if !strings.HasPrefix(resp, "OK commands:") {
		t.Errorf("help 应返回命令列表, 实际: %s", resp)
	}
	for _, kw := range []string{"status", "start", "stop", "restart", "group-start", "group-stop", "group-restart", "help", "quit"} {
		if !strings.Contains(resp, kw) {
			t.Errorf("help 应包含 %s, 实际: %s", kw, resp)
		}
	}
}

func TestHandleCommandUnknown(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("unknown_cmd")
	if resp != "ERR unknown command: unknown_cmd" {
		t.Errorf("未知命令应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandEmpty(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("")
	if resp != "ERR empty command" {
		t.Errorf("空命令应返回错误, 实际: %s", resp)
	}
}

func TestHandleCommandStatusWithRunningProcess(t *testing.T) {
	s := newTestSocketServer(t)
	s.handleCommand("start test1")
	resp := s.handleCommand("status test1")
	if !strings.Contains(resp, "test1") {
		t.Errorf("status 应包含进程名: %s", resp)
	}
}

func TestHandleCommandStartDuplicate(t *testing.T) {
	s := newTestSocketServer(t)
	s.handleCommand("start test1")
	// Second start should return error since process is already started
	resp := s.handleCommand("start test1")
	if !strings.HasPrefix(resp, "ERR") {
		t.Errorf("重复 start 应返回错误, 实际: %s", resp)
	}
}

func TestStartStopSocket(t *testing.T) {
	s := newTestSocketServer(t)
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cleanupSocket(socketPath)

	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	if s.socket == nil {
		t.Fatal("socket listener 为 nil")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		s.Stop()
		t.Fatalf("连接 socket 失败: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("status\n"))
	if err != nil {
		s.Stop()
		t.Fatalf("写入命令失败: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		s.Stop()
		t.Fatalf("读取响应失败: %v", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "test1") {
		t.Errorf("socket 响应应包含进程信息: %s", resp)
	}

	if err := s.Stop(); err != nil {
		t.Errorf("Stop 失败: %v", err)
	}
	cleanupSocket(socketPath)
}

func TestHandleConnWithQuit(t *testing.T) {
	s := newTestSocketServer(t)
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cleanupSocket(socketPath)

	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer s.Stop()
	defer cleanupSocket(socketPath)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("连接 socket 失败: %v", err)
	}

	_, err = conn.Write([]byte("quit\n"))
	if err != nil {
		t.Fatalf("写入命令失败: %v", err)
	}

	// quit should close the connection
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)
	conn.Close()
}

func TestHandleConnWithExit(t *testing.T) {
	s := newTestSocketServer(t)
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cleanupSocket(socketPath)

	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer s.Stop()
	defer cleanupSocket(socketPath)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("连接 socket 失败: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("exit\n"))
	if err != nil {
		t.Fatalf("写入命令失败: %v", err)
	}

	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)
}

func TestHandleConnHelpQuit(t *testing.T) {
	s := newTestSocketServer(t)
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cleanupSocket(socketPath)

	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer s.Stop()
	defer cleanupSocket(socketPath)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("连接 socket 失败: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("help\nquit\n"))
	if err != nil {
		t.Fatalf("写入命令失败: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	resp := string(buf[:n])
	if !strings.Contains(resp, "commands:") {
		t.Errorf("help 应返回命令列表: %s", resp)
	}
}

func TestStartReplacesExistingSocket(t *testing.T) {
	s := newTestSocketServer(t)
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cleanupSocket(socketPath)

	// Create a socket file and close it, leaving the file on disk
	old, err := net.Listen("unix", socketPath)
	if err == nil {
		old.Close()
	}

	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start 应覆盖已存在的 socket 文件: %v", err)
	}
	s.Stop()
	cleanupSocket(socketPath)
}

func TestStopNoSocket(t *testing.T) {
	s := newTestSocketServer(t)
	if err := s.Stop(); err != nil {
		t.Errorf("Stop 在无 socket 时应返回 nil: %v", err)
	}
}

func TestHandleCommandExtraWhitespace(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("  status    test1  ")
	if !strings.HasPrefix(resp, "OK test1 ") {
		t.Errorf("多余空格应被正确处理, 实际: %s", resp)
	}
}

func TestSocketStartInvalidPath(t *testing.T) {
	s := newTestSocketServer(t)
	err := s.Start("/nonexistent_dir_xyz/socket.sock")
	if err == nil {
		t.Error("无效路径应返回错误")
	}
}

func TestHandleCommandGroupStopEmptyGroup(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-stop nonexistent")
	if !strings.HasPrefix(resp, "OK stopped 0:") {
		t.Errorf("空组 stop 应返回0, 实际: %s", resp)
	}
}

func TestHandleCommandGroupRestartEmptyGroup(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("group-restart nonexistent")
	if !strings.HasPrefix(resp, "OK restarted 0:") {
		t.Errorf("空组 restart 应返回0, 实际: %s", resp)
	}
}


// --- Fuzz tests ---

func FuzzHandleCommand(f *testing.F) {
	f.Add("status")
	f.Add("status test1")
	f.Add("start test1")
	f.Add("stop test1")
	f.Add("restart test1")
	f.Add("group-start web")
	f.Add("group-stop web")
	f.Add("help")
	f.Add("quit")
	f.Add("  status  test1  ")

	f.Fuzz(func(t *testing.T, data string) {
		logManager, err := logger.NewDefaultLogger(t.TempDir())
		if err != nil {
			t.Skipf("logger: %v", err)
		}
		pm := process.NewProcessManager(logManager)
		pm.AddProcess(&config.ProgramConfig{
			Name: "test1", Command: "true",
			Group: "web", AutoStart: false, AutoRestart: false,
			StartSecs: 0, StartRetries: 0,
		})
		pm.AddProcess(&config.ProgramConfig{
			Name: "test2", Command: "true",
			Group: "web", AutoStart: false, AutoRestart: false,
			StartSecs: 0, StartRetries: 0,
		})
		s := NewSocketServer(pm)

		resp := s.handleCommand(data)
		if resp == "" {
			t.Errorf("handleCommand 返回空响应, 输入: %q", data)
		}
		parts := strings.Fields(data)
		if len(parts) > 0 {
			switch parts[0] {
			case "status", "start", "stop", "restart", "group-start", "group-stop", "group-restart", "help":
			default:
				if !strings.HasPrefix(resp, "ERR") {
					t.Errorf("未知命令应返回 ERR, 实际: %q", resp)
				}
			}
		}
	})
}

func TestHandleCommandSignal(t *testing.T) {
	s := newTestSocketServer(t)

	// Start a process first so there's something to signal
	p := s.pm.GetProcess("test1")
	p.Start()
	defer p.Stop()

	resp := s.handleCommand("signal test1 SIGHUP")
	if !strings.HasPrefix(resp, "OK signal") {
		t.Errorf("signal should succeed, got: %s", resp)
	}
}

func TestHandleCommandSignalInvalidSignal(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("signal test1 INVALID")
	if !strings.HasPrefix(resp, "ERR unknown signal") {
		t.Errorf("expected ERR unknown signal, got: %s", resp)
	}
}

func TestHandleCommandSignalMissingArgs(t *testing.T) {
	s := newTestSocketServer(t)
	resp := s.handleCommand("signal test1")
	if !strings.HasPrefix(resp, "ERR usage") {
		t.Errorf("expected ERR usage, got: %s", resp)
	}
}

func TestHandleCommandEvents(t *testing.T) {
	s := newTestSocketServer(t)

	process.RecordEvent("test1", process.EventStart, 1, 0, "test")
	process.RecordEvent("test1", process.EventStop, 1, 0, "test")

	resp := s.handleCommand("events")
	if !strings.HasPrefix(resp, "OK") {
		t.Errorf("events should return OK, got: %s", resp)
	}
	if !strings.Contains(resp, "start") {
		t.Error("events should contain 'start' event")
	}
}

func TestHandleCommandEventsWithLimit(t *testing.T) {
	s := newTestSocketServer(t)

	for i := 0; i < 10; i++ {
		process.RecordEvent("test1", process.EventStart, 1, 0, "test")
	}

	resp := s.handleCommand("events 3")
	if !strings.HasPrefix(resp, "OK 3 events") {
		t.Errorf("expected OK 3 events, got: %s", resp)
	}
}

func TestSetSocketMode_Applied(t *testing.T) {
	s := newTestSocketServer(t)
	s.SetSocketMode(0700)
	if s.socketMode != 0700 {
		t.Errorf("expected socketMode=0700, got %o", s.socketMode)
	}
}

func TestSetSocketOwner_Applied(t *testing.T) {
	s := newTestSocketServer(t)
	s.SetSocketOwner("1000:1000")
	if s.socketOwner != "1000:1000" {
		t.Errorf("expected socketOwner='1000:1000', got %q", s.socketOwner)
	}
}

func TestApplySocketOwner_Invalid(t *testing.T) {
	s := newTestSocketServer(t)
	s.socketOwner = "invalid_format_no_colon"
	err := s.applySocketOwner("/tmp/fake.sock")
	if err == nil {
		t.Error("expected error for invalid socketOwner format")
	}
	if !strings.Contains(err.Error(), "格式无效") {
		t.Errorf("expected format error message, got: %v", err)
	}
}

func TestApplySocketOwner_UIDGID(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root to chown files")
	}
	s := newTestSocketServer(t)
	tmpFile := filepath.Join(t.TempDir(), "test.sock")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	uid := os.Getuid()
	gid := os.Getgid()
	s.socketOwner = fmt.Sprintf("%d:%d", uid, gid)
	err = s.applySocketOwner(tmpFile)
	if err != nil {
		t.Errorf("applySocketOwner with valid uid:gid should succeed: %v", err)
	}
}

func TestStart_WithSocketMode(t *testing.T) {
	s := newTestSocketServer(t)
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cleanupSocket(socketPath)

	s.SetSocketMode(0700)

	if err := s.Start(socketPath); err != nil {
		t.Fatalf("Start with socket mode failed: %v", err)
	}
	defer s.Stop()
	defer cleanupSocket(socketPath)

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	mode := info.Mode()
	if mode&os.ModeSocket == 0 {
		t.Error("file is not a socket")
	}
}
