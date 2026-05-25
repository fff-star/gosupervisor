package process

import (
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

// ---------------------------------------------------------------------------
// handleExitedProcess tests
// ---------------------------------------------------------------------------

func TestHandleExitedProcess_AutoRestartOff(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: false, AutoStart: false,
	})
	p.mu.Lock()
	p.State = StateExited
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	// Should stay EXITED (not FATAL, not STARTING)
	if p.GetState() != StateExited {
		t.Errorf("expected EXITED, got %s", p.GetState())
	}
}

func TestHandleExitedProcess_NotExited(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
	})
	// State is STOPPED (not EXITED) — should skip
	p.mu.Lock()
	p.State = StateStopped
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	// Should stay STOPPED
	if p.GetState() != StateStopped {
		t.Errorf("expected STOPPED, got %s", p.GetState())
	}
}

func TestHandleExitedProcess_ExitCodeBlocked(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
		NoRestartCodes: []int{1},
	})
	p.mu.Lock()
	p.State = StateExited
	p.ExitCode = 1
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	state := p.GetState()
	if state != StateFatal {
		t.Errorf("expected FATAL, got %s", state)
	}
}

func TestHandleExitedProcess_ExitCodeNotAllowed(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
		RestartCodes: []int{0},
	})
	p.mu.Lock()
	p.State = StateExited
	p.ExitCode = 1
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	state := p.GetState()
	if state != StateFatal {
		t.Errorf("expected FATAL, got %s", state)
	}
}

func TestHandleExitedProcess_RateLimitExceeded(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
		RestartMaxCount: 2, RestartWindowSecs: 60,
	})
	p.mu.Lock()
	p.State = StateExited
	now := time.Now()
	p.restartTimestamps = []time.Time{now, now}
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	state := p.GetState()
	if state != StateFatal {
		t.Errorf("expected FATAL, got %s", state)
	}
}

func TestHandleExitedProcess_RateLimitNotExceeded(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
		RestartMaxCount: 2, RestartWindowSecs: 60,
		StartRetries: 99,
	})
	p.mu.Lock()
	p.State = StateExited
	now := time.Now()
	p.restartTimestamps = []time.Time{now.Add(-10 * time.Second)} // only 1 restart, within window
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	state := p.GetState()
	if state != StateStarting {
		t.Errorf("expected STARTING, got %s", state)
	}

	// Prevent the spawned goroutine from actually starting the process
	p.mu.Lock()
	p.State = StateStopped
	p.mu.Unlock()
}

func TestHandleExitedProcess_MaxRetries(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
		StartRetries: 3,
	})
	p.mu.Lock()
	p.State = StateExited
	p.StartRetries = 4 // > Config.StartRetries(3)
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	state := p.GetState()
	if state != StateFatal {
		t.Errorf("expected FATAL, got %s", state)
	}
}

func TestHandleExitedProcess_NormalRestart(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoRestart: true, AutoStart: false,
		StartRetries: 5,
	})
	p.mu.Lock()
	p.State = StateExited
	p.StartRetries = 0
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkProcess(p)

	state := p.GetState()
	if state != StateStarting {
		t.Errorf("expected STARTING, got %s", state)
	}

	// Prevent the spawned goroutine from actually starting the process
	p.mu.Lock()
	p.State = StateStopped
	p.mu.Unlock()
}

// ---------------------------------------------------------------------------
// checkRunningProcess tests
// ---------------------------------------------------------------------------

func TestCheckRunningProcess_StartSecsReset(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoStart: false,
		StartSecs: 1,
	})
	p.mu.Lock()
	p.State = StateRunning
	p.StartTime = time.Now().Add(-2 * time.Second) // running longer than StartSecs
	p.StartRetries = 5
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkRunningProcess(p)

	p.mu.Lock()
	retries := p.StartRetries
	p.mu.Unlock()

	if retries != 0 {
		t.Errorf("expected StartRetries=0 after running past StartSecs, got %d", retries)
	}
}

func TestCheckRunningProcess_Stopping(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "test", Command: "sleep 1", AutoStart: false,
	})
	p.mu.Lock()
	p.State = StateStopping
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkRunningProcess(p)

	// Should stay STOPPING
	if p.GetState() != StateStopping {
		t.Errorf("expected STOPPING, got %s", p.GetState())
	}
}

func TestCheckRunningProcess_HealthCheckRestart(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "hc_restart", Command: "sleep 60", AutoStart: false,
		HealthCheckRestart: true, HealthCheckURL: "http://127.0.0.1:1",
		HealthCheckInterval: 10,
		StartSecs: 10,
		StopSecs:  1,
	})

	// Start the process first so it is actually RUNNING
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if p.GetState() != StateRunning {
		t.Fatalf("expected RUNNING, got %s", p.GetState())
	}

	p.mu.Lock()
	p.Healthy = false
	p.healthCheckRestartFired = false
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkRunningProcess(p)

	// Wait for Restart() → Stop() → Start() to complete.
	time.Sleep(2 * time.Second)

	state := p.GetState()
	if state != StateRunning {
		t.Errorf("expected RUNNING after health check restart, got %s", state)
	}

	// Cleanup: stop the restarted process
	if st := p.GetState(); st == StateRunning || st == StateStarting {
		p.Stop()
		time.Sleep(200 * time.Millisecond)
	}
}

func TestCheckRunningProcess_HealthCheckRestartStorm(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "hc_storm", Command: "sleep 1", AutoStart: false,
		HealthCheckRestart: true, HealthCheckURL: "http://127.0.0.1:1",
		StartSecs: 10,
	})
	p.mu.Lock()
	p.State = StateRunning
	p.Healthy = false
	p.healthCheckRestartFired = true // storm prevention flag already set
	p.StartTime = time.Now()
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkRunningProcess(p)

	// Should still be RUNNING (storm prevention — restart not triggered)
	state := p.GetState()
	if state != StateRunning {
		t.Errorf("expected RUNNING, got %s", state)
	}
}

func TestCheckRunningProcess_HealthCheckNoURL(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)
	p := pm.AddProcess(&config.ProgramConfig{
		Name: "hc_nourl", Command: "sleep 1", AutoStart: false,
		HealthCheckRestart: true, // No HealthCheckURL set
		StartSecs: 10,
	})
	p.mu.Lock()
	p.State = StateRunning
	p.Healthy = false
	p.healthCheckRestartFired = false
	p.StartTime = time.Now()
	p.mu.Unlock()

	m := NewMonitor(pm)
	m.checkRunningProcess(p)

	// Should still be RUNNING (no URL → no restart)
	state := p.GetState()
	if state != StateRunning {
		t.Errorf("expected RUNNING, got %s", state)
	}
}

// ---------------------------------------------------------------------------
// Monitor lifecycle tests
// ---------------------------------------------------------------------------

func TestMonitorStartStop(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)

	m := NewMonitor(pm)
	m.Start()
	time.Sleep(100 * time.Millisecond)
	m.Stop()

	select {
	case <-m.Done:
		// ok — monitorLoop exited
	case <-time.After(2 * time.Second):
		t.Error("monitor did not stop within 2 seconds")
	}
}

func TestMonitorDoubleStop(t *testing.T) {
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("logger creation failed: %v", err)
	}
	defer logManager.Close()
	pm := NewProcessManager(logManager)

	m := NewMonitor(pm)
	m.Start()
	time.Sleep(100 * time.Millisecond)
	m.Stop()

	// Second stop should not panic (uses sync.Once)
	m.Stop()

	// Done should still be closed
	select {
	case <-m.Done:
		// ok
	case <-time.After(2 * time.Second):
		t.Error("monitor did not stop after second stop call")
	}
}
