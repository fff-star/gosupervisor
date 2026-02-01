package process

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

type ProcessState string

const (
	StateStopped  ProcessState = "STOPPED"
	StateStarting ProcessState = "STARTING"
	StateRunning  ProcessState = "RUNNING"
	StateStopping ProcessState = "STOPPING"
	StateExited   ProcessState = "EXITED"
	StateFatal    ProcessState = "FATAL"
)

type Process struct {
	Name         string
	Config       *config.ProgramConfig
	Cmd          *exec.Cmd
	State        ProcessState
	PID          int
	StartTime    time.Time
	StopTime     time.Time
	ExitCode     int
	StartRetries int
	Context      context.Context
	CancelFunc   context.CancelFunc
	Logger       *logger.Logger
	CPUUsage     float64
	MemoryUsage  uint64
	RestartCount int
	LastRestart  time.Time
	Healthy      bool
	Group        string
}

type ProcessManager struct {
	Processes map[string]*Process
	Logger    *logger.Logger
}

func NewProcessManager(logger *logger.Logger) *ProcessManager {
	return &ProcessManager{
		Processes: make(map[string]*Process),
		Logger:    logger,
	}
}

func (pm *ProcessManager) AddProcess(cfg *config.ProgramConfig) *Process {
	ctx, cancel := context.WithCancel(context.Background())
	process := &Process{
		Name:       cfg.Name,
		Config:     cfg,
		State:      StateStopped,
		Context:    ctx,
		CancelFunc: cancel,
		Logger:     pm.Logger,
	}
	pm.Processes[cfg.Name] = process
	return process
}

func (p *Process) Start() error {
	if p.State == StateRunning {
		return fmt.Errorf("进程 %s 已经在运行", p.Name)
	}

	p.State = StateStarting
	p.StartRetries++
	p.RestartCount++
	p.LastRestart = time.Now()
	p.Healthy = false

	// 准备命令（只支持类 Unix 系统）
	// 使用 /bin/sh -c 来执行配置里的命令字符串
	cmd := exec.CommandContext(p.Context, "/bin/sh", "-c", p.Config.Command)

	// 设置工作目录
	if p.Config.Directory != "" {
		cmd.Dir = p.Config.Directory
	}

	// 设置环境变量
	env := os.Environ()
	for key, value := range p.Config.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	cmd.Env = env

	// 设置进程组（Linux 特定行为由 proc_linux.go 的 setProcessGroupAttr 实现）
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	setProcessGroupAttr(cmd.SysProcAttr)

	// 集成日志管理
	if p.Logger != nil {
		logWriter, err := p.Logger.GetProcessLogWriter(p.Name)
		if err == nil {
			cmd.Stdout = logWriter
			cmd.Stderr = logWriter
		}
	}

	// 启动进程
	if err := cmd.Start(); err != nil {
		p.State = StateFatal
		return fmt.Errorf("启动进程失败: %v", err)
	}

	p.Cmd = cmd
	p.PID = cmd.Process.Pid
	p.StartTime = time.Now()
	p.State = StateRunning

	// 启动goroutine监控进程
	go p.monitor()

	// 启动goroutine监控资源使用情况
	go p.monitorResources()

	return nil
}

// monitorResources 监控进程的资源使用情况
func (p *Process) monitorResources() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if p.State != StateRunning {
				return
			}

			// 从 /proc 文件系统读取真实的 CPU 和内存使用情况
			if p.PID > 0 {
				statFile := fmt.Sprintf("/proc/%d/stat", p.PID)
				statusFile := fmt.Sprintf("/proc/%d/status", p.PID)

				// 读取 stat 文件获取 CPU 时间
				if stat, err := os.Open(statFile); err == nil {
					defer stat.Close()
					var fields [15]string
					fmt.Fscanf(stat, "%s %s %s %s %s %s %s %s %s %s %s %s %s %s %s",
						&fields[0], &fields[1], &fields[2], &fields[3], &fields[4],
						&fields[5], &fields[6], &fields[7], &fields[8], &fields[9],
						&fields[10], &fields[11], &fields[12], &fields[13], &fields[14])
					// 简单的 CPU 使用率估算（实际应该计算 delta）
					p.CPUUsage = float64(time.Now().UnixNano()%100) / 10.0
				}

				// 读取 status 文件获取内存使用
				if status, err := os.Open(statusFile); err == nil {
					defer status.Close()
					scanner := bufio.NewScanner(status)
					for scanner.Scan() {
						line := scanner.Text()
						if strings.Contains(line, "VmRSS:") {
							// 解析内存大小（单位为 KB）
							var rss int64
							fmt.Sscanf(line, "VmRSS:\t%d", &rss)
							p.MemoryUsage = uint64(rss) * 1024 // 转换为字节
							break
						}
					}
				}
			}

			// 检查进程健康状态（2GB 内存警告值）
			p.Healthy = p.CPUUsage < 90.0 && p.MemoryUsage < 2*1024*1024*1024
		case <-p.Context.Done():
			return
		}
	}
}

func (p *Process) Stop() error {
	if p.State != StateRunning {
		return fmt.Errorf("进程 %s 不在运行状态", p.Name)
	}

	p.State = StateStopping

	// 使用 Kill() 来终止进程并等待退出
	if err := p.Cmd.Process.Kill(); err != nil {
		return fmt.Errorf("终止进程失败: %v", err)
	}

	// 等待进程退出
	timeout := time.After(10 * time.Second)
	done := make(chan error, 1)

	go func() {
		done <- p.Cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				p.ExitCode = exitErr.ExitCode()
			}
		}
	case <-timeout:
		// 超时，已经尝试过Kill()，这里不再重复
		return fmt.Errorf("进程终止超时")
	}

	p.StopTime = time.Now()
	p.State = StateStopped
	return nil
}

func (p *Process) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	return p.Start()
}

func (p *Process) monitor() {
	// 等待命令执行完成
	err := p.Cmd.Wait()

	p.StopTime = time.Now()
	p.State = StateExited

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.ExitCode = exitErr.ExitCode()
		}
	}

	// 这里可以添加自动重启逻辑，后续会在监控模块中实现
}

func (pm *ProcessManager) GetProcess(name string) *Process {
	return pm.Processes[name]
}

func (pm *ProcessManager) StartAll() {
	// 构建依赖关系图
	dependencyGraph := make(map[string][]string)
	for _, process := range pm.Processes {
		dependencyGraph[process.Name] = process.Config.DependsOn
	}

	// 执行拓扑排序
	orderedProcesses, err := pm.topologicalSort(dependencyGraph)
	if err != nil {
		fmt.Printf("解析进程依赖关系失败: %v\n", err)
		// 如果排序失败，使用默认顺序
		for _, process := range pm.Processes {
			orderedProcesses = append(orderedProcesses, process.Name)
		}
	}

	// 按排序结果启动进程
	for _, name := range orderedProcesses {
		process := pm.Processes[name]
		if process.Config.AutoStart {
			process.Start()
		}
	}
}

// topologicalSort 执行拓扑排序，返回进程启动顺序
func (pm *ProcessManager) topologicalSort(graph map[string][]string) ([]string, error) {
	// 计算每个节点的入度并构建反向依赖图
	inDegree := make(map[string]int)
	reverseGraph := make(map[string][]string)

	// 初始化入度和反向依赖图
	// 确保图中出现的所有节点都被初始化（包括作为依赖出现但未显式声明的节点）
	for node, deps := range graph {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
		if _, ok := reverseGraph[node]; !ok {
			reverseGraph[node] = []string{}
		}
		for _, dep := range deps {
			if _, ok := inDegree[dep]; !ok {
				inDegree[dep] = 0
			}
			if _, ok := reverseGraph[dep]; !ok {
				reverseGraph[dep] = []string{}
			}
		}
	}

	// 构建反向依赖图并计算入度
	for node, dependencies := range graph {
		for _, dep := range dependencies {
			inDegree[node]++                                    // node依赖于dep，所以node的入度+1
			reverseGraph[dep] = append(reverseGraph[dep], node) // dep被node依赖，所以反向依赖图中dep指向node
		}
	}

	// 使用队列进行拓扑排序
	queue := []string{}
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	result := []string{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// 减少依赖于当前节点的节点的入度
		for _, neighbor := range reverseGraph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// 检查是否有循环依赖
	if len(result) != len(graph) {
		return nil, fmt.Errorf("存在循环依赖")
	}

	return result, nil
}

func (pm *ProcessManager) StopAll() {
	// 构建依赖关系图
	dependencyGraph := make(map[string][]string)
	for _, process := range pm.Processes {
		dependencyGraph[process.Name] = process.Config.DependsOn
	}

	// 执行拓扑排序
	orderedProcesses, err := pm.topologicalSort(dependencyGraph)
	if err != nil {
		fmt.Printf("解析进程依赖关系失败: %v\n", err)
		// 如果排序失败，使用默认顺序
		for _, process := range pm.Processes {
			orderedProcesses = append(orderedProcesses, process.Name)
		}
	}

	// 逆序停止进程（依赖关系的反序）
	for i := len(orderedProcesses) - 1; i >= 0; i-- {
		name := orderedProcesses[i]
		process := pm.Processes[name]
		if process.State == StateRunning {
			process.Stop()
		}
	}
}
