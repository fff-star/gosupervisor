package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// mockServer writes a response then closes the pipe so the client scanner exits.
// net.Pipe doesn't support CloseWrite, so we close the whole server side.
func mockServer(t *testing.T, server net.Conn, expectCmd string, response ...string) {
	t.Helper()
	scanner := bufio.NewScanner(server)
	if scanner.Scan() {
		cmd := scanner.Text()
		if cmd != expectCmd {
			t.Errorf("expected command %q, got %q", expectCmd, cmd)
		}
	}
	for _, line := range response {
		fmt.Fprintln(server, line)
	}
	server.Close()
}

func TestHandleCommand_Status(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "status", "OK 1 process", "")
	handleCommand(client, []string{"status"})
}

func TestHandleCommand_Start(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "start testproc", "OK")
	handleCommand(client, []string{"start", "testproc"})
}

func TestHandleCommand_Stop(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "stop testproc", "OK")
	handleCommand(client, []string{"stop", "testproc"})
}

func TestHandleCommand_Restart(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "restart testproc", "OK")
	handleCommand(client, []string{"restart", "testproc"})
}

func TestHandleCommand_Signal(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "signal testproc SIGHUP", "OK")
	handleCommand(client, []string{"signal", "testproc", "SIGHUP"})
}

func TestHandleCommand_Reload(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "reload testproc", "OK")
	handleCommand(client, []string{"reload", "testproc"})
}

func TestHandleCommand_Events(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "events", "OK 0 events")
	handleCommand(client, []string{"events"})
}

func TestHandleCommand_EventsWithLimit(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "events 10", "OK 0 events")
	handleCommand(client, []string{"events", "10"})
}

func TestHandleCommand_GroupStart(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "group-start mygroup", "OK")
	handleCommand(client, []string{"group-start", "mygroup"})
}

func TestHandleCommand_GroupStop(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "group-stop mygroup", "OK")
	handleCommand(client, []string{"group-stop", "mygroup"})
}

func TestHandleCommand_GroupRestart(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "group-restart mygroup", "OK")
	handleCommand(client, []string{"group-restart", "mygroup"})
}

func TestHandleCommand_Help(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go mockServer(t, server, "help", "OK commands: start stop restart")
	handleCommand(client, []string{"help"})
}

func TestHandleCommand_Unknown(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"nonexistent"}) })
	if !strings.Contains(stderr, "未知命令") {
		t.Errorf("expected stderr to contain '未知命令', got %q", stderr)
	}
}

func TestHandleCommand_StartNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"start"}) })
	if !strings.Contains(stderr, "需要进程名") {
		t.Errorf("expected stderr to contain '需要进程名', got %q", stderr)
	}
}

func TestHandleCommand_StopNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"stop"}) })
	if !strings.Contains(stderr, "需要进程名") {
		t.Errorf("expected stderr to contain '需要进程名', got %q", stderr)
	}
}

func TestHandleCommand_RestartNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"restart"}) })
	if !strings.Contains(stderr, "需要进程名") {
		t.Errorf("expected stderr to contain '需要进程名', got %q", stderr)
	}
}

func TestHandleCommand_SignalNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"signal"}) })
	if !strings.Contains(stderr, "需要进程名和信号名") {
		t.Errorf("expected stderr to contain '需要进程名和信号名', got %q", stderr)
	}
}

func TestHandleCommand_ReloadNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"reload"}) })
	if !strings.Contains(stderr, "需要进程名") {
		t.Errorf("expected stderr to contain '需要进程名', got %q", stderr)
	}
}

func TestHandleCommand_GroupStartNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"group-start"}) })
	if !strings.Contains(stderr, "需要组名") {
		t.Errorf("expected stderr to contain '需要组名', got %q", stderr)
	}
}

func TestHandleCommand_GroupStopNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"group-stop"}) })
	if !strings.Contains(stderr, "需要组名") {
		t.Errorf("expected stderr to contain '需要组名', got %q", stderr)
	}
}

func TestHandleCommand_GroupRestartNoArgs(t *testing.T) {
	_, server := net.Pipe()
	defer server.Close()
	client, _ := net.Pipe()
	defer client.Close()
	stderr := captureStderr(func() { handleCommand(client, []string{"group-restart"}) })
	if !strings.Contains(stderr, "需要组名") {
		t.Errorf("expected stderr to contain '需要组名', got %q", stderr)
	}
}

func TestBuildCompleterStatic_Commands(t *testing.T) {
	completer := buildCompleterStatic(nil)

	result := completer("st")
	if len(result) < 3 {
		t.Errorf("expected at least 3 completions for 'st', got %d: %v", len(result), result)
	}
	expected := []string{"start", "status", "stop"}
	for _, exp := range expected {
		found := false
		for _, r := range result {
			if r == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in completions for 'st', got %v", exp, result)
		}
	}
}

func TestBuildCompleterStatic_ExactMatch(t *testing.T) {
	completer := buildCompleterStatic(nil)

	result := completer("quit")
	if len(result) != 1 || result[0] != "quit" {
		t.Errorf("expected ['quit'], got %v", result)
	}
}

func TestBuildCompleterStatic_NoMatch(t *testing.T) {
	completer := buildCompleterStatic(nil)

	result := completer("xyz")
	if len(result) != 0 {
		t.Errorf("expected no completions for 'xyz', got %v", result)
	}
}

func TestBuildCompleterStatic_SignalCompletion(t *testing.T) {
	completer := buildCompleterStatic([]string{"proc1", "proc2"})

	result := completer("signal proc1 SIG")
	if len(result) < 1 {
		t.Errorf("expected signal name completions, got %v", result)
	}
	for _, r := range result {
		if !strings.HasPrefix(r, "SIG") {
			t.Errorf("expected signal name to start with SIG, got %q", r)
		}
	}
}

func TestBuildCompleterStatic_ProcessNameCompletion(t *testing.T) {
	completer := buildCompleterStatic([]string{"webapp", "worker", "db"})

	result := completer("start wo")
	if len(result) != 1 || result[0] != "worker" {
		t.Errorf("expected ['worker'], got %v", result)
	}
}

func TestBuildCompleterStatic_SingleWord(t *testing.T) {
	completer := buildCompleterStatic(nil)

	result := completer("sta")
	found := false
	for _, r := range result {
		if r == "start" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'start' in command completions, got %v", result)
	}
}

func TestFetchProcessNames(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		scanner := bufio.NewScanner(server)
		if scanner.Scan() {
			fmt.Fprintln(server, "OK 2 processes")
			fmt.Fprintln(server, "proc1 RUNNING pid=1 restarts=0")
			fmt.Fprintln(server, "proc2 STOPPED pid=0 restarts=0")
		}
		server.Close()
	}()

	names := fetchProcessNames(client)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}
	if names[0] != "proc1" {
		t.Errorf("expected 'proc1', got %q", names[0])
	}
	if names[1] != "proc2" {
		t.Errorf("expected 'proc2', got %q", names[1])
	}
}

func TestFetchProcessNames_EmptyResponse(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		scanner := bufio.NewScanner(server)
		if scanner.Scan() {
			fmt.Fprintln(server, "OK 0 processes")
		}
		server.Close()
	}()

	names := fetchProcessNames(client)
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %v", names)
	}
}

func TestFetchProcessNames_NoResponse(t *testing.T) {
	client, server := net.Pipe()
	server.Close()
	client.Close()

	names := fetchProcessNames(client)
	if len(names) != 0 {
		t.Errorf("expected 0 names on closed connection, got %v", names)
	}
}

func TestHistoryPath(t *testing.T) {
	path := historyPath()
	if !strings.Contains(path, ".gosupervisorctl_history") {
		t.Errorf("expected path to contain '.gosupervisorctl_history', got %q", path)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
	home, _ := os.UserHomeDir()
	if home != "" && !strings.HasPrefix(path, home) {
		t.Errorf("expected path under home dir %q, got %q", home, path)
	}
}

func TestPrintREPLHelp(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printREPLHelp()
	w.Close()
	os.Stdout = old
	var buf strings.Builder
	b := make([]byte, 1024)
	for {
		n, _ := r.Read(b)
		if n == 0 {
			break
		}
		buf.Write(b[:n])
	}
	out := buf.String()

	expectedCommands := []string{"status", "start", "stop", "restart", "signal",
		"reload", "events", "group-start", "group-stop", "group-restart", "help", "quit"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(out, cmd) {
			t.Errorf("REPL help missing command %q", cmd)
		}
	}
}

func TestPrintUsage(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printUsage()
	w.Close()
	os.Stdout = old
	var buf strings.Builder
	b := make([]byte, 1024)
	for {
		n, _ := r.Read(b)
		if n == 0 {
			break
		}
		buf.Write(b[:n])
	}
	out := buf.String()

	if !strings.Contains(out, ProgramName) {
		t.Errorf("usage should contain program name %q", ProgramName)
	}
	if !strings.Contains(out, "socket") {
		t.Errorf("usage should mention -socket flag")
	}
}

func TestBuildCompleterStatic_EmptyInput(t *testing.T) {
	completer := buildCompleterStatic(nil)

	result := completer("")
	if len(result) != 0 {
		t.Errorf("expected no completions for empty input, got %v", result)
	}
}

func TestBuildCompleterStatic_CaseInsensitive(t *testing.T) {
	completer := buildCompleterStatic(nil)

	result := completer("ST")
	if len(result) < 1 {
		t.Errorf("expected case-insensitive completions for 'ST', got %v", result)
	}
}

func TestBuildCompleterStatic_SignalThirdArg(t *testing.T) {
	completer := buildCompleterStatic([]string{"proc1"})

	result := completer("signal proc1 SIGT")
	if len(result) != 1 || result[0] != "SIGTERM" {
		t.Errorf("expected ['SIGTERM'], got %v", result)
	}
}
