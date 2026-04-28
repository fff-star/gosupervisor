package process

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	mu sync.Mutex

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

	// waitCh is closed by monitor() after cmd.Wait() returns.
	// Stop() waits on it instead of calling cmd.Wait() directly.
	waitCh chan struct{}
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
	p.mu.Lock()
	if p.State == StateRunning {
		p.mu.Unlock()
		return fmt.Errorf("进程 %s 已经在运行", p.Name)
	}

	// 先停止旧进程，避免进程泄漏
	if p.Cmd != nil && p.Cmd.Process != nil {
		p.Cmd.Process.Kill()
	}

	p.State = StateStarting
	p.StartRetries++
	if p.PID > 0 {
		p.RestartCount++
	}
	p.LastRestart = time.Now()
	p.Healthy = false

	// 准备命令（只支持类 Unix 系统）
	ctx, cancel := context.WithCancel(p.Context)
	_ = cancel // keep for future use
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", p.Config.Command)

	if p.Config.Directory != "" {
		cmd.Dir = p.Config.Directory
	}

	env := os.Environ()
	for key, value := range p.Config.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	cmd.Env = env

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	setProcessGroupAttr(cmd.SysProcAttr)

	if p.Logger != nil {
		logWriter, err := p.Logger.GetProcessLogWriter(p.Name)
		if err == nil {
			cmd.Stdout = logWriter
			cmd.Stderr = logWriter
		}
	}

	// 创建 waitCh 供 Stop() 使用
	p.waitCh = make(chan struct{})
	p.mu.Unlock()

	// 执行 fork/exec（不持有锁）
	if err := cmd.Start(); err != nil {
		p.mu.Lock()
		p.State = StateExited
		p.mu.Unlock()
		return fmt.Errorf("启动进程失败: %v", err)
	}

	p.mu.Lock()
	p.Cmd = cmd
	p.PID = cmd.Process.Pid
	p.StartTime = time.Now()
	p.State = StateRunning
	p.mu.Unlock()

	go p.monitor()
	go p.monitorResources()

	return nil
}

func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.Cmd

	if cmd == nil || cmd.Process == nil {
		p.State = StateStopped
		p.mu.Unlock()
		return nil
	}

	if p.State != StateRunning && p.State != StateStopping {
		cmd.Process.Kill()
		p.State = StateStopped
		p.mu.Unlock()
		return nil
	}

	p.State = StateStopping
	waitCh := p.waitCh
	p.mu.Unlock()

	// 发送 SIGKILL（不持有锁）
	cmd.Process.Kill()

	// 等待 monitor() goroutine 的 cmd.Wait() 返回
	if waitCh != nil {
		if p.Config.StopSecs > 0 {
			select {
			case <-waitCh:
			case <-time.After(time.Duration(p.Config.StopSecs) * time.Second):
			}
		} else {
			<-waitCh
		}
	}

	p.mu.Lock()
	p.StopTime = time.Now()
	if p.State == StateStopping {
		p.State = StateStopped
	}
	p.mu.Unlock()
	return nil
}

func (p *Process) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	return p.Start()
}

// GetState returns a thread-safe copy of the process state.
func (p *Process) GetState() ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State
}

// Snapshot holds a point-in-time copy of a Process's display fields.
// All fields are read under the Process mutex.
type Snapshot struct {
	Name         string
	State        ProcessState
	PID          int
	StartTime    time.Time
	StopTime     time.Time
	ExitCode     int
	StartRetries int
	RestartCount int
	LastRestart  time.Time
	CPUUsage     float64
	MemoryUsage  uint64
	Healthy      bool
	Config       *config.ProgramConfig
}

// Snapshot returns a thread-safe copy of the process's display fields.
func (p *Process) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{
		Name:         p.Name,
		State:        p.State,
		PID:          p.PID,
		StartTime:    p.StartTime,
		StopTime:     p.StopTime,
		ExitCode:     p.ExitCode,
		StartRetries: p.StartRetries,
		RestartCount: p.RestartCount,
		LastRestart:  p.LastRestart,
		CPUUsage:     p.CPUUsage,
		MemoryUsage:  p.MemoryUsage,
		Healthy:      p.Healthy,
		Config:       p.Config,
	}
}

// monitor 等待进程退出，由 Start() 启动为 goroutine
func (p *Process) monitor() {
	err := p.Cmd.Wait()

	p.mu.Lock()
	// 关闭 waitCh，通知 Stop() 等待者
	if p.waitCh != nil {
		close(p.waitCh)
		p.waitCh = nil
	}

	p.StopTime = time.Now()
	if p.State != StateStopping {
		p.State = StateExited
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.ExitCode = exitErr.ExitCode()
		}
	}
	p.mu.Unlock()
}

// monitorResources 监控进程的资源使用情况
func (p *Process) monitorResources() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			if p.State != StateRunning {
				p.mu.Unlock()
				return
			}
			pid := p.PID
			p.mu.Unlock()

			if pid > 0 {
				p.readProcStats(pid)
			}
		case <-p.Context.Done():
			return
		}
	}
}

// readProcStats 读取 /proc 文件系统中的资源使用信息
func (p *Process) readProcStats(pid int) {
	// 读取 VmRSS 内存使用
	statusFile := fmt.Sprintf("/proc/%d/status", pid)
	if f, err := os.Open(statusFile); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "VmRSS:") {
				var rss int64
				fmt.Sscanf(line, "VmRSS:\t%d", &rss)
				p.mu.Lock()
				p.MemoryUsage = uint64(rss) * 1024
				p.mu.Unlock()
				break
			}
		}
		f.Close()
	}

	// /proc/pid/stat: 找最后一个 ')' 后解析 utime/stime
	statFile := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statFile)
	if err != nil {
		return
	}
	content := string(data)
	// 跳过 comm 字段（被括号包围，如 "(sleep)"）
	idx := strings.LastIndex(content, ")")
	if idx < 0 || idx+2 >= len(content) {
		return
	}
	rest := strings.Fields(content[idx+2:])
	// rest[0]=state, rest[11]=utime, rest[12]=stime (14-indexed from start, minus pid and comm)
	if len(rest) < 13 {
		return
	}
	var utime, stime int64
	fmt.Sscanf(rest[11], "%d", &utime)
	fmt.Sscanf(rest[12], "%d", &stime)
	cpuTotal := utime + stime

	p.mu.Lock()
	p.CPUUsage = float64(cpuTotal)
	// 健康检查
	p.Healthy = p.CPUUsage < 90.0 && p.MemoryUsage < 2*1024*1024*1024
	p.mu.Unlock()
}

func (pm *ProcessManager) GetProcess(name string) *Process {
	return pm.Processes[name]
}

func (pm *ProcessManager) StartAll() {
	dependencyGraph := make(map[string][]string)
	for _, process := range pm.Processes {
		dependencyGraph[process.Name] = process.Config.DependsOn
	}

	orderedProcesses, err := pm.topologicalSort(dependencyGraph)
	if err != nil {
		fmt.Printf("解析进程依赖关系失败: %v\n", err)
		for _, process := range pm.Processes {
			orderedProcesses = append(orderedProcesses, process.Name)
		}
	}

	for _, name := range orderedProcesses {
		process := pm.Processes[name]
		if process.Config.AutoStart {
			if err := process.Start(); err != nil {
				fmt.Printf("启动进程 %s 失败: %v\n", name, err)
			}
		}
	}
}

func (pm *ProcessManager) StopAll() {
	dependencyGraph := make(map[string][]string)
	for _, process := range pm.Processes {
		dependencyGraph[process.Name] = process.Config.DependsOn
	}

	orderedProcesses, err := pm.topologicalSort(dependencyGraph)
	if err != nil {
		fmt.Printf("解析进程依赖关系失败: %v\n", err)
		for _, process := range pm.Processes {
			orderedProcesses = append(orderedProcesses, process.Name)
		}
	}

	// 逆序停止进程
	for i := len(orderedProcesses) - 1; i >= 0; i-- {
		name := orderedProcesses[i]
		process := pm.Processes[name]
		state := process.GetState()
		if state == StateRunning || state == StateStarting || state == StateExited || state == StateFatal {
			process.Stop()
		}
	}
}

func (pm *ProcessManager) topologicalSort(graph map[string][]string) ([]string, error) {
	inDegree := make(map[string]int)
	reverseGraph := make(map[string][]string)

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

	for node, dependencies := range graph {
		for _, dep := range dependencies {
			inDegree[node]++
			reverseGraph[dep] = append(reverseGraph[dep], node)
		}
	}

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

		for _, neighbor := range reverseGraph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(graph) {
		return nil, fmt.Errorf("存在循环依赖")
	}

	return result, nil
}
