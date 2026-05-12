package process

import (
	"fmt"
	"time"
)

type Monitor struct {
	Manager *ProcessManager
	Done    chan struct{}
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
	close(m.Done)
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
	m.Manager.mu.RLock()
	defer m.Manager.mu.RUnlock()
	for _, process := range m.Manager.Processes {
		m.checkProcess(process)
	}
}

func (m *Monitor) checkProcess(process *Process) {
	switch process.GetState() {
	case StateExited:
		m.handleExitedProcess(process)
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
		exitCode := process.ExitCode
		process.mu.Unlock()
		fmt.Printf("进程 %s 因退出码策略不重启 (exit=%d)\n", name, exitCode)
		process.sendWebhook()
		RecordEvent(name, EventFatal, process.PID, exitCode, "exit code policy")
		return
	}

	// Check restart rate limiting
	if process.restartRateExceeded() {
		process.State = StateFatal
		name := process.Name
		process.mu.Unlock()
		fmt.Printf("进程 %s 达到重启速率限制，进入FATAL状态\n", name)
		process.sendWebhook()
		RecordEvent(name, EventFatal, process.PID, process.ExitCode, "restart rate exceeded")
		return
	}

	if process.StartRetries >= process.Config.StartRetries {
		process.State = StateFatal
		name := process.Name
		process.mu.Unlock()
		fmt.Printf("进程 %s 达到最大重启次数，进入FATAL状态\n", name)
		process.sendWebhook()
		RecordEvent(name, EventFatal, process.PID, process.ExitCode, "max retries")
		return
	}

	// Record restart timestamp for rate limiting
	process.addRestartTimestamp(process.Config.RestartWindowSecs)

	// 标记为 STARTING 防止 monitor 重复调度
	process.State = StateStarting
	name := process.Name
	process.mu.Unlock()

	// 在独立 goroutine 中执行重启，不阻塞 monitor 循环
	go func() {
		time.Sleep(1 * time.Second)
		fmt.Printf("进程 %s 已退出，尝试重启...\n", name)

		if err := process.Start(); err != nil {
			fmt.Printf("重启进程 %s 失败: %v\n", name, err)

			process.mu.Lock()
			if process.StartRetries > process.Config.StartRetries {
				process.State = StateFatal
				fmt.Printf("进程 %s 达到最大重启次数，进入FATAL状态\n", name)
				process.mu.Unlock()
				process.sendWebhook()
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

	// 启动时间超过 StartSecs，重置重试次数
	if process.Config.StartSecs > 0 && time.Since(process.StartTime) > time.Duration(process.Config.StartSecs)*time.Second {
		process.StartRetries = 0
	}

	// Health check restart: unhealthy process with HealthCheckRestart enabled
	if process.Config.HealthCheckRestart && !process.Healthy && process.Config.HealthCheckURL != "" {
		if process.State != StateRunning {
			process.mu.Unlock()
			return
		}
		name := process.Name
		process.mu.Unlock()
		fmt.Printf("进程 %s 健康检查失败，触发重启...\n", name)
		if err := process.Restart(); err != nil {
			fmt.Printf("健康检查重启进程 %s 失败: %v\n", name, err)
			process.mu.Lock()
			if process.StartRetries > process.Config.StartRetries {
				process.State = StateFatal
				process.mu.Unlock()
				process.sendWebhook()
				return
			}
			process.mu.Unlock()
		}
		return
	}

	process.mu.Unlock()
}
