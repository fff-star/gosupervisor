package process

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"sort"
	"strconv"
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

// umaskMu prevents races when setting umask before fork.
var umaskMu sync.Mutex

var stopSignals = map[string]syscall.Signal{
	"SIGTERM": syscall.SIGTERM,
	"SIGQUIT": syscall.SIGQUIT,
	"SIGINT":  syscall.SIGINT,
	"SIGHUP":  syscall.SIGHUP,
	"SIGUSR1": syscall.SIGUSR1,
	"SIGUSR2": syscall.SIGUSR2,
	"SIGKILL": syscall.SIGKILL,
}

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

	// CPU percentage tracking
	prevCPUTicks int64
	prevCPUTime  time.Time

	// System-wide CPU ticks for accurate percentage
	prevSysTicks int64
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

	ctx, cancel := context.WithCancel(p.Context)
	_ = cancel
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

	if p.Config.User != "" {
		u, err := user.Lookup(p.Config.User)
		if err != nil {
			p.State = StateExited
			p.mu.Unlock()
			return fmt.Errorf("查找用户 %s 失败: %v", p.Config.User, err)
		}
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)

		gidStrs, _ := u.GroupIds()
		var groups []uint32
		for _, gs := range gidStrs {
			g, _ := strconv.ParseUint(gs, 10, 32)
			groups = append(groups, uint32(g))
		}

		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid:         uint32(uid),
			Gid:         uint32(gid),
			Groups:      groups,
			NoSetGroups: false,
		}
	}

	if p.Logger != nil {
		stdoutWriter, stderrWriter, err := p.Logger.GetProcessLogWriters(p.Name, p.Config)
		if err == nil {
			if p.Config.RedirectStdout {
				cmd.Stdout = stdoutWriter
			}
			if p.Config.RedirectStderr {
				cmd.Stderr = stderrWriter
			}
		}
	}

	p.waitCh = make(chan struct{})
	p.mu.Unlock()

	// Set umask before fork (process-wide, so serialise via mutex)
	umaskMu.Lock()
	oldUmask := syscall.Umask(p.Config.Umask)
	if err := cmd.Start(); err != nil {
		syscall.Umask(oldUmask)
		umaskMu.Unlock()
		p.mu.Lock()
		p.State = StateExited
		p.mu.Unlock()
		return fmt.Errorf("启动进程失败: %v", err)
	}
	syscall.Umask(oldUmask)
	umaskMu.Unlock()

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

	// Send graceful stop signal
	sig, ok := stopSignals[p.Config.StopSignal]
	if !ok {
		sig = syscall.SIGTERM
	}
	cmd.Process.Signal(sig)

	// Wait for graceful exit
	if waitCh != nil {
		if p.Config.StopSecs > 0 {
			select {
			case <-waitCh:
				goto done
			case <-time.After(time.Duration(p.Config.StopSecs) * time.Second):
			}
		} else {
			select {
			case <-waitCh:
				goto done
			default:
			}
		}
	}

	// Force kill if still alive
	cmd.Process.Kill()
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
		}
	}

done:
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

func (p *Process) GetState() ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State
}

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

func (p *Process) monitor() {
	err := p.Cmd.Wait()

	p.mu.Lock()
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

// userHz is the kernel's USER_HZ constant (clock ticks per second).
// On almost all Linux architectures this is 100.
const userHz = 100.0

func (p *Process) readProcStats(pid int) {
	// Read VmRSS
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

	// Read process CPU ticks from /proc/pid/stat
	statFile := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statFile)
	if err != nil {
		return
	}
	content := string(data)
	idx := strings.LastIndex(content, ")")
	if idx < 0 || idx+2 >= len(content) {
		return
	}
	rest := strings.Fields(content[idx+2:])
	if len(rest) < 13 {
		return
	}
	var utime, stime int64
	fmt.Sscanf(rest[11], "%d", &utime)
	fmt.Sscanf(rest[12], "%d", &stime)
	cpuTicks := utime + stime

	// Read system-wide CPU ticks from /proc/stat
	sysTicks := readSystemCPUTicks()

	p.mu.Lock()
	now := time.Now()

	if p.prevCPUTicks > 0 && !p.prevCPUTime.IsZero() && sysTicks > p.prevSysTicks {
		deltaProcess := cpuTicks - p.prevCPUTicks
		deltaSystem := sysTicks - p.prevSysTicks
		if deltaSystem > 0 {
			numCPUs := float64(runtime.NumCPU())
			p.CPUUsage = (float64(deltaProcess) / float64(deltaSystem)) * numCPUs * 100.0
		}
	}

	p.prevCPUTicks = cpuTicks
	p.prevCPUTime = now
	p.prevSysTicks = sysTicks

	p.Healthy = p.CPUUsage < 90.0 && p.MemoryUsage < 2*1024*1024*1024
	p.mu.Unlock()
}

// readSystemCPUTicks reads total CPU ticks from /proc/stat.
func readSystemCPUTicks() int64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	// First line: "cpu  user nice system idle iowait irq softirq steal ..."
	var (
		user, nice, system, idle, iowait, irq, softirq, steal int64
	)
	n, _ := fmt.Sscanf(string(data), "cpu %d %d %d %d %d %d %d %d",
		&user, &nice, &system, &idle, &iowait, &irq, &softirq, &steal)
	if n < 4 {
		return 0
	}
	return user + nice + system + idle + iowait + irq + softirq + steal
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

	// Collect initial zero-degree nodes and sort by priority
	queue := make([]string, 0)
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}
	pm.sortByPriority(queue)

	result := make([]string, 0, len(graph))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		nextBatch := make([]string, 0)
		for _, neighbor := range reverseGraph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				nextBatch = append(nextBatch, neighbor)
			}
		}
		pm.sortByPriority(nextBatch)
		queue = append(queue, nextBatch...)
	}

	if len(result) != len(graph) {
		return nil, fmt.Errorf("存在循环依赖")
	}

	return result, nil
}

// sortByPriority sorts a slice of process names by their configured priority.
func (pm *ProcessManager) sortByPriority(names []string) {
	sort.Slice(names, func(i, j int) bool {
		pi, ok := pm.Processes[names[i]]
		if !ok {
			return false
		}
		pj, ok := pm.Processes[names[j]]
		if !ok {
			return false
		}
		return pi.Config.Priority < pj.Config.Priority
	})
}
