package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// captureStdout runs fn and returns what was written to stdout.
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func makeLogger(t *testing.T) *logger.Logger {
	t.Helper()
	dir := t.TempDir()
	l, err := logger.NewDefaultLogger(dir)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func makeProcessManager(t *testing.T) *process.ProcessManager {
	t.Helper()
	l := makeLogger(t)
	return process.NewProcessManager(l)
}

func writeConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}

func TestPrintVersion(t *testing.T) {
	out := captureStdout(printVersion)
	if !strings.Contains(out, ProgramName) {
		t.Errorf("expected output to contain %q, got %q", ProgramName, out)
	}
	if !strings.Contains(out, Version) {
		t.Errorf("expected output to contain version %q, got %q", Version, out)
	}
}

func TestPrintProcessStatus(t *testing.T) {
	pm := makeProcessManager(t)

	cfg := &config.ProgramConfig{
		Name:    "test_proc",
		Command: "echo hello",
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("test_proc")
	if p == nil {
		t.Fatal("AddProcess did not create process")
	}

	out := captureStdout(func() { printProcessStatus(p) })

	if !strings.Contains(out, "test_proc") {
		t.Errorf("expected process name in output, got: %s", out)
	}
	if !strings.Contains(out, "STOPPED") {
		t.Errorf("expected STOPPED state, got: %s", out)
	}
}

func TestReloadConfiguration_NoChange(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo app\n")

	// Add existing process
	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo app",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	if pm.GetProcess("app") == nil {
		t.Error("process 'app' should still exist after no-change reload")
	}
}

func TestReloadConfiguration_AddProcess(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo app\n[program:worker]\ncommand=echo worker\n")

	// Add only one process
	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo app",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	if pm.GetProcess("worker") == nil {
		t.Error("process 'worker' should have been added")
	}
	if pm.GetProcess("app") == nil {
		t.Error("process 'app' should still exist")
	}
}

func TestReloadConfiguration_RemoveProcess(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo app\n")

	// Add two processes but config only has one
	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo app",
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:    "old",
		Command: "echo old",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	if pm.GetProcess("old") != nil {
		t.Error("process 'old' should have been removed")
	}
	if pm.GetProcess("app") == nil {
		t.Error("process 'app' should still exist")
	}
}

func TestReloadConfiguration_ModifyProcess(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo newcmd\n")

	// Add process with different command
	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo oldcmd",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	p := pm.GetProcess("app")
	if p == nil {
		t.Fatal("process 'app' should still exist")
	}
	s := p.Snapshot()
	if s.Config.Command != "echo newcmd" {
		t.Errorf("expected command 'echo newcmd', got %q", s.Config.Command)
	}
}

func TestReloadConfiguration_WithExpansion(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini",
		"[program:worker]\ncommand=echo worker\nprocess_name=%(program_name)s_%(process_num)02d\nnumprocs=3\n")

	pm.AddProcess(&config.ProgramConfig{
		Name:    "worker_01",
		Command: "echo worker",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	for _, name := range []string{"worker_01", "worker_02", "worker_03"} {
		if pm.GetProcess(name) == nil {
			t.Errorf("process %q should exist after reload", name)
		}
	}
}

func TestReloadConfiguration_InvalidPath(t *testing.T) {
	pm := makeProcessManager(t)
	l := makeLogger(t)

	err := reloadConfiguration(pm, "/nonexistent/path/config.ini", l)
	if err == nil {
		t.Error("expected error for nonexistent config file")
	}
}

func TestUpdateProcessConfig_Success(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo updated\n")

	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo original",
	})

	err := updateProcessConfig(pm, cfgPath, "app", l)
	if err != nil {
		t.Fatalf("updateProcessConfig failed: %v", err)
	}

	p := pm.GetProcess("app")
	if p == nil {
		t.Fatal("process 'app' should exist after update")
	}
	s := p.Snapshot()
	if s.Config.Command != "echo updated" {
		t.Errorf("expected command 'echo updated', got %q", s.Config.Command)
	}
}

func TestUpdateProcessConfig_NotFound(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	// Config has 'app' but we try to update 'nonexistent'
	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo app\n")

	err := updateProcessConfig(pm, cfgPath, "nonexistent", l)
	if err == nil {
		t.Error("expected error for nonexistent process")
	}
}

func TestUpdateProcessConfig_ProcessInConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	// Config has only 'other', we try to update 'missing'
	cfgPath := writeConfig(t, dir, "test.ini", "[program:other]\ncommand=echo other\n")

	pm.AddProcess(&config.ProgramConfig{
		Name:    "missing",
		Command: "echo x",
	})

	err := updateProcessConfig(pm, cfgPath, "missing", l)
	if err == nil {
		t.Error("expected error when process not found in config")
	}
}

func TestHandleSignals_SIGTERM(t *testing.T) {
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := "/nonexistent/config.ini"
	stateFile := ""

	sigChan := make(chan os.Signal, 1)
	done := make(chan struct{})

	go handleSignals(sigChan, done, pm, &cfgPath, l, &stateFile)

	sigChan <- syscall.SIGTERM

	// Wait for done signal or timeout
	select {
	case <-done:
		// Expected: handleSignals closed the done channel
	case <-time.After(2 * time.Second):
		t.Error("handleSignals did not close done channel on SIGTERM")
	}
}

func TestHandleSignals_SIGINT(t *testing.T) {
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := "/nonexistent/config.ini"
	stateFile := ""

	sigChan := make(chan os.Signal, 1)
	done := make(chan struct{})

	go handleSignals(sigChan, done, pm, &cfgPath, l, &stateFile)

	sigChan <- syscall.SIGINT

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handleSignals did not close done channel on SIGINT")
	}
}

func TestHandleSignals_SIGHUP(t *testing.T) {
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := "/nonexistent/config.ini"
	stateFile := ""

	sigChan := make(chan os.Signal, 1)
	done := make(chan struct{})

	go handleSignals(sigChan, done, pm, &cfgPath, l, &stateFile)

	// SIGHUP should not close done (it triggers reload, not shutdown)
	sigChan <- syscall.SIGHUP

	// Give it time to attempt reload
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM to clean up
	sigChan <- syscall.SIGTERM
	<-done
}

func TestReload_ChangedProcessRestarts(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo newcmd\n")

	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo oldcmd",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	p := pm.GetProcess("app")
	if p == nil {
		t.Fatal("process 'app' should exist after reload")
	}
	s := p.Snapshot()
	if s.Config.Command != "echo newcmd" {
		t.Errorf("expected command 'echo newcmd', got %q", s.Config.Command)
	}
}

func TestReload_NewProcessStarts(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini",
		"[program:app]\ncommand=echo app\nautostart=true\n"+
			"[program:worker]\ncommand=echo worker\nautostart=true\n")

	pm.AddProcess(&config.ProgramConfig{
		Name:      "app",
		Command:   "echo app",
		AutoStart: true,
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	if pm.GetProcess("worker") == nil {
		t.Error("new process 'worker' should be added")
	}
}

func TestReload_RemovedProcessStops(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := writeConfig(t, dir, "test.ini", "[program:app]\ncommand=echo app\n")

	pm.AddProcess(&config.ProgramConfig{
		Name:    "app",
		Command: "echo app",
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:    "removed",
		Command: "echo removed",
	})

	err := reloadConfiguration(pm, cfgPath, l)
	if err != nil {
		t.Fatalf("reloadConfiguration failed: %v", err)
	}

	if pm.GetProcess("removed") != nil {
		t.Error("process 'removed' should be gone after reload")
	}
}

func TestHandleSignals_SIGTERMWithStateFile(t *testing.T) {
	dir := t.TempDir()
	pm := makeProcessManager(t)
	l := makeLogger(t)

	cfgPath := "/nonexistent/config.ini"
	stateFile := filepath.Join(dir, "state.json")

	sigChan := make(chan os.Signal, 1)
	done := make(chan struct{})

	go handleSignals(sigChan, done, pm, &cfgPath, l, &stateFile)

	sigChan <- syscall.SIGTERM

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handleSignals did not close done channel on SIGTERM with state file")
	}
}
