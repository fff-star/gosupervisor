package fcgi

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFCGI_Lifecycle_StartStop(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "app.sock")

	sm := NewSocketManager("unix://"+sockPath, 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer sm.Close()

	cmd := exec.Command("sleep", "5")
	cleanup, err := sm.Attach(cmd)
	if err != nil {
		t.Fatalf("Attach() failed: %v", err)
	}
	defer cleanup()

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed: %v", err)
	}
	if cmd.Process.Pid <= 0 {
		t.Fatal("expected positive PID")
	}

	if sm.RefCount() != 1 {
		t.Errorf("expected RefCount=1 after Attach+Start, got %d", sm.RefCount())
	}

	// Kill the process
	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Kill error (may already be dead): %v", err)
	}
	cmd.Wait()

	last := sm.Detach()
	if !last {
		t.Error("expected Detach() to return true (last reference)")
	}
	if sm.RefCount() != 0 {
		t.Errorf("expected RefCount=0, got %d", sm.RefCount())
	}
}

func TestFCGI_SocketSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "app.sock")

	sm := NewSocketManager("unix://"+sockPath, 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer sm.Close()

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket file should exist after Listen()")
	}

	// First child
	cmd1 := exec.Command("sleep", "2")
	cleanup1, err := sm.Attach(cmd1)
	if err != nil {
		t.Fatalf("Attach() failed: %v", err)
	}
	cmd1.Start()
	if sm.RefCount() != 1 {
		t.Errorf("expected RefCount=1, got %d", sm.RefCount())
	}
	cmd1.Process.Kill()
	cmd1.Wait()
	cleanup1()
	sm.Detach()

	time.Sleep(50 * time.Millisecond)

	// Second child — socket should still be usable
	cmd2 := exec.Command("sleep", "2")
	cleanup2, err := sm.Attach(cmd2)
	if err != nil {
		t.Fatalf("Attach() to second child failed: %v", err)
	}
	cmd2.Start()
	if sm.RefCount() != 1 {
		t.Errorf("expected RefCount=1 after second attach, got %d", sm.RefCount())
	}
	cmd2.Process.Kill()
	cmd2.Wait()
	cleanup2()
	sm.Detach()
}

func TestFCGI_SocketClosesOnLastExit(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "app.sock")

	sm := NewSocketManager("unix://"+sockPath, 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	cmd := exec.Command("sleep", "2")
	cleanup, err := sm.Attach(cmd)
	if err != nil {
		t.Fatalf("Attach() failed: %v", err)
	}
	cmd.Start()
	cmd.Process.Kill()
	cmd.Wait()
	cleanup()
	sm.Detach()

	if err := sm.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after last Close()")
	}
}
