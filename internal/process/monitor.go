package process

import (
	"fmt"
	"sync"
	"time"
)

type Monitor struct {
	Manager  *ProcessManager
	Done     chan struct{}
	stopOnce sync.Once
}

func NewMonitor(manager *ProcessManager) *Monitor {
	return &Monitor{
		Manager: manager,
		Done:    make(chan struct{}),
	}
}

func (m *Monitor) Start() {
	go m.monitorLoop()
}

func (m *Monitor) Stop() {
	m.stopOnce.Do(func() { close(m.Done) })
}

func (m *Monitor) monitorLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkProcesses()
		case <-m.Done:
			return
		}
	}
}

func (m *Monitor) checkProcesses() {
	var procs []*Process
	m.Manager.RangeProcesses(func(name string, p *Process) {
		procs = append(procs, p)
	})
	for _, process := range procs {
		m.checkProcess(process)
	}
}

func (m *Monitor) checkProcess(process *Process) {
	switch process.GetState() {
	case StateExited:
		m.handleExitedProcess(process)

		// If the process is done (not restarting), detach fcgi socket.
		// handleExitedProcess sets StateStarting when it schedules a restart,
		// and StateFatal when it won't restart.
		if process.Config.Socket != "" && process.manager != nil {
			state := process.GetState()
			if state == StateFatal || state == StateExited || state == StateStopped {
				baseName := fcgiBaseName(process.Config.Name)
				process.manager.mu.Lock()
				if sm, ok := process.manager.fcgiSockets[baseName]; ok {
					if sm.Detach() {
						sm.Close()
						delete(process.manager.fcgiSockets, baseName)
					}
				}
				process.manager.mu.Unlock()
			}
		}
	case StateRunning:
		m.checkRunningProcess(process)
	}
}

func (m *Monitor) handleExitedProcess(process *Process) {
	process.mu.Lock()
	if !process.Config.AutoRestart {
		process.mu.Unlock()
		return
	}

	if process.State != StateExited {
		process.mu.Unlock()
		return
	}

	// Check exit code policy
	if !process.shouldRestartOnExitCode() {
		process.State = StateFatal
		name := process.Name
		pid := process.PID
		exitCode := process.ExitCode
		process.mu.Unlock()
		fmt.Printf("进程 %s 因退出码策略不重启 (exit=%d)\n", name, exitCode)
		process.sendWebhook(StateFatal, pid, exitCode)
		RecordEvent(name, EventFatal, pid, exitCode, "exit code policy")
		return
	}

	// Reset retry count if exit code is in ExitCodes (expected/normal exits)
	for _, code := range process.Config.ExitCodes {
		if process.ExitCode == code {
			process.StartRetries = 0
			break
		}
	}

	// Check restart rate limiting
	if process.restartRateExceeded() {
		process.State = StateFatal
		name := process.Name
		pid := process.PID
		exitCode := process.ExitCode
		process.mu.Unlock()
		fmt.Printf("进程 %s 达到重启速率限制，进入FATAL状态\n", name)
		process.sendWebhook(StateFatal, pid, exitCode)
		RecordEvent(name, EventFatal, pid, exitCode, "restart rate exceeded")
		return
	}

	if process.StartRetries >= process.Config.StartRetries {
		process.State = StateFatal
		name := process.Name
		pid := process.PID
		exitCode := process.ExitCode
		process.mu.Unlock()
		fmt.Printf("进程 %s 达到最大重启次数，进入FATAL状态\n", name)
		process.sendWebhook(StateFatal, pid, exitCode)
		RecordEvent(name, EventFatal, pid, exitCode, "max retries")
		return
	}

	// 标记为 STARTING 防止 monitor 重复调度
	process.State = StateStarting
	name := process.Name
	process.mu.Unlock()

	// 在独立 goroutine 中执行重启，不阻塞 monitor 循环
	go func() {
		time.Sleep(1 * time.Second)

		process.mu.Lock()
		// Stop() was called while we slept — bail.
		if process.State == StateStopped || process.State == StateStopping {
			process.mu.Unlock()
			return
		}
		// Health check restart already took over — bail.
		if process.State == StateStarting && process.healthCheckRestartFired {
			process.mu.Unlock()
			return
		}
		// Reset state so Start() can proceed (we set it to Starting on line 114
		// to prevent the monitor from re-scheduling this process).
		if process.State == StateStarting {
			process.State = StateExited
		}
		process.mu.Unlock()

		fmt.Printf("进程 %s 已退出，尝试重启...\n", name)

		if err := process.Start(); err != nil {
			fmt.Printf("重启进程 %s 失败: %v\n", name, err)

			process.mu.Lock()
			if process.State == StateStopped || process.State == StateStopping || process.State == StateStarting {
				process.mu.Unlock()
				return
			}
			if process.StartRetries > process.Config.StartRetries {
				process.State = StateFatal
				pid := process.PID
				exitCode := process.ExitCode
				fmt.Printf("进程 %s 达到最大重启次数，进入FATAL状态\n", name)
				process.mu.Unlock()
				process.sendWebhook(StateFatal, pid, exitCode)
				return
			}
			process.mu.Unlock()
		}
	}()
}

func (m *Monitor) checkRunningProcess(process *Process) {
	process.mu.Lock()

	// 正在停止中，不检查
	if process.State == StateStopping {
		process.mu.Unlock()
		return
	}

	// 启动时间超过 StartSecs（且进程健康或无健康检查），重置重试次数。
	// StartSecs <= 0 在 Start() 中立即重置（见 process.go）。
	if process.Config.StartSecs > 0 && time.Since(process.StartTime) > time.Duration(process.Config.StartSecs)*time.Second && (process.Healthy || process.Config.HealthCheckURL == "") {
		process.StartRetries = 0
	}

	// Health check restart: unhealthy process with HealthCheckRestart enabled.
	// Only fire once per unhealthy cycle to prevent restart storms.
	if process.Config.HealthCheckRestart && !process.Healthy && process.Config.HealthCheckURL != "" {
		if process.State != StateRunning {
			process.mu.Unlock()
			return
		}
		if process.healthCheckRestartFired {
			process.mu.Unlock()
			return
		}
		process.healthCheckRestartFired = true
		name := process.Name
		process.mu.Unlock()
		fmt.Printf("进程 %s 健康检查失败，触发重启...\n", name)
		if err := process.Restart(); err != nil {
			fmt.Printf("健康检查重启进程 %s 失败: %v\n", name, err)
			process.mu.Lock()
			// If another path already started a restart, ignore this failure.
			if process.State == StateRunning || process.State == StateStarting {
				process.mu.Unlock()
				return
			}
			if process.StartRetries > process.Config.StartRetries {
				process.State = StateFatal
				pid := process.PID
				exitCode := process.ExitCode
				process.mu.Unlock()
				process.sendWebhook(StateFatal, pid, exitCode)
				return
			}
			process.mu.Unlock()
		}
		return
	}

	process.mu.Unlock()
}
