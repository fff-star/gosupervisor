package process

import (
	"bufio"
	"context"
	"encoding/json"
	"reflect"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"sort"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/fcgi"
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

// Logf is the package-level log function. Set to t.Logf in tests to silence output.
var Logf = func(format string, args ...any) {
	fmt.Printf(format, args...)
}

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

// ParseSignal returns the syscall.Signal for a signal name string.
func ParseSignal(name string) (syscall.Signal, bool) {
	sig, ok := stopSignals[name]
	return sig, ok
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
	Healthy         bool
	ResourceHealthy bool
	Group           string

	// Restart rate limiting
	restartTimestamps []time.Time

	// Health check tracking
	healthCheckFailures     int
	healthCheckRestartFired bool // suppress repeat health-check restarts until next success
	healthCheckCtx          context.Context
	healthCheckCancel       context.CancelFunc

	// waitCh is closed by monitor() after cmd.Wait() returns.
	// Stop() waits on it instead of calling cmd.Wait() directly.
	waitCh chan struct{}

	// monitorDone is closed by monitor() when it exits, allowing Start() to
	// wait for the old monitor goroutine before starting a new one.
	monitorDone chan struct{}

	// startCtx is cancelled on subsequent Start() calls to signal the
	// previous monitorResources goroutine to exit.
	startCtx    context.Context
	startCancel context.CancelFunc

	// CPU percentage tracking
	prevCPUTicks int64
	prevCPUTime  time.Time

	// System-wide CPU ticks for accurate percentage
	prevSysTicks int64

	// Callback for external systems (metrics, etc.)
	OnHealthCheckFailure func(name string)

	// Resource history ring buffer (60 samples at 5s = 5 minutes)
	ResourceHistory *ResourceHistory

	manager *ProcessManager // back-reference for fcgi socket access
}

type ProcessManager struct {
	mu          sync.RWMutex
	Processes   map[string]*Process
	Logger      *logger.Logger
	fcgiSockets map[string]*fcgi.SocketManager // fcgi socket lifecycle, keyed by base program name
}

func NewProcessManager(logger *logger.Logger) *ProcessManager {
	return &ProcessManager{
		Processes:   make(map[string]*Process),
		Logger:      logger,
		fcgiSockets: make(map[string]*fcgi.SocketManager),
	}
}

func (pm *ProcessManager) AddProcess(cfg *config.ProgramConfig) *Process {
	ctx, cancel := context.WithCancel(context.Background())
	process := &Process{
		Name:            cfg.Name,
		Config:          cfg,
		State:           StateStopped,
		Context:         ctx,
		CancelFunc:      cancel,
		Logger:          pm.Logger,
		Group:           cfg.Group,
		ResourceHistory: NewResourceHistory(60),
		manager:         pm,
	}
	pm.mu.Lock()
	pm.Processes[cfg.Name] = process
	pm.mu.Unlock()
	return process
}

// RangeProcesses calls fn for each process while holding a read lock.
func (pm *ProcessManager) RangeProcesses(fn func(name string, p *Process)) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for name, p := range pm.Processes {
		fn(name, p)
	}
}

// RemoveProcess removes a process from the manager by name.
func (pm *ProcessManager) RemoveProcess(name string) {
	pm.mu.Lock()
	delete(pm.Processes, name)
	pm.mu.Unlock()
}

// Len returns the number of managed processes.
func (pm *ProcessManager) Len() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.Processes)
}

// ReplaceProcesses atomically replaces the entire process map.
func (pm *ProcessManager) ReplaceProcesses(newMap map[string]*Process) {
	pm.mu.Lock()
	pm.Processes = newMap
	pm.mu.Unlock()
}

func (p *Process) Start() error {
	p.mu.Lock()
	if p.State == StateRunning {
		p.mu.Unlock()
		return fmt.Errorf("进程 %s 已经在运行", p.Name)
	}

	// Cancel previous start's context to signal old monitorResources to exit.
	if p.startCancel != nil {
		p.startCancel()
	}

	// Wait for old monitor goroutine to exit before assigning a new waitCh.
	monDone := p.monitorDone
	p.mu.Unlock()

	if monDone != nil {
		<-monDone
	}

	p.mu.Lock()
	// Re-check: another goroutine may have started the process while we waited
	if p.State == StateRunning || p.State == StateStarting || p.State == StateStopping {
		p.mu.Unlock()
		return fmt.Errorf("进程 %s 已经在运行或启动中", p.Name)
	}
	// Old process has already exited (we waited for monDone above).
	// Avoid calling Kill on the stale PID to prevent PID reuse attacks.
	p.Cmd = nil

	p.State = StateStarting
	p.StartRetries++
	if p.PID > 0 {
		p.RestartCount++
	}
	p.LastRestart = time.Now()
	p.Healthy = false
	p.ResourceHealthy = false
	p.healthCheckFailures = 0

	ctx, cancel := context.WithCancel(p.Context)
	p.startCtx = ctx
	p.startCancel = cancel
	var cmd *exec.Cmd
	if !needsShell(p.Config.Command) {
		parts := strings.Fields(p.Config.Command)
		if len(parts) > 0 {
			if _, err := exec.LookPath(parts[0]); err == nil {
				cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
			}
		}
	}
	if cmd == nil {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", p.Config.Command)
	}

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
		uid, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			Logf("警告: 进程 %s 的用户 %s 的 UID '%s' 无效: %v", p.Name, p.Config.User, u.Uid, err)
		}
		gid, err := strconv.ParseUint(u.Gid, 10, 32)
		if err != nil {
			Logf("警告: 进程 %s 的用户 %s 的 GID '%s' 无效: %v", p.Name, p.Config.User, u.Gid, err)
		}

		gidStrs, _ := u.GroupIds()
		var groups []uint32
		for _, gs := range gidStrs {
			g, err := strconv.ParseUint(gs, 10, 32)
			if err != nil {
				Logf("警告: 进程 %s 的用户 %s 的附加组 ID '%s' 无效: %v", p.Name, p.Config.User, gs, err)
			}
			groups = append(groups, uint32(g))
		}

		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid:         uint32(uid),
			Gid:         uint32(gid),
			Groups:      groups,
			NoSetGroups: false,
		}
	}

	var fcgiCleanup func()

	// Attach fcgi socket if configured.
	// Release p.mu before acquiring p.manager.mu to avoid lock-ordering
	// deadlock with RangeProcesses→Snapshot (pm.mu.RLock → p.mu.Lock).
	if p.Config.Socket != "" && p.manager != nil {
		baseName := fcgiBaseName(p.Config.Name)
		mgr := p.manager
		p.mu.Unlock()

		mgr.mu.Lock()
		sm, ok := mgr.fcgiSockets[baseName]
		if !ok {
			sm = fcgi.NewSocketManager(p.Config.Socket, p.Config.SocketMode, p.Config.SocketOwner)
			mgr.fcgiSockets[baseName] = sm
		}
		if !ok {
			// Release mgr.mu for the slow Listen() call, then re-acquire
			// before Attach() to prevent a concurrent Stop() from closing
			// the socket between lookup and the refCount increment.
			mgr.mu.Unlock()
			if err := sm.Listen(); err != nil {
				mgr.mu.Lock()
				delete(mgr.fcgiSockets, baseName)
				mgr.mu.Unlock()
				p.mu.Lock()
				p.State = StateExited
				p.mu.Unlock()
				return fmt.Errorf("fcgi socket listen: %v", err)
			}
			mgr.mu.Lock()
		}

		var err error
		fcgiCleanup, err = sm.Attach(cmd)
		mgr.mu.Unlock()

		if err != nil {
			p.mu.Lock()
			p.State = StateExited
			p.mu.Unlock()
			return fmt.Errorf("fcgi socket attach: %v", err)
		}

		p.mu.Lock()
		// Re-check: a concurrent Stop() may have changed state while p.mu was released.
		if p.State != StateStarting {
			p.mu.Unlock()
			sm.Detach()
			if fcgiCleanup != nil {
				fcgiCleanup()
			}
			return fmt.Errorf("进程 %s 在启动期间被停止", p.Name)
		}
	}

	if p.Logger != nil {
		stdoutWriter, stderrWriter, err := p.Logger.GetProcessLogWriters(p.Name, p.Config)
		if err != nil {
			Logf("警告: 进程 %s 创建日志写入器失败: %v", p.Name, err)
		} else {
			if p.Config.RedirectStdout {
				cmd.Stdout = stdoutWriter
			}
			if p.Config.RedirectStderr {
				cmd.Stderr = stderrWriter
			}
		}
	}

	// Set up stdin from file if configured
	var stdinFile *os.File
	if p.Config.StdinFile != "" {
		f, err := os.Open(p.Config.StdinFile)
		if err != nil {
			fmt.Printf("进程 %s 打开 stdin 文件失败: %v\n", p.Name, err)
		} else {
			cmd.Stdin = f
			stdinFile = f
		}
	}

	p.mu.Unlock()

	// Run pre-start hook
	if p.Config.PreStartScript != "" {
		if err := runHook(p.Config.PreStartScript); err != nil {
			fmt.Printf("进程 %s pre-start 脚本失败: %v\n", p.Name, err)
		}
	}

	// Set umask before fork (process-wide, so serialise via mutex)
	umaskMu.Lock()
	rlimitRestore := p.applyRlimits()
	oldUmask := syscall.Umask(p.Config.Umask)
	if err := cmd.Start(); err != nil {
		syscall.Umask(oldUmask)
		for i := len(rlimitRestore) - 1; i >= 0; i-- {
			rlimitRestore[i]()
		}
		umaskMu.Unlock()
		if stdinFile != nil {
			_ = stdinFile.Close()
		}
		if fcgiCleanup != nil {
			fcgiCleanup()
		}
		p.mu.Lock()
		p.State = StateExited
		p.mu.Unlock()
		return fmt.Errorf("启动进程失败: %v", err)
	}
	syscall.Umask(oldUmask)
	for i := len(rlimitRestore) - 1; i >= 0; i-- {
		rlimitRestore[i]()
	}
	umaskMu.Unlock()

	// Close stdin file in parent — child has its own fd copy
	if stdinFile != nil {
		_ = stdinFile.Close()
		stdinFile = nil
	}

	// Close fcgi dup'd fd in parent — child has its own copy via ExtraFiles
	if fcgiCleanup != nil {
		fcgiCleanup()
		fcgiCleanup = nil
	}

	p.mu.Lock()
	// Re-check: Stop() may have been called while p.mu was released
	// (PreStartScript, umask, rlimits, cmd.Start).
	if p.State != StateStarting {
		p.mu.Unlock()
		cmd.Process.Kill()
		if fcgiCleanup != nil {
			fcgiCleanup()
		}
		if stdinFile != nil {
			stdinFile.Close()
		}
		return fmt.Errorf("进程 %s 在启动期间被停止", p.Name)
	}
	// Create channels only after cmd.Start() succeeds so that
	// a failed Start() does not leak unclosed channels that would
	// block the next Start() on <-monDone.
	p.waitCh = make(chan struct{})
	p.monitorDone = make(chan struct{})
	p.Cmd = cmd
	p.PID = cmd.Process.Pid
	p.StartTime = time.Now()
	p.State = StateRunning
	// Record restart timestamp for rate limiting (only after successful start).
	p.addRestartTimestamp(p.Config.RestartWindowSecs)
	pid := p.PID
	p.mu.Unlock()

	p.sendWebhook(StateRunning, pid, 0)
	RecordEvent(p.Name, EventStart, pid, 0, "started")

	go p.monitor()
	go p.monitorResources()

	// Start health check if configured
	if p.Config.HealthCheckURL != "" {
		p.startHealthCheck()
	}

	// Apply cgroup if configured
	if p.Config.CgroupPath != "" {
		p.applyCgroup(pid)
	}

	return nil
}

func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.Cmd
	cfg := p.Config

	if cmd == nil || cmd.Process == nil {
		p.State = StateStopped
		p.mu.Unlock()
		// Clean up fcgi socket even if process already exited.
		if cfg != nil && cfg.Socket != "" && p.manager != nil {
			baseName := fcgiBaseName(p.Name)
			p.manager.mu.Lock()
			sm, ok := p.manager.fcgiSockets[baseName]
			if ok && sm.Detach() {
				sm.Close()
				delete(p.manager.fcgiSockets, baseName)
			}
			p.manager.mu.Unlock()
		}
		return nil
	}

	if p.State != StateRunning && p.State != StateStopping {
		// Process not running; skip Kill to avoid PID reuse.
		p.State = StateStopped
		p.mu.Unlock()
		// Clean up fcgi socket even if process has already exited.
		if cfg != nil && cfg.Socket != "" && p.manager != nil {
			baseName := fcgiBaseName(p.Name)
			p.manager.mu.Lock()
			sm, ok := p.manager.fcgiSockets[baseName]
			if ok && sm.Detach() {
				sm.Close()
				delete(p.manager.fcgiSockets, baseName)
			}
			p.manager.mu.Unlock()
		}
		return nil
	}

	p.State = StateStopping
	waitCh := p.waitCh
	pidBefore := p.PID
	p.mu.Unlock()

	// Send graceful stop signal
	sig, ok := stopSignals[p.Config.StopSignal]
	if !ok {
		sig = syscall.SIGTERM
	}
	if p.Config.StopAsGroup || p.Config.KillsAsGroup {
		_ = signalProcessGroup(pidBefore, sig)
	} else {
		cmd.Process.Signal(sig)
	}

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

	// Re-check waitCh before force-kill: the process may have exited
	// naturally during the StopSecs wait, avoiding a reused PID.
	if waitCh != nil {
		select {
		case <-waitCh:
			goto done
		default:
		}
	}
	if p.Config.StopAsGroup || p.Config.KillsAsGroup {
		_ = signalProcessGroup(pidBefore, syscall.SIGKILL)
	} else {
		cmd.Process.Kill()
	}
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
		}
	}

done:
	// Now that the process is confirmed dead, cancel contexts to clean up
	// health check and resource monitor goroutines. Doing this after the
	// process exits (rather than before SIGTERM) avoids CommandContext's
	// internal SIGKILL racing with our graceful shutdown.
	p.mu.Lock()
	if p.healthCheckCancel != nil {
		p.healthCheckCancel()
	}
	if p.startCancel != nil {
		p.startCancel()
	}
	p.StopTime = time.Now()
	if p.State == StateStopping {
		p.State = StateStopped
	}
	pid := p.PID
	exitCode := p.ExitCode
	// Clear Cmd and PID after the process is confirmed dead so that no
	// future operation can accidentally use a stale (potentially reused) PID.
	p.Cmd = nil
	p.PID = 0
	p.mu.Unlock()

	// Detach fcgi socket
	if cfg != nil && cfg.Socket != "" && p.manager != nil {
		baseName := fcgiBaseName(p.Name)
		p.manager.mu.Lock()
		sm, ok := p.manager.fcgiSockets[baseName]
		if ok && sm.Detach() {
			sm.Close()
			delete(p.manager.fcgiSockets, baseName)
		}
		p.manager.mu.Unlock()
	}

	p.sendWebhook(StateStopped, pid, exitCode)
	RecordEvent(p.Name, EventStop, pid, exitCode, "stopped")
	return nil
}

func (p *Process) Restart() error {
	p.mu.Lock()
	if p.State == StateFatal {
		p.mu.Unlock()
		return fmt.Errorf("进程 %s 处于FATAL状态，无法重启", p.Name)
	}
	if p.State == StateStopping || p.State == StateStarting {
		p.mu.Unlock()
		return nil // already restarting
	}
	// Atomically transition RUNNING → STOPPING so concurrent Restart() calls skip.
	if p.State == StateRunning {
		p.State = StateStopping
	}
	p.mu.Unlock()
	if err := p.Stop(); err != nil {
		return err
	}
	// If another caller already started the process between Stop and Start,
	// the restart is effectively done — don't fail with "already running".
	p.mu.Lock()
	if p.State == StateRunning || p.State == StateStarting {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	return p.Start()
}

// Signal sends a signal to a running process.
func (p *Process) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Cmd == nil || p.Cmd.Process == nil {
		return fmt.Errorf("process %s is not running", p.Name)
	}
	if p.State != StateRunning {
		return fmt.Errorf("process %s is not running (state=%s)", p.Name, p.State)
	}
	if p.Config.StopAsGroup || p.Config.KillsAsGroup {
		if err := signalProcessGroup(p.Cmd.Process.Pid, sig); err != nil {
			return fmt.Errorf("failed to send signal %s to process group of %s: %v", sig, p.Name, err)
		}
	} else {
		if err := p.Cmd.Process.Signal(sig); err != nil {
			return fmt.Errorf("failed to send signal %s to %s: %v", sig, p.Name, err)
		}
	}
	RecordEvent(p.Name, EventSignal, p.PID, 0, fmt.Sprintf("signal %s", sig))
	return nil
}

func (p *Process) GetState() ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State
}

// SetOnHealthCheckFailure sets the health check failure callback under p.mu
// to prevent data races with the health check goroutine.
func (p *Process) SetOnHealthCheckFailure(fn func(name string)) {
	p.mu.Lock()
	p.OnHealthCheckFailure = fn
	p.mu.Unlock()
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
	FcgiSocket   string `json:"fcgi_socket,omitempty"`
	FcgiRefCount int    `json:"fcgi_refcount,omitempty"`
}

func (p *Process) Snapshot() Snapshot {
	p.mu.Lock()
	healthy := true
	if p.Config.HealthCheckURL != "" {
		healthy = healthy && p.Healthy
	}
	if p.Config.CPUThresholdPercent > 0 || p.Config.MemoryThresholdBytes > 0 {
		healthy = healthy && p.ResourceHealthy
	}
	snap := Snapshot{
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
		Healthy:      healthy,
		Config:       p.Config,
	}
	p.mu.Unlock()

	// Populate fcgi fields from immutable references (no p.mu needed).
	if p.Config != nil && p.Config.Socket != "" && p.manager != nil {
		baseName := fcgiBaseName(p.Config.Name)
		p.manager.mu.RLock()
		if sm, ok := p.manager.fcgiSockets[baseName]; ok {
			snap.FcgiSocket = sm.SocketAddr()
			snap.FcgiRefCount = sm.RefCount()
		}
		p.manager.mu.RUnlock()
	}
	return snap
}

// fcgiBaseName strips the _NN suffix from numprocs-expanded names
// so siblings share one socket. "php_1" → "php", "php" → "php".
func fcgiBaseName(name string) string {
	if idx := strings.LastIndex(name, "_"); idx > 0 {
		rest := name[idx+1:]
		for _, c := range rest {
			if c < '0' || c > '9' {
				return name
			}
		}
		return name[:idx]
	}
	return name
}

func (p *Process) monitor() {
	defer close(p.monitorDone)
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("panic in monitor goroutine for %s: %v\n", p.Name, r)
		}
	}()

	err := p.Cmd.Wait()

	// Capture startCancel under the lock to avoid a data race with the next
	// Start() call, which writes p.startCancel concurrently.
	p.mu.Lock()
	cancel := p.startCancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	// Compute exit code before closing waitCh so Stop() sees the correct value.
	var exitCode int
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	p.mu.Lock()
	if p.waitCh != nil {
		close(p.waitCh)
		p.waitCh = nil
	}

	p.StopTime = time.Now()
	p.ExitCode = exitCode
	if p.State != StateStopping {
		p.State = StateExited
	}

	// Capture webhook data before releasing lock.
	isExited := p.State == StateExited
	pid := p.PID
	p.mu.Unlock()

	if isExited {
		p.sendWebhook(StateExited, pid, exitCode)
		RecordEvent(p.Name, EventExit, pid, exitCode, "exited")
	}

	// Run post-stop hook
	if p.Config.PostStopScript != "" {
		if hookErr := runHook(p.Config.PostStopScript); hookErr != nil {
			fmt.Printf("进程 %s post-stop 脚本失败: %v\n", p.Name, hookErr)
		}
	}
}

func (p *Process) monitorResources() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("panic in monitorResources goroutine for %s: %v\n", p.Name, r)
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	p.mu.Lock()
	ctx := p.startCtx
	p.mu.Unlock()

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
		case <-ctx.Done():
			return
		}
	}
}


func (p *Process) readProcStats(pid int) {
	var rss uint64

	// Read VmRSS
	statusFile := fmt.Sprintf("/proc/%d/status", pid)
	if f, err := os.Open(statusFile); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if v, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						rss = uint64(v) * 1024
					}
				}
				break
			}
		}
		f.Close()
	}

	// Read process CPU ticks from /proc/pid/stat before acquiring the lock,
	// then update MemoryUsage, CPUUsage, ResourceHealthy, and ResourceHistory
	// under a single lock so Snapshot() sees a consistent view.
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
	p.MemoryUsage = rss

	now := time.Now()

	if p.prevCPUTicks > 0 && !p.prevCPUTime.IsZero() && sysTicks > p.prevSysTicks {
		deltaProcess := cpuTicks - p.prevCPUTicks
		deltaSystem := sysTicks - p.prevSysTicks
		if deltaSystem > 0 {
			numCPUs := float64(runtime.NumCPU())
			p.CPUUsage = (float64(deltaProcess) / float64(deltaSystem)) * numCPUs * 100.0
		}
	}

	firstSample := p.prevCPUTicks == 0

	p.prevCPUTicks = cpuTicks
	p.prevCPUTime = now
	p.prevSysTicks = sysTicks

	// Negative threshold means disabled (set to -1 in config). Zero means
	// unset (config defaults fill in the production values).
	cpuOK := p.Config.CPUThresholdPercent < 0 || p.CPUUsage < p.Config.CPUThresholdPercent
	memOK := p.Config.MemoryThresholdBytes < 0 || p.MemoryUsage < uint64(p.Config.MemoryThresholdBytes)
	p.ResourceHealthy = cpuOK && memOK

	// Skip first sample since CPU delta is not yet meaningful
	if p.ResourceHistory != nil && !firstSample {
		p.ResourceHistory.Push(ResourceSample{
			Timestamp: now,
			CPU:       p.CPUUsage,
			Memory:    p.MemoryUsage,
		})
	}
	p.mu.Unlock()
}

type rlimitEntry struct {
	resource int
	value    uint64
}

// applyRlimits applies per-process resource limits before fork.
// Returns restore functions that must be called (in reverse order) after fork.
func (p *Process) applyRlimits() (restore []func()) {
	entries := []rlimitEntry{
		{syscall.RLIMIT_AS, p.Config.RlimitAs},
		{syscall.RLIMIT_CORE, p.Config.RlimitCore},
		{syscall.RLIMIT_CPU, p.Config.RlimitCpu},
		{syscall.RLIMIT_DATA, p.Config.RlimitData},
		{syscall.RLIMIT_FSIZE, p.Config.RlimitFsize},
		{syscall.RLIMIT_NOFILE, p.Config.RlimitNofile},
		{6 /* RLIMIT_NPROC - not exported by Go syscall on Linux */, p.Config.RlimitNproc},
		{syscall.RLIMIT_STACK, p.Config.RlimitStack},
	}
	for _, e := range entries {
		if e.value == 0 {
			continue
		}
		var old syscall.Rlimit
		if err := syscall.Getrlimit(e.resource, &old); err != nil {
			continue
		}
		if err := syscall.Setrlimit(e.resource, &syscall.Rlimit{Cur: e.value, Max: e.value}); err != nil {
			continue
		}
		r := e.resource
		o := old
		restore = append(restore, func() {
			syscall.Setrlimit(r, &o) // nolint: errcheck
		})
	}
	return restore
}

// needsShell reports whether cmd contains shell metacharacters and requires /bin/sh -c.
func needsShell(cmd string) bool {
	return strings.ContainsAny(cmd, "|;&$`(){}<>*?!~#'\"\\")
}

// runHook executes a shell script hook.
func runHook(script string) error {
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// startHealthCheck starts a health check goroutine for the process.
func (p *Process) startHealthCheck() {
	p.mu.Lock()
	// Cancel previous health check if any
	if p.healthCheckCancel != nil {
		p.healthCheckCancel()
	}
	ctx, cancel := context.WithCancel(p.Context)
	p.healthCheckCtx = ctx
	p.healthCheckCancel = cancel
	p.mu.Unlock()

	go p.runHealthCheck(ctx)
}

func (p *Process) runHealthCheck(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("panic in health check goroutine for %s: %v\n", p.Name, r)
		}
	}()
	interval := time.Duration(p.Config.HealthCheckInterval) * time.Second
	timeout := time.Duration(p.Config.HealthCheckTimeout) * time.Second
	threshold := p.Config.HealthCheckUnhealthyThreshold
	url := p.Config.HealthCheckURL

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			if p.State != StateRunning {
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()

			ok := checkHealth(url, timeout)

			p.mu.Lock()
			// Bail out if the process was stopped/restarted while checkHealth was in flight.
			// Must check under the lock to avoid TOCTOU: if ctx is cancelled between the
			// check and Lock(), a stale goroutine would mutate p.Healthy on a dead process.
			select {
			case <-ctx.Done():
				p.mu.Unlock()
				return
			default:
			}
			wasHealthy := p.Healthy
			var healthRestore, healthFail bool
			var eventPID int
			var hcFailCb func(string)

			if ok {
				p.healthCheckFailures = 0
				p.Healthy = true
				p.healthCheckRestartFired = false
				if !wasHealthy {
					healthRestore = true
					eventPID = p.PID
				}
			} else {
				p.healthCheckFailures++
				hcFailCb = p.OnHealthCheckFailure
				if p.healthCheckFailures >= threshold {
					p.Healthy = false
					if wasHealthy {
						healthFail = true
						eventPID = p.PID
					}
				}
			}
			p.mu.Unlock()

			if healthRestore {
				RecordEvent(p.Name, EventHealthRestore, eventPID, 0, "health restored")
				p.sendWebhook(StateRunning, eventPID, 0)
			}
			if healthFail {
				RecordEvent(p.Name, EventHealthFail, eventPID, 0, "health check failed")
				p.sendWebhook(StateRunning, eventPID, 0)
			}
			if hcFailCb != nil {
				hcFailCb(p.Name)
			}
		}
	}
}

func checkHealth(url string, timeout time.Duration) bool {
	if addr, ok := strings.CutPrefix(url, "tcp://"); ok {
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// applyCgroup writes the process PID to cgroup.procs for cgroup v2.
func (p *Process) applyCgroup(pid int) {
	if pid <= 0 {
		return
	}
	cgroupProcs := p.Config.CgroupPath + "/cgroup.procs"
	if err := os.WriteFile(cgroupProcs, []byte(strconv.Itoa(pid)), 0644); err != nil {
		fmt.Printf("进程 %s 加入 cgroup 失败: %v\n", p.Name, err)
	}
}

// addRestartTimestamp records a restart and prunes old entries outside the window.
func (p *Process) addRestartTimestamp(windowSecs int) {
	if windowSecs <= 0 {
		windowSecs = 60
	}
	now := time.Now()
	p.restartTimestamps = append(p.restartTimestamps, now)
	cutoff := now.Add(-time.Duration(windowSecs) * time.Second)
	n := 0
	for _, ts := range p.restartTimestamps {
		if ts.After(cutoff) {
			p.restartTimestamps[n] = ts
			n++
		}
	}
	p.restartTimestamps = p.restartTimestamps[:n]
}

// restartRateExceeded checks if the restart rate limit has been exceeded.
func (p *Process) restartRateExceeded() bool {
	maxCount := p.Config.RestartMaxCount
	if maxCount <= 0 {
		return false
	}
	window := p.Config.RestartWindowSecs
	if window <= 0 {
		window = 60
	}
	cutoff := time.Now().Add(-time.Duration(window) * time.Second)
	count := 0
	for _, ts := range p.restartTimestamps {
		if ts.After(cutoff) {
			count++
		}
	}
	return count >= maxCount
}

// shouldRestartOnExitCode checks if the process should restart based on exit code policy.
func (p *Process) shouldRestartOnExitCode() bool {
	if len(p.Config.RestartCodes) > 0 {
		return slices.Contains(p.Config.RestartCodes, p.ExitCode)
	}
	if len(p.Config.NoRestartCodes) > 0 {
		return !slices.Contains(p.Config.NoRestartCodes, p.ExitCode)
	}
	return true
}

// StartGroup starts all processes in a group (including autostart=false).
func (pm *ProcessManager) StartGroup(group string) (started, failed []string) {
	ordered := pm.topologicalSortForGroup(group)
	for _, name := range ordered {
		p := pm.GetProcess(name)
		if p == nil {
			continue
		}
		if err := p.Start(); err != nil {
			fmt.Printf("启动进程 %s 失败: %v\n", p.Name, err)
			failed = append(failed, name)
		} else {
			started = append(started, p.Name)
		}
	}
	return
}

// StopGroup stops all processes in a group (reverse dependency order within group).
func (pm *ProcessManager) StopGroup(group string) []string {
	ordered := pm.topologicalSortForGroup(group)
	var stopped []string
	// Reverse order: stop dependents before dependencies
	for i := len(ordered) - 1; i >= 0; i-- {
		p := pm.GetProcess(ordered[i])
		if p == nil {
			continue
		}
		if err := p.Stop(); err != nil {
			fmt.Printf("停止进程 %s 失败: %v\n", p.Name, err)
		} else {
			stopped = append(stopped, p.Name)
		}
	}
	return stopped
}

// PersistentState holds the state that can be saved/restored across restarts.
type PersistentState struct {
	ProcessName  string       `json:"name"`
	State        ProcessState `json:"state"`
	PID          int          `json:"pid"`
	ExitCode     int          `json:"exitCode"`
	RestartCount int          `json:"restartCount"`
	StartRetries int          `json:"startRetries"`
	LastRestart  time.Time    `json:"lastRestart"`
}

// SaveState saves the current state of all processes to a JSON file.
func (pm *ProcessManager) SaveState(path string) error {
	var states []PersistentState
	pm.RangeProcesses(func(name string, p *Process) {
		s := p.Snapshot()
		states = append(states, PersistentState{
			ProcessName:  s.Name,
			State:        s.State,
			PID:          s.PID,
			ExitCode:     s.ExitCode,
			RestartCount: s.RestartCount,
			StartRetries: s.StartRetries,
			LastRestart:  s.LastRestart,
		})
	})
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化进程状态失败: %v", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	// fsync the temp file before rename to ensure crash safety.
	if f, err := os.Open(tmpPath); err == nil {
		_ = f.Sync()
		f.Close()
	}
	return os.Rename(tmpPath, path)
}

// RestoreState reads process state from a JSON file. It only restores metadata,
// not running processes.
func (pm *ProcessManager) RestoreState(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var states []PersistentState
	if err := json.Unmarshal(data, &states); err != nil {
		return fmt.Errorf("反序列化进程状态失败: %v", err)
	}
	for _, s := range states {
		if p := pm.GetProcess(s.ProcessName); p != nil {
			p.mu.Lock()
			p.RestartCount = s.RestartCount
			p.StartRetries = s.StartRetries
			if !s.LastRestart.IsZero() {
				p.LastRestart = s.LastRestart
			}
			p.mu.Unlock()
		}
	}
	return nil
}

// sendWebhook POSTs process state change to the configured webhook URL.
// Supports configurable retries with exponential backoff.
// Caller must capture state, pid, and exitCode under lock before calling.
func (p *Process) sendWebhook(state ProcessState, pid int, exitCode int) {
	if p.Config.WebhookURL == "" {
		return
	}
	timeout := time.Duration(p.Config.WebhookTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	retries := max(p.Config.WebhookRetries, 0)

	payload := struct {
		Name      string `json:"name"`
		Group     string `json:"group"`
		State     string `json:"state"`
		PID       int    `json:"pid"`
		ExitCode  int    `json:"exitCode"`
		Timestamp string `json:"timestamp"`
	}{
		Name:      p.Name,
		Group:     p.Config.Group,
		State:     string(state),
		PID:       pid,
		ExitCode:  exitCode,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		Logf("进程 %s webhook payload 序列化失败: %v\n", p.Name, err)
		return
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			backoff := min(time.Duration(1<<(attempt-1))*time.Second, 30*time.Second)
			time.Sleep(backoff)
		}
		client := &http.Client{Timeout: timeout}
		resp, err := client.Post(p.Config.WebhookURL, "application/json",
			strings.NewReader(string(body)))
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		return
	}
	Logf("进程 %s webhook 发送失败 (已重试 %d 次): %v\n", p.Name, retries, lastErr)
}

// RestartGroup restarts all processes in a group (dependency order).
func (pm *ProcessManager) RestartGroup(group string) []string {
	ordered := pm.topologicalSortForGroup(group)
	var restarted []string
	for _, name := range ordered {
		p := pm.GetProcess(name)
		if p == nil {
			continue
		}
		if err := p.Restart(); err != nil {
			fmt.Printf("重启进程 %s 失败: %v\n", p.Name, err)
		} else {
			restarted = append(restarted, p.Name)
		}
	}
	return restarted
}

// topologicalSortForGroup returns process names in dependency order for a specific group.
func (pm *ProcessManager) topologicalSortForGroup(group string) []string {
	// Build a subgraph for the group
	pm.mu.RLock()
	inGroup := make(map[string]bool)
	allNames := make(map[string]bool)
	for name, p := range pm.Processes {
		if p.Config.Group == group {
			inGroup[name] = true
		}
		allNames[name] = true
	}

	// Calculate in-degree counting only deps within the group
	inDegree := make(map[string]int)
	deps := make(map[string][]string)
	for name := range inGroup {
		p := pm.Processes[name]
		for _, dep := range p.Config.DependsOn {
			if inGroup[dep] {
				inDegree[name]++
				deps[dep] = append(deps[dep], name)
			}
		}
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
	}
	pm.mu.RUnlock()

	// Kahn's algorithm
	var queue []string
	for name := range inGroup {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // deterministic order for same priority

	var result []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, name)
		for _, dependent := range deps[name] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
				sort.Strings(queue)
			}
		}
	}

	// Append any remaining group members not reached (cycles or missing deps)
	for name := range inGroup {
		if !slices.Contains(result, name) {
			result = append(result, name)
		}
	}

	return result
}

// readSystemCPUTicks reads total CPU ticks from /proc/stat.
func readSystemCPUTicks() int64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	// First line: "cpu  user nice system idle iowait irq softirq steal ..."
	var (
		user, nice, system, idle, iowait, irq, softirq, steal int64
	)
	n, _ := fmt.Sscanf(scanner.Text(), "cpu %d %d %d %d %d %d %d %d",
		&user, &nice, &system, &idle, &iowait, &irq, &softirq, &steal)
	if n < 4 {
		return 0
	}
	return user + nice + system + idle + iowait + irq + softirq + steal
}

func (pm *ProcessManager) GetProcess(name string) *Process {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.Processes[name]
}

// CompareConfigs compares the current process map against new configs and returns
// lists of added, removed, and modified process names.
func (pm *ProcessManager) CompareConfigs(newConfigs map[string]*config.ProgramConfig) (added, removed, modified []string) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for name := range pm.Processes {
		if _, exists := newConfigs[name]; !exists {
			removed = append(removed, name)
		}
	}
	for name, newCfg := range newConfigs {
		if oldProc, exists := pm.Processes[name]; !exists {
			added = append(added, name)
		} else if !reflect.DeepEqual(oldProc.Config, newCfg) {
			modified = append(modified, name)
		}
	}
	return
}

func (pm *ProcessManager) StartAll() []string {
	pm.mu.RLock()
	dependencyGraph := make(map[string][]string, len(pm.Processes))
	for _, process := range pm.Processes {
		dependencyGraph[process.Name] = process.Config.DependsOn
	}
	pm.mu.RUnlock()

	orderedProcesses, err := pm.topologicalSort(dependencyGraph)
	if err != nil {
		fmt.Printf("解析进程依赖关系失败: %v\n", err)
		pm.mu.RLock()
		for _, process := range pm.Processes {
			orderedProcesses = append(orderedProcesses, process.Name)
		}
		pm.mu.RUnlock()
	}

	var failed []string
	for _, name := range orderedProcesses {
		pm.mu.RLock()
		process := pm.Processes[name]
		pm.mu.RUnlock()
		if process == nil {
			continue
		}
		if err := process.Start(); err != nil {
			fmt.Printf("启动进程 %s 失败: %v\n", name, err)
			failed = append(failed, name)
		}
	}
	return failed
}

func (pm *ProcessManager) StopAll() {
	pm.mu.RLock()
	dependencyGraph := make(map[string][]string, len(pm.Processes))
	for _, process := range pm.Processes {
		dependencyGraph[process.Name] = process.Config.DependsOn
	}
	pm.mu.RUnlock()

	orderedProcesses, err := pm.topologicalSort(dependencyGraph)
	if err != nil {
		fmt.Printf("解析进程依赖关系失败: %v\n", err)
		pm.mu.RLock()
		for _, process := range pm.Processes {
			orderedProcesses = append(orderedProcesses, process.Name)
		}
		pm.mu.RUnlock()
	}

	for i := len(orderedProcesses) - 1; i >= 0; i-- {
		name := orderedProcesses[i]
		pm.mu.RLock()
		process := pm.Processes[name]
		pm.mu.RUnlock()
		if process == nil {
			continue
		}
		state := process.GetState()
		if state == StateRunning || state == StateStarting || state == StateExited || state == StateFatal {
			process.Stop()
		}
	}
}

// SortByDeps returns names in dependency order (dependencies before dependents).
func (pm *ProcessManager) SortByDeps(graph map[string][]string) ([]string, error) {
	return pm.topologicalSort(graph)
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
	pm.mu.RLock()
	defer pm.mu.RUnlock()
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
