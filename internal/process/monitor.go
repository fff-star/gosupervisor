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
	for _, process := range m.Manager.Processes {
		m.checkProcess(process)
	}
}

func (m *Monitor) checkProcess(process *Process) {
	switch process.State {
	case StateExited:
		m.handleExitedProcess(process)
	case StateRunning:
		m.checkRunningProcess(process)
	}
}

func (m *Monitor) handleExitedProcess(process *Process) {
	if process.Config.AutoRestart {
		if process.StartRetries <= process.Config.StartRetries {
			// 等待一段时间后重启
			time.Sleep(1 * time.Second)
			fmt.Printf("进程 %s 已退出，尝试重启...\n", process.Name)
			if err := process.Start(); err != nil {
				fmt.Printf("重启进程 %s 失败: %v\n", process.Name, err)
				if process.StartRetries >= process.Config.StartRetries {
					process.State = StateFatal
					fmt.Printf("进程 %s 达到最大重启次数，进入FATAL状态\n", process.Name)
				}
			}
		} else {
			process.State = StateFatal
			fmt.Printf("进程 %s 达到最大重启次数，进入FATAL状态\n", process.Name)
		}
	}
}

func (m *Monitor) checkRunningProcess(process *Process) {
	// 检查进程是否真的在运行
	if process.Cmd != nil && process.Cmd.Process != nil {
		if err := process.Cmd.Process.Signal(nil); err != nil {
			// 进程不存在，标记为已退出
			process.State = StateExited
			fmt.Printf("进程 %s 不存在，标记为已退出\n", process.Name)
			m.handleExitedProcess(process)
		}
	}

	// 检查启动时间，判断是否启动成功
	if process.State == StateRunning && time.Since(process.StartTime) > time.Duration(process.Config.StartSecs)*time.Second {
		// 启动成功，重置重试次数
		process.StartRetries = 0
	}
}
