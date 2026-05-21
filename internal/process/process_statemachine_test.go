package process

import (
	"syscall"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

// TestStateTransitions systematically tests every valid and invalid state
// transition for Process.Start(), Stop(), Restart(), and Signal().
func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, pm *ProcessManager) *Process
		action    func(*Process) error
		wantState ProcessState
		wantErr   bool
	}{
		// === START transitions ===
		{
			name: "STOPPED_Start_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				return pm.AddProcess(&config.ProgramConfig{
					Name: "start_stopped", Command: "sleep 1", AutoStart: false,
				})
			},
			action:    (*Process).Start,
			wantState: StateRunning,
			wantErr:   false,
		},
		{
			name: "RUNNING_Start_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "start_running", Command: "sleep 60", AutoStart: false,
				})
				if err := p.Start(); err != nil {
					t.Fatalf("setup Start failed: %v", err)
				}
				time.Sleep(200 * time.Millisecond)
				return p
			},
			action:    (*Process).Start,
			wantState: StateRunning,
			wantErr:   true,
		},
		{
			name: "FATAL_Start_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "start_fatal", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateFatal
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Start,
			wantState: StateRunning,
			wantErr:   false,
		},
		{
			name: "EXITED_Start_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "start_exited", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateExited
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Start,
			wantState: StateRunning,
			wantErr:   false,
		},

		// === STOP transitions ===
		{
			name: "RUNNING_Stop_STOPPED",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "stop_running", Command: "sleep 60", AutoStart: false,
				})
				if err := p.Start(); err != nil {
					t.Fatalf("setup Start failed: %v", err)
				}
				time.Sleep(200 * time.Millisecond)
				return p
			},
			action:    (*Process).Stop,
			wantState: StateStopped,
			wantErr:   false,
		},
		{
			name: "STOPPED_Stop_STOPPED",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				return pm.AddProcess(&config.ProgramConfig{
					Name: "stop_stopped", Command: "sleep 1", AutoStart: false,
				})
			},
			action:    (*Process).Stop,
			wantState: StateStopped,
			wantErr:   false,
		},
		{
			name: "FATAL_Stop_STOPPED",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "stop_fatal", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateFatal
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Stop,
			wantState: StateStopped,
			wantErr:   false,
		},
		{
			name: "EXITED_Stop_STOPPED",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "stop_exited", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateExited
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Stop,
			wantState: StateStopped,
			wantErr:   false,
		},
		{
			name: "STARTING_Stop_STOPPED",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "stop_starting", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStarting
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Stop,
			wantState: StateStopped,
			wantErr:   false,
		},
		{
			name: "STOPPING_Stop_STOPPED",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "stop_stopping", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStopping
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Stop,
			wantState: StateStopped,
			wantErr:   false,
		},

		// === RESTART transitions ===
		{
			name: "RUNNING_Restart_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "restart_running", Command: "sleep 60", AutoStart: false,
				})
				if err := p.Start(); err != nil {
					t.Fatalf("setup Start failed: %v", err)
				}
				time.Sleep(200 * time.Millisecond)
				return p
			},
			action:    (*Process).Restart,
			wantState: StateRunning,
			wantErr:   false,
		},
		{
			name: "FATAL_Restart_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "restart_fatal", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateFatal
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Restart,
			wantState: StateFatal,
			wantErr:   true,
		},
		{
			name: "STOPPING_Restart_nil",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "restart_stopping", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStopping
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Restart,
			wantState: StateStopping,
			wantErr:   false,
		},
		{
			name: "STARTING_Restart_nil",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "restart_starting", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStarting
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Restart,
			wantState: StateStarting,
			wantErr:   false,
		},
		{
			name: "STOPPED_Restart_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				return pm.AddProcess(&config.ProgramConfig{
					Name: "restart_stopped", Command: "sleep 1", AutoStart: false,
				})
			},
			action:    (*Process).Restart,
			wantState: StateRunning,
			wantErr:   false,
		},
		{
			name: "EXITED_Restart_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "restart_exited", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateExited
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Restart,
			wantState: StateRunning,
			wantErr:   false,
		},

		// === SIGNAL transitions ===
		{
			name: "RUNNING_Signal_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "signal_running", Command: "sleep 60", AutoStart: false,
				})
				if err := p.Start(); err != nil {
					t.Fatalf("setup Start failed: %v", err)
				}
				time.Sleep(200 * time.Millisecond)
				return p
			},
			action: func(p *Process) error {
				return p.Signal(syscall.SIGCONT)
			},
			wantState: StateRunning,
			wantErr:   false,
		},
		{
			name: "STOPPED_Signal_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				return pm.AddProcess(&config.ProgramConfig{
					Name: "signal_stopped", Command: "sleep 1", AutoStart: false,
				})
			},
			action: func(p *Process) error {
				return p.Signal(syscall.SIGCONT)
			},
			wantState: StateStopped,
			wantErr:   true,
		},
		{
			name: "EXITED_Signal_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "signal_exited", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateExited
				p.mu.Unlock()
				return p
			},
			action: func(p *Process) error {
				return p.Signal(syscall.SIGCONT)
			},
			wantState: StateExited,
			wantErr:   true,
		},
		{
			name: "FATAL_Signal_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "signal_fatal", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateFatal
				p.mu.Unlock()
				return p
			},
			action: func(p *Process) error {
				return p.Signal(syscall.SIGCONT)
			},
			wantState: StateFatal,
			wantErr:   true,
		},
		{
			name: "STARTING_Signal_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "signal_starting", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStarting
				p.mu.Unlock()
				return p
			},
			action: func(p *Process) error {
				return p.Signal(syscall.SIGCONT)
			},
			wantState: StateStarting,
			wantErr:   true,
		},
		{
			name: "STOPPING_Signal_error",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "signal_stopping", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStopping
				p.mu.Unlock()
				return p
			},
			action: func(p *Process) error {
				return p.Signal(syscall.SIGCONT)
			},
			wantState: StateStopping,
			wantErr:   true,
		},

		// === START from intermediate states ===
		{
			name: "STARTING_Start_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "start_from_starting", Command: "sleep 1", AutoStart: false,
				})
				// STARTING with no cmd — simulates the window between
				// Start() setting StateStarting and cmd.Start() completing.
				// A concurrent Start() call should be rejected.
				p.mu.Lock()
				p.State = StateStarting
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Start,
			wantState: StateStarting,
			wantErr:   true,
		},
		{
			name: "STOPPING_Start_RUNNING",
			setup: func(t *testing.T, pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "start_from_stopping", Command: "sleep 1", AutoStart: false,
				})
				p.mu.Lock()
				p.State = StateStopping
				p.mu.Unlock()
				return p
			},
			action:    (*Process).Start,
			wantState: StateRunning,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logManager, err := logger.NewDefaultLogger(t.TempDir())
			if err != nil {
				t.Fatalf("logger creation failed: %v", err)
			}
			defer logManager.Close()

			pm := NewProcessManager(logManager)
			p := tt.setup(t, pm)

			actErr := tt.action(p)

			if (actErr != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, actErr)
			}

			// Allow async transitions (monitor goroutine) to settle.
			time.Sleep(100 * time.Millisecond)

			state := p.GetState()
			if state != tt.wantState {
				t.Errorf("want state %s, got %s", tt.wantState, state)
			}

			// Cleanup: stop the process if it is still running or starting.
			if st := p.GetState(); st == StateRunning || st == StateStarting {
				p.Stop()
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
}

// TestHandleExitedProcessTransitions tests every code path through
// Monitor.handleExitedProcess.
func TestHandleExitedProcessTransitions(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(pm *ProcessManager) *Process
		wantState ProcessState
	}{
		{
			name: "EXITED_NoAutoRestart_stays_EXITED",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "no_autorestart", Command: "sleep 1",
					AutoRestart: false, StartRetries: 3,
				})
				p.mu.Lock()
				p.State = StateExited
				p.mu.Unlock()
				return p
			},
			wantState: StateExited,
		},
		{
			name: "EXITED_NotExitedState_noop",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "not_exited", Command: "sleep 1",
					AutoRestart: true, StartRetries: 3,
				})
				// Process is STOPPED, not EXITED — handleExitedProcess should return early.
				return p
			},
			wantState: StateStopped,
		},
		{
			name: "EXITED_RestartCodesMismatch_FATAL",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "restartcodes", Command: "sleep 1",
					AutoRestart: true, StartRetries: 3,
					RestartCodes: []int{1, 2},
				})
				p.mu.Lock()
				p.State = StateExited
				p.ExitCode = 0
				p.mu.Unlock()
				return p
			},
			wantState: StateFatal,
		},
		{
			name: "EXITED_NoRestartCodesBlocked_FATAL",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "norestartcodes", Command: "sleep 1",
					AutoRestart: true, StartRetries: 3,
					NoRestartCodes: []int{0, 143},
				})
				p.mu.Lock()
				p.State = StateExited
				p.ExitCode = 143
				p.mu.Unlock()
				return p
			},
			wantState: StateFatal,
		},
		{
			name: "EXITED_RateLimitExceeded_FATAL",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "ratelimit", Command: "sleep 1",
					AutoRestart: true, StartRetries: 99,
					RestartMaxCount: 3, RestartWindowSecs: 60,
				})
				p.mu.Lock()
				p.State = StateExited
				now := time.Now()
				p.restartTimestamps = []time.Time{now, now, now}
				p.mu.Unlock()
				return p
			},
			wantState: StateFatal,
		},
		{
			name: "EXITED_RateLimitNotExceeded_STARTING",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "ratelimit_ok", Command: "sleep 1",
					AutoRestart: true, StartRetries: 99,
					RestartMaxCount: 5, RestartWindowSecs: 60,
				})
				p.mu.Lock()
				p.State = StateExited
				now := time.Now()
				p.restartTimestamps = []time.Time{now.Add(-10 * time.Second)}
				p.mu.Unlock()
				return p
			},
			wantState: StateStarting,
		},
		{
			name: "EXITED_StartRetriesExceeded_FATAL",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "retries_exceeded", Command: "sleep 1",
					AutoRestart: true, StartRetries: 2,
				})
				p.mu.Lock()
				p.State = StateExited
				p.StartRetries = 2
				p.mu.Unlock()
				return p
			},
			wantState: StateFatal,
		},
		{
			name: "EXITED_AutoRestartTrue_STARTING",
			setup: func(pm *ProcessManager) *Process {
				p := pm.AddProcess(&config.ProgramConfig{
					Name: "autorestart", Command: "sleep 1",
					AutoRestart: true, StartRetries: 5,
				})
				p.mu.Lock()
				p.State = StateExited
				p.StartRetries = 0
				p.mu.Unlock()
				return p
			},
			wantState: StateStarting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logManager, err := logger.NewDefaultLogger(t.TempDir())
			if err != nil {
				t.Fatalf("logger creation failed: %v", err)
			}
			defer logManager.Close()

			pm := NewProcessManager(logManager)
			p := tt.setup(pm)

			m := &Monitor{Manager: pm}
			m.handleExitedProcess(p)

			state := p.GetState()
			if state != tt.wantState {
				t.Errorf("want state %s, got %s", tt.wantState, state)
			}

			// handleExitedProcess may have set StateStarting and spawned a goroutine
			// that will attempt Start() after a 1-second sleep. Prevent that by
			// resetting the state so the goroutine returns early.
			if state == StateStarting {
				p.mu.Lock()
				p.State = StateStopped
				p.mu.Unlock()
			}

			// If state is FATAL and the process was never started, cmd is nil,
			// so no cleanup is needed.
		})
	}
}
