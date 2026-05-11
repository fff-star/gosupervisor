package socket

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"gosupervisor/internal/process"
)

type SocketServer struct {
	pm     *process.ProcessManager
	socket net.Listener
}

func NewSocketServer(pm *process.ProcessManager) *SocketServer {
	return &SocketServer{pm: pm}
}

func (s *SocketServer) Start(socketPath string) error {
	os.Remove(socketPath)
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("创建 Unix socket 失败: %v", err)
	}
	s.socket = l
	fmt.Printf("Unix socket CLI 启动在 %s\n", socketPath)

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go s.handleConn(conn)
		}
	}()
	return nil
}

func (s *SocketServer) Stop() error {
	if s.socket != nil {
		return s.socket.Close()
	}
	return nil
}

func (s *SocketServer) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "quit" || line == "exit" {
			return
		}
		response := s.handleCommand(line)
		fmt.Fprintln(conn, response)
	}
}

func (s *SocketServer) handleCommand(line string) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "ERR empty command"
	}
	cmd := parts[0]
	var name string
	if len(parts) > 1 {
		name = parts[1]
	}

	switch cmd {
	case "status":
		if name != "" {
			p := s.pm.GetProcess(name)
			if p == nil {
				return "ERR process not found"
			}
			snap := p.Snapshot()
			return fmt.Sprintf("OK %s %s pid=%d exit=%d restarts=%d cpu=%.2f mem=%d",
				snap.Name, snap.State, snap.PID, snap.ExitCode, snap.RestartCount, snap.CPUUsage, snap.MemoryUsage)
		}
		var lines []string
		s.pm.RangeProcesses(func(n string, p *process.Process) {
			snap := p.Snapshot()
			lines = append(lines, fmt.Sprintf("%s %s pid=%d restarts=%d",
				snap.Name, snap.State, snap.PID, snap.RestartCount))
		})
		return fmt.Sprintf("OK %d processes\n%s", len(lines), strings.Join(lines, "\n"))

	case "start":
		if name == "" {
			return "ERR missing process name"
		}
		p := s.pm.GetProcess(name)
		if p == nil {
			return "ERR process not found"
		}
		if err := p.Start(); err != nil {
			return fmt.Sprintf("ERR %v", err)
		}
		return "OK started"

	case "stop":
		if name == "" {
			return "ERR missing process name"
		}
		p := s.pm.GetProcess(name)
		if p == nil {
			return "ERR process not found"
		}
		if err := p.Stop(); err != nil {
			return fmt.Sprintf("ERR %v", err)
		}
		return "OK stopped"

	case "restart":
		if name == "" {
			return "ERR missing process name"
		}
		p := s.pm.GetProcess(name)
		if p == nil {
			return "ERR process not found"
		}
		if err := p.Restart(); err != nil {
			return fmt.Sprintf("ERR %v", err)
		}
		return "OK restarted"

	case "group-start":
		if name == "" {
			return "ERR missing group name"
		}
		started := s.pm.StartGroup(name)
		return fmt.Sprintf("OK started %d: %s", len(started), strings.Join(started, ", "))

	case "group-stop":
		if name == "" {
			return "ERR missing group name"
		}
		stopped := s.pm.StopGroup(name)
		return fmt.Sprintf("OK stopped %d: %s", len(stopped), strings.Join(stopped, ", "))

	case "group-restart":
		if name == "" {
			return "ERR missing group name"
		}
		restarted := s.pm.RestartGroup(name)
		return fmt.Sprintf("OK restarted %d: %s", len(restarted), strings.Join(restarted, ", "))

	case "help":
		return "OK commands: status [name], start <name>, stop <name>, restart <name>, group-start <group>, group-stop <group>, group-restart <group>, help, quit"

	default:
		return fmt.Sprintf("ERR unknown command: %s", cmd)
	}
}
