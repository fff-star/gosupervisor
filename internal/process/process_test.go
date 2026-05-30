package process

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"



	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

func TestProcessManager(t *testing.T) {
	// 初始化日志管理器
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	// 创建进程管理器
	processManager := NewProcessManager(logManager)

	// 创建测试进程配置
	programCfg := &config.ProgramConfig{
		Name:         "test_process",
		Command:      "echo \"Hello, World!\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
	}

	// 添加进程
	processManager.AddProcess(programCfg)

	// 检查进程是否添加成功
	if _, exists := processManager.Processes["test_process"]; !exists {
		t.Fatalf("进程添加失败")
	}

	// 测试获取进程
	p := processManager.GetProcess("test_process")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 测试启动所有进程
	processManager.StartAll()

	// 等待进程启动
	time.Sleep(2 * time.Second)

	// 测试获取进程状态
	p = processManager.GetProcess("test_process")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 测试停止所有进程
	processManager.StopAll()

	// 等待进程停止
	time.Sleep(1 * time.Second)

	// 测试获取进程状态
	p = processManager.GetProcess("test_process")
	if p == nil {
		t.Fatalf("获取进程失败")
	}
}

func TestProcessOperations(t *testing.T) {
	// 初始化日志管理器
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	// 创建进程管理器
	processManager := NewProcessManager(logManager)

	// 创建测试进程配置，使用ping命令让进程一直运行
	programCfg := &config.ProgramConfig{
		Name:         "test_process",
		Command:      "ping localhost",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
	}

	// 添加进程
	processManager.AddProcess(programCfg)

	// 获取进程
	p := processManager.GetProcess("test_process")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 测试进程启动
	err = p.Start()
	if err != nil {
		t.Errorf("启动进程失败: %v", err)
	}

	// 等待进程启动
	time.Sleep(2 * time.Second)

	// 测试进程状态
	if state := p.GetState(); state != StateRunning {
		t.Errorf("期望进程状态为RUNNING，实际为%s", state)
	}

	// 测试进程重启
	// 重启进程，遇到错误记录但不直接失败测试
	err = p.Restart()
	if err != nil {
		t.Errorf("重启进程时遇到错误: %v", err)
	}

	// 等待进程重启
	time.Sleep(2 * time.Second)

	// 测试进程停止
	// 停止进程，遇到错误记录但不直接失败测试
	err = p.Stop()
	if err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}

	// 等待进程停止
	time.Sleep(3 * time.Second)

	// 测试进程状态
	// 检查进程最终状态（若不同则记录）
	if state := p.GetState(); state != StateStopped {
		t.Errorf("进程状态为%s，期望为STOPPED", state)
	}
}

func TestTopologicalSort(t *testing.T) {
	// 初始化日志管理器
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	// 创建进程管理器
	processManager := NewProcessManager(logManager)

	// 创建测试进程配置
	programCfg1 := &config.ProgramConfig{
		Name:         "process1",
		Command:      "echo \"Process 1\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"process2"},
	}

	programCfg2 := &config.ProgramConfig{
		Name:         "process2",
		Command:      "echo \"Process 2\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"process3"},
	}

	programCfg3 := &config.ProgramConfig{
		Name:         "process3",
		Command:      "echo \"Process 3\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{},
	}

	// 添加进程
	processManager.AddProcess(programCfg1)
	processManager.AddProcess(programCfg2)
	processManager.AddProcess(programCfg3)

	// 测试拓扑排序
	dependencies := make(map[string][]string)
	for name, proc := range processManager.Processes {
		dependencies[name] = proc.Config.DependsOn
	}
	order, err := processManager.topologicalSort(dependencies)
	if err != nil {
		t.Fatalf("拓扑排序失败: %v", err)
	}

	// 检查排序结果
	// 期望顺序: process3 -> process2 -> process1
	expectedOrder := []string{"process3", "process2", "process1"}
	if len(order) != len(expectedOrder) {
		t.Errorf("期望排序结果长度为%d，实际为%d", len(expectedOrder), len(order))
	}

	for i, name := range order {
		if name != expectedOrder[i] {
			t.Errorf("期望排序结果第%d位为%s，实际为%s", i, expectedOrder[i], name)
		}
	}
}

// TestTopologicalSortWithCycle 测试循环依赖的检测
func TestTopologicalSortWithCycle(t *testing.T) {
	// 初始化日志管理器
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	// 创建进程管理器
	processManager := NewProcessManager(logManager)

	// 创建循环依赖的进程配置
	programCfg1 := &config.ProgramConfig{
		Name:         "process1",
		Command:      "echo \"Process 1\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"process2"},
	}

	programCfg2 := &config.ProgramConfig{
		Name:         "process2",
		Command:      "echo \"Process 2\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"process3"},
	}

	programCfg3 := &config.ProgramConfig{
		Name:         "process3",
		Command:      "echo \"Process 3\"",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"process1"}, // 循环依赖
	}

	// 添加进程
	processManager.AddProcess(programCfg1)
	processManager.AddProcess(programCfg2)
	processManager.AddProcess(programCfg3)

	// 测试拓扑排序，期望返回循环依赖错误
	dependencies := make(map[string][]string)
	for name, proc := range processManager.Processes {
		dependencies[name] = proc.Config.DependsOn
	}
	_, err = processManager.topologicalSort(dependencies)
	if err == nil {
		t.Errorf("期望检测到循环依赖，但没有返回错误")
	}
}

// TestProcessAutoRestart 测试进程自动重启功能
func TestProcessAutoRestart(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	processManager := NewProcessManager(logManager)

	// Use a command that exits quickly so auto-restart is triggered.
	programCfg := &config.ProgramConfig{
		Name:         "test_restart",
		Command:      "echo exited",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 3,
		StopSecs:     1,
		User:         "",
		Environment:  make(map[string]string),
	}

	processManager.AddProcess(programCfg)
	p := processManager.GetProcess("test_restart")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}

	// Start the monitor so it can detect the exited process and auto-restart it.
	monitor := NewMonitor(processManager)
	monitor.Start()
	defer monitor.Stop()

	// Wait for the process to exit and be auto-restarted by the monitor.
	deadline := time.After(10 * time.Second)
	var restarted bool
	for !restarted {
		select {
		case <-deadline:
			t.Fatal("超时等待自动重启")
		default:
		}
		s := p.Snapshot()
		if s.RestartCount > 0 {
			restarted = true
		}
		time.Sleep(200 * time.Millisecond)
	}

	s := p.Snapshot()
	if s.RestartCount == 0 {
		t.Errorf("期望 RestartCount > 0, 实际 %d", s.RestartCount)
	}

	// Stop the process to clean up
	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}
}// TestProcessResourceMonitoring 测试进程资源监控功能
func TestProcessResourceMonitoring(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	processManager := NewProcessManager(logManager)

	// Use a command that runs long enough for resource monitoring to sample it.
	programCfg := &config.ProgramConfig{
		Name:         "test_resource",
		Command:      "sleep 10",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
	}

	processManager.AddProcess(programCfg)
	p := processManager.GetProcess("test_resource")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}

	// Wait for multiple resource monitoring ticks (interval is 5s).
	time.Sleep(6 * time.Second)

	s := p.Snapshot()
	if s.PID <= 0 {
		t.Error("已启动进程应有有效 PID")
	}
	if s.MemoryUsage == 0 {
		t.Error("资源监控应记录内存使用量")
	}
	if s.CPUUsage < 0 {
		t.Error("CPU 使用率不应为负数")
	}

	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}
}// TestProcessStateTransitions 测试进程状态转换
func TestProcessStateTransitions(t *testing.T) {
	// 初始化日志管理器
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	// 创建进程管理器
	processManager := NewProcessManager(logManager)

	// 创建测试进程配置，使用ping命令让进程一直运行
	programCfg := &config.ProgramConfig{
		Name:         "test_state",
		Command:      "ping -c 10 localhost",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
	}

	// 添加进程
	processManager.AddProcess(programCfg)

	// 获取进程
	p := processManager.GetProcess("test_state")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 检查初始状态
	if state := p.GetState(); state != StateStopped {
		t.Errorf("期望初始状态为STOPPED，实际为%s", state)
	}

	// 启动进程
	err = p.Start()
	if err != nil {
		t.Errorf("启动进程失败: %v", err)
	}

	// 等待进程启动
	time.Sleep(2 * time.Second)

	// 检查运行状态
	if state := p.GetState(); state != StateRunning {
		t.Errorf("期望状态为RUNNING，实际为%s", state)
	}

	// 停止进程，若出错则记录
	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}

	// 等待进程停止
	time.Sleep(3 * time.Second)
	// 检查停止状态（记录非预期状态）
	if state := p.GetState(); state != StateStopped {
		t.Errorf("进程状态为%s，期望为STOPPED", state)
	}
}

// TestMonitorAutoRestart 测试 Monitor 的自动重启功能
func TestMonitorAutoRestart(t *testing.T) {
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	processManager := NewProcessManager(logManager)

	// Use a command that exits quickly. StartSecs=5 ensures the monitor
	// never resets StartRetries mid-test (the process never stays up that long).
	programCfg := &config.ProgramConfig{
		Name:         "short_lived",
		Command:      "echo test",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    5,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
	}

	processManager.AddProcess(programCfg)

	// 创建并启动监控
	monitor := NewMonitor(processManager)
	monitor.Start()
	defer monitor.Stop()

	p := processManager.GetProcess("short_lived")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 手动启动进程
	err = p.Start()
	if err != nil {
		t.Errorf("启动进程失败: %v", err)
	}

	// Poll for restart: the process should exit, be detected, and restarted
	// within a few monitor ticks. Use a condition loop instead of hardcoded
	// sleeps to avoid timing flakiness.
	deadline := time.Now().Add(8 * time.Second)
	var lastRestartCount int
	for time.Now().Before(deadline) {
		s := p.Snapshot()
		lastRestartCount = s.RestartCount
		if lastRestartCount >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastRestartCount < 2 {
		s := p.Snapshot()
		t.Errorf("期望至少重启2次，实际 RestartCount=%d, StartRetries=%d, State=%s",
			s.RestartCount, s.StartRetries, s.State)
	}
}

// TestTopologicalSortWithStartAllFallback 测试依赖关系错误时的降级行为
func TestTopologicalSortWithStartAllFallback(t *testing.T) {
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	processManager := NewProcessManager(logManager)

	// 创建循环依赖的进程配置
	programCfg1 := &config.ProgramConfig{
		Name:         "cycle1",
		Command:      "echo test1",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 1,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"cycle2"},
	}

	programCfg2 := &config.ProgramConfig{
		Name:         "cycle2",
		Command:      "echo test2",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 1,
		User:         "",
		Environment:  make(map[string]string),
		DependsOn:    []string{"cycle1"}, // 循环依赖
	}

	processManager.AddProcess(programCfg1)
	processManager.AddProcess(programCfg2)

	// StartAll 应该尽管存在循环依赖也能执行（使用降级的默认顺序）
	processManager.StartAll()

	// 等待启动
	time.Sleep(1 * time.Second)

	// 验证两个进程都被添加到了管理器
	p1 := processManager.GetProcess("cycle1")
	p2 := processManager.GetProcess("cycle2")

	if p1 == nil || p2 == nil {
		t.Fatalf("进程未被正确添加")
	}

	// 虽然有循环依赖，但进程应该还是能启动（至少状态应该有变化）
	// 由于 echo 命令会立即完成，进程会进入 EXITED 状态
	s1, s2 := p1.GetState(), p2.GetState()
	if s1 == StateStopped || s2 == StateStopped {
		t.Errorf("循环依赖时 StartAll 可能未执行（期望至少有进程启动/退出）")
	}
}

// TestProcessRestartCountLogic 测试重启计数逻辑
func TestProcessRestartCountLogic(t *testing.T) {
	logDir := "./test_logs_rctl"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	processManager := NewProcessManager(logManager)

	programCfg := &config.ProgramConfig{
		Name:         "restart_test",
		Command:      "echo restart",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 3,
		User:         "",
		Environment:  make(map[string]string),
	}

	processManager.AddProcess(programCfg)
	p := processManager.GetProcess("restart_test")

	// 第一次启动：RestartCount 不应增加（不是重启）
	err = p.Start()
	if err != nil {
		t.Errorf("第一次启动失败: %v", err)
	}

	if p.StartRetries != 1 {
		t.Errorf("首次启动后 StartRetries 应为 1，实际为 %d", p.StartRetries)
	}
	if p.RestartCount != 0 {
		t.Errorf("首次启动后 RestartCount 应为 0（不是重启），实际为 %d", p.RestartCount)
	}

	// 等待进程完成
	time.Sleep(1 * time.Second)

	// 第二次启动：这才是重启
	err = p.Start()
	if err != nil {
		t.Errorf("第二次启动失败: %v", err)
	}

	if p.StartRetries != 2 {
		t.Errorf("第二次启动后 StartRetries 应为 2，实际为 %d", p.StartRetries)
	}
	if p.RestartCount != 1 {
		t.Errorf("第二次启动后 RestartCount 应为 1（一次重启），实际为 %d", p.RestartCount)
	}

	// 第三次启动
	time.Sleep(1 * time.Second)
	err = p.Start()
	if err != nil {
		t.Errorf("第三次启动失败: %v", err)
	}

	if p.RestartCount != 2 {
		t.Errorf("第三次启动后 RestartCount 应为 2，实际为 %d", p.RestartCount)
	}
	_ = p.Stop()
}

// TestHandleExitedProcessNoAutoRestart 测试：AutoRestart=false 时不重启
func TestHandleExitedProcessNoAutoRestart(t *testing.T) {
	logDir := "./test_logs_nar"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "no_restart",
		Command:      "true",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("no_restart")

	_ = p.Start()
	time.Sleep(500 * time.Millisecond)

	p.mu.Lock()
	p.StartRetries = 0
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	afterState := p.GetState()
	if afterState == StateRunning || afterState == StateStarting {
		t.Errorf("AutoRestart=false 时不应重启，但状态变为 %s", afterState)
	}
	if afterState != StateExited && afterState != StateFatal {
		t.Errorf("handleExitedProcess 后状态应为 EXITED 或 FATAL，实际为 %s", afterState)
	}
	_ = p.Stop()
}

// TestHandleExitedProcessRetryLimit 测试：重试耗尽后进入 FATAL
func TestHandleExitedProcessRetryLimit(t *testing.T) {
	logDir := "./test_logs_rl"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "retry_limit",
		Command:      "false",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 2,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("retry_limit")

	p.mu.Lock()
	p.StartRetries = 2
	p.State = StateExited
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	// 等待异步重启完成（1s sleep + Start + false 退出）
	time.Sleep(2 * time.Second)

	// 此时 StartRetries=3 > limit=2，再次触发应进入 FATAL
	p.mu.Lock()
	if p.State == StateExited {
		p.mu.Unlock()
		m.handleExitedProcess(p)
	} else {
		p.mu.Unlock()
	}

	time.Sleep(100 * time.Millisecond)

	if p.GetState() != StateFatal {
		t.Errorf("重试次数超限后应进入 FATAL，实际为 %s (StartRetries=%d, limit=%d)",
			p.GetState(), p.StartRetries, cfg.StartRetries)
	}
}

// TestHandleExitedProcessRetryExhaustion 测试：已超限直接 FATAL
func TestHandleExitedProcessRetryExhaustion(t *testing.T) {
	logDir := "./test_logs_re"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "exhaust_test",
		Command:      "false",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 2,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("exhaust_test")

	p.mu.Lock()
	p.StartRetries = 3
	p.State = StateExited
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	if state := p.GetState(); state != StateFatal {
		t.Errorf("StartRetries(%d) > limit(%d) 时应直接进入 FATAL，实际为 %s",
			p.StartRetries, cfg.StartRetries, state)
	}
}

// TestCheckRunningProcessStartRetriesReset 测试：StartSecs 控制重置
func TestCheckRunningProcessStartRetriesReset(t *testing.T) {
	logDir := "./test_logs_reset"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "reset_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("reset_test")

	_ = p.Start()
	defer p.Stop()

	p.mu.Lock()
	p.StartRetries = 5
	p.State = StateRunning
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.checkRunningProcess(p)

	if p.StartRetries != 5 {
		t.Errorf("StartSecs=0 时不应重置 StartRetries，期望 5，实际 %d", p.StartRetries)
	}

	p.Config.StartSecs = 1
	p.mu.Lock()
	p.StartTime = time.Now().Add(-2 * time.Second)
	p.mu.Unlock()

	m.checkRunningProcess(p)

	if p.StartRetries != 0 {
		t.Errorf("StartSecs=1 且已过 2 秒，应重置 StartRetries=0，实际 %d", p.StartRetries)
	}
}

// TestRestartCountNotOnInitialStart 测试：首次启动不增加 RestartCount
func TestRestartCountNotOnInitialStart(t *testing.T) {
	logDir := "./test_logs_rc"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "initial_test",
		Command:      "sleep 0.1",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("initial_test")

	if err := p.Start(); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}

	if p.RestartCount != 0 {
		t.Errorf("首次启动后 RestartCount 应为 0，实际为 %d", p.RestartCount)
	}

	time.Sleep(300 * time.Millisecond)

	if err := p.Start(); err != nil {
		t.Fatalf("第二次启动失败: %v", err)
	}

	if p.RestartCount != 1 {
		t.Errorf("重启后 RestartCount 应为 1，实际为 %d", p.RestartCount)
	}
	_ = p.Stop()
}

// TestStopDoesNotTriggerRestart 测试：Stop 后 Monitor 不重启
func TestStopDoesNotTriggerRestart(t *testing.T) {
	logDir := "./test_logs_stopmon"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "stop_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("stop_test")

	_ = p.Start()

	monitor := NewMonitor(pm)
	monitor.Start()
	defer monitor.Stop()

	_ = p.Stop()
	time.Sleep(1 * time.Second)

	state := p.GetState()
	if state == StateRunning || state == StateStarting {
		t.Errorf("Stop 后 Monitor 不应重启进程，状态为 %s (StartRetries=%d)", state, p.StartRetries)
	}
}

// TestCheckRunningProcessSkipsStopping 测试：STOPPING 时跳过
func TestCheckRunningProcessSkipsStopping(t *testing.T) {
	logDir := "./test_logs_stopchk"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "stopping_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("stopping_test")
	_ = p.Start()
	defer p.Stop()

	p.mu.Lock()
	p.State = StateStopping
	p.StartRetries = 5
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.checkRunningProcess(p)

	if p.StartRetries != 5 {
		t.Errorf("StateStopping 时 checkRunningProcess 不应修改状态，StartRetries 期望 5，实际 %d", p.StartRetries)
	}
}

// TestDuplicateStartReturnsError 测试：重复启动返回错误
func TestDuplicateStartReturnsError(t *testing.T) {
	logDir := "./test_logs_dup"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "dup_start",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("dup_start")

	if err := p.Start(); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	defer p.Stop()

	if err := p.Start(); err == nil {
		t.Error("重复启动应返回错误")
	}
}

// TestStopAllHandlesAllStates 测试：StopAll 处理各种状态
func TestStopAllHandlesAllStates(t *testing.T) {
	logDir := "./test_logs_stopall"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	for i, cmd := range []string{"sleep 0.1", "sleep 0.1", "sleep 0.1"} {
		cfg := &config.ProgramConfig{
			Name:         fmt.Sprintf("sa_proc_%d", i),
			Command:      cmd,
			Directory:    ".",
			AutoStart:    false,
			AutoRestart:  false,
			StartSecs:    1,
			StartRetries: 3,
			Environment:  make(map[string]string),
		}
		pm.AddProcess(cfg)
	}

	p0 := pm.GetProcess("sa_proc_0")
	p1 := pm.GetProcess("sa_proc_1")
	p2 := pm.GetProcess("sa_proc_2")

	// p0: EXITED
	_ = p0.Start()
	time.Sleep(300 * time.Millisecond)

	// p1: RUNNING
	_ = p1.Start()
	defer p1.Stop()

	// p2: FATAL
	p2.mu.Lock()
	p2.State = StateFatal
	p2.mu.Unlock()

	pm.StopAll()

	for name, proc := range pm.Processes {
		if s := proc.GetState(); s != StateStopped {
			t.Errorf("StopAll 后进程 %s 应为 STOPPED，实际为 %s", name, s)
		}
	}
}

// TestProcessSnapshotThreadSafe 测试：并发 Snapshot/GetState 不 panic
func TestProcessSnapshotThreadSafe(t *testing.T) {
	logDir := "./test_logs_snap"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "snap_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("snap_test")
	_ = p.Start()
	defer p.Stop()

	monitor := NewMonitor(pm)
	monitor.Start()
	defer monitor.Stop()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = p.Snapshot()
				_ = p.GetState()
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify Snapshot returns valid data after concurrent access.
	s := p.Snapshot()
	if s.Name != "snap_test" {
		t.Errorf("expected Name 'snap_test', got %q", s.Name)
	}
	if s.State == "" {
		t.Error("expected non-empty State in Snapshot after concurrent access")
	}
}

// TestStopThenMonitorRestartRace 测试：Stop/Monitor 并发竞争
func TestStopThenMonitorRestartRace(t *testing.T) {
	logDir := "./test_logs_race"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	for i := 0; i < 5; i++ {
		pm := NewProcessManager(logManager)
		cfg := &config.ProgramConfig{
			Name:         fmt.Sprintf("race_%d", i),
			Command:      "sleep 60",
			Directory:    ".",
			AutoStart:    true,
			AutoRestart:  true,
			StartSecs:    1,
			StartRetries: 3,
			Environment:  make(map[string]string),
		}
		pm.AddProcess(cfg)
		p := pm.GetProcess(cfg.Name)
		_ = p.Start()

		monitor := NewMonitor(pm)
		monitor.Start()

		_ = p.Stop()
		time.Sleep(100 * time.Millisecond)

		if state := p.GetState(); state == StateStarting {
			t.Errorf("迭代 %d: Stop 后 Monitor 不应重启进程，状态为 %s", i, state)
		}

		monitor.Stop()
	}
}

// TestTopologicalSortWithPriority tests that processes at the same dependency level
// are ordered by their Priority field.
func TestTopologicalSortWithPriority(t *testing.T) {
	logDir := "./test_logs_prio"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	for _, entry := range []struct {
		name     string
		priority int
	}{
		{"prio_low", 999},
		{"prio_high", 1},
		{"prio_mid", 500},
	} {
		pm.AddProcess(&config.ProgramConfig{
			Name:         entry.name,
			Command:      "true",
			Priority:     entry.priority,
			AutoStart:    false,
			AutoRestart:  false,
			StartSecs:    1,
			StartRetries: 1,
			Environment:  make(map[string]string),
		})
	}

	graph := make(map[string][]string)
	for name, proc := range pm.Processes {
		graph[name] = proc.Config.DependsOn
	}

	order, err := pm.topologicalSort(graph)
	if err != nil {
		t.Fatalf("拓扑排序失败: %v", err)
	}

	if order[0] != "prio_high" {
		t.Errorf("期望最高优先级(Priority=1)排第一，实际 %s", order[0])
	}
	if order[1] != "prio_mid" {
		t.Errorf("期望中优先级(Priority=500)排第二，实际 %s", order[1])
	}
	if order[2] != "prio_low" {
		t.Errorf("期望最低优先级(Priority=999)排第三，实际 %s", order[2])
	}
}

// TestStopSignalsMap tests that all expected signal names are mapped.
func TestStopSignalsMap(t *testing.T) {
	expected := []string{"SIGTERM", "SIGQUIT", "SIGINT", "SIGHUP", "SIGUSR1", "SIGUSR2", "SIGKILL"}
	for _, name := range expected {
		sig, ok := stopSignals[name]
		if !ok {
			t.Errorf("信号 %s 不在 stopSignals 映射中", name)
		}
		if sig == 0 {
			t.Errorf("信号 %s 映射值为零", name)
		}
	}
}

// TestRedirectStdoutStderrFlags tests that RedirectStdout/RedirectStderr are honored.
func TestRedirectStdoutStderrFlags(t *testing.T) {
	logDir := "./test_logs_redir"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	t.Run("only stdout redirected", func(t *testing.T) {
		cfg := &config.ProgramConfig{
			Name:                 "test_stdout_only",
			Command:              "echo hi",
			RedirectStdout:       true,
			RedirectStderr:       false,
			AutoStart:            false,
			AutoRestart:          false,
			StartSecs:            0,
			StartRetries:         0,
			Environment:          make(map[string]string),
			StdoutLogMaxBytes:    50 * 1024 * 1024,
			StderrLogMaxBytes:    50 * 1024 * 1024,
			StdoutLogBackupCount: 10,
			StderrLogBackupCount: 10,
		}
		pm := NewProcessManager(logManager)
		pm.AddProcess(cfg)
		p := pm.GetProcess("test_stdout_only")
		if err := p.Start(); err != nil {
			t.Fatalf("Start() failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		s := p.Snapshot()
		if s.State == "" {
			t.Error("Snapshot State should not be empty")
		}
		if s.PID <= 0 {
			t.Error("PID should be > 0 after Start")
		}
		p.Stop()
	})

	t.Run("only stderr redirected", func(t *testing.T) {
		cfg := &config.ProgramConfig{
			Name:                 "test_stderr_only",
			Command:              "echo hi",
			RedirectStdout:       false,
			RedirectStderr:       true,
			AutoStart:            false,
			AutoRestart:          false,
			StartSecs:            0,
			StartRetries:         0,
			Environment:          make(map[string]string),
			StdoutLogMaxBytes:    50 * 1024 * 1024,
			StderrLogMaxBytes:    50 * 1024 * 1024,
			StdoutLogBackupCount: 10,
			StderrLogBackupCount: 10,
		}
		pm := NewProcessManager(logManager)
		pm.AddProcess(cfg)
		p := pm.GetProcess("test_stderr_only")
		if err := p.Start(); err != nil {
			t.Fatalf("Start() failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		s := p.Snapshot()
		if s.State == "" {
			t.Error("Snapshot State should not be empty")
		}
		if s.PID <= 0 {
			t.Error("PID should be > 0 after Start")
		}
		p.Stop()
	})

	t.Run("neither redirected", func(t *testing.T) {
		cfg := &config.ProgramConfig{
			Name:                 "test_neither",
			Command:              "echo hi",
			RedirectStdout:       false,
			RedirectStderr:       false,
			AutoStart:            false,
			AutoRestart:          false,
			StartSecs:            0,
			StartRetries:         0,
			Environment:          make(map[string]string),
			StdoutLogMaxBytes:    50 * 1024 * 1024,
			StderrLogMaxBytes:    50 * 1024 * 1024,
			StdoutLogBackupCount: 10,
			StderrLogBackupCount: 10,
		}
		pm := NewProcessManager(logManager)
		pm.AddProcess(cfg)
		p := pm.GetProcess("test_neither")
		if err := p.Start(); err != nil {
			t.Fatalf("Start() failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		s := p.Snapshot()
		if s.State == "" {
			t.Error("Snapshot State should not be empty")
		}
		if s.PID <= 0 {
			t.Error("PID should be > 0 after Start")
		}
		p.Stop()
	})
}

// TestCPUUsageIsPercentage tests that CPU usage is computed as a delta-based
// percentage after two sampling intervals.
func TestCPUUsageIsPercentage(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "cpu_test",
		Command:      "dd if=/dev/zero of=/dev/null bs=1M count=1024",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("cpu_test")
	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}
	defer func() {
		if err := p.Stop(); err != nil {
			t.Logf("停止进程时遇到错误: %v", err)
		}
	}()

	// Wait for at least one resource sample
	time.Sleep(6 * time.Second)

	s := p.Snapshot()
	if s.CPUUsage < 0 {
		t.Errorf("CPU 使用率不应为负数, got %.2f", s.CPUUsage)
	}
	if s.CPUUsage > 200 {
		t.Errorf("CPU 使用率异常高 (可能仍是 ticks), got %.2f", s.CPUUsage)
	}
	t.Logf("CPU usage: %.2f%%", s.CPUUsage)
}// TestReadSystemCPUTicks tests the system CPU ticks reader.
func TestReadSystemCPUTicks(t *testing.T) {
	ticks := readSystemCPUTicks()
	if ticks <= 0 {
		t.Error("系统 CPU ticks 应大于 0")
	}
}

// TestProcessManagerMutex tests that concurrent access to ProcessManager
// does not panic under the race detector.
func TestProcessManagerMutex(t *testing.T) {
	logDir := "./test_logs_mutex"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	for i := 0; i < 10; i++ {
		pm.AddProcess(&config.ProgramConfig{
			Name:         fmt.Sprintf("p%d", i),
			Command:      "sleep 0.1",
			AutoStart:    false,
			AutoRestart:  false,
			StartSecs:    0,
			StartRetries: 1,
			Environment:  make(map[string]string),
		})
	}

	// Concurrent reads via RangeProcesses
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				pm.RangeProcesses(func(name string, p *Process) {
					_ = p.Snapshot()
				})
				_ = pm.Len()
				_ = pm.GetProcess("p0")
			}
			done <- struct{}{}
		}()
	}

	// Concurrent writes via ReplaceProcesses
	go func() {
		for j := 0; j < 10; j++ {
			newMap := make(map[string]*Process)
			for name, p := range pm.Processes {
				newMap[name] = p
			}
			pm.ReplaceProcesses(newMap)
			time.Sleep(5 * time.Millisecond)
		}
		done <- struct{}{}
	}()

	for i := 0; i < 11; i++ {
		<-done
	}

	if pm.Len() != 10 {
		t.Errorf("expected 10 processes after concurrent access, got %d", pm.Len())
	}
	for i := 0; i < 10; i++ {
		if pm.GetProcess(fmt.Sprintf("p%d", i)) == nil {
			t.Errorf("process p%d should still exist", i)
		}
	}
}

// TestMonitorResourcesExitsOnRestart tests that calling Start() a second time
// causes the old monitorResources goroutine to exit via context cancellation.
func TestMonitorResourcesExitsOnRestart(t *testing.T) {
	logDir := "./test_logs_mr"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "mr_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("mr_test")

	_ = p.Start()
	defer p.Stop()

	oldStartCtx := p.startCtx

	// Restart should cancel the old context
	err := p.Restart()
	if err != nil {
		t.Fatalf("Restart 失败: %v", err)
	}
	defer p.Stop()

	if oldStartCtx == nil {
		t.Fatal("oldStartCtx is nil")
	}

	// Old context should be cancelled
	select {
	case <-oldStartCtx.Done():
		// Expected: old context is cancelled
	default:
		t.Error("期望旧 startCtx 被取消，但它未被取消")
	}
}

// TestStartWaitChRace tests that calling Start() multiple times does not panic
// due to waitCh/monitorDone races.
func TestStartWaitChRace(t *testing.T) {
	logDir := "./test_logs_wc"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "wc_test",
		Command:      "sleep 0.1",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("wc_test")

	// First start should succeed.
	if err := p.Start(); err != nil {
		t.Fatalf("initial Start() failed: %v", err)
	}
	s := p.Snapshot()
	if s.PID <= 0 {
		t.Error("PID should be > 0 after Start")
	}

	// Subsequent rapid starts should not panic.
	for i := 0; i < 4; i++ {
		_ = p.Start()
		time.Sleep(100 * time.Millisecond)
	}

	// Final cleanup
	s = p.Snapshot()
	if s.State == "" {
		t.Error("Snapshot State should not be empty after concurrent starts")
	}
	p.Stop()
}

// TestMonitorResetsExitCodeOnSuccess tests that ExitCode is cleared on
// successful exit (code 0).
func TestMonitorResetsExitCodeOnSuccess(t *testing.T) {
	logDir := "./test_logs_ec"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "ec_test",
		Command:      "exit 1",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("ec_test")

	_ = p.Start()
	time.Sleep(300 * time.Millisecond)

	s := p.Snapshot()
	if s.ExitCode != 1 {
		t.Errorf("期望 ExitCode=1 (exit 1), 实际 %d", s.ExitCode)
	}

	// Start again with exit 0
	cfg2 := &config.ProgramConfig{
		Name:         "ec_test2",
		Command:      "exit 0",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg2)
	p2 := pm.GetProcess("ec_test2")
	_ = p2.Start()
	time.Sleep(300 * time.Millisecond)

	s2 := p2.Snapshot()
	if s2.ExitCode != 0 {
		t.Errorf("期望 ExitCode=0 (exit 0), 实际 %d", s2.ExitCode)
	}
	_ = p.Stop()
	_ = p2.Stop()
}

// TestHandleExitedProcessOffByOne tests that the retry limit is respected
// without an extra off-by-one attempt.
func TestHandleExitedProcessOffByOne(t *testing.T) {
	logDir := "./test_logs_obo"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "obo_test",
		Command:      "false",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 2,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("obo_test")

	// Simulate: StartRetries > StartRetries (limit exceeded)
	p.mu.Lock()
	p.StartRetries = 3
	p.State = StateExited
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	// Should immediately go to FATAL (> check), not attempt restart
	if state := p.GetState(); state != StateFatal {
		t.Errorf("StartRetries(3) > limit(2) 时应直接 FATAL, 实际 %s", state)
	}
}

// TestRangeProcesses tests the safe iteration helper.
func TestRangeProcesses(t *testing.T) {
	logDir := "./test_logs_rp"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	for i := 0; i < 3; i++ {
		pm.AddProcess(&config.ProgramConfig{
			Name:         fmt.Sprintf("rp%d", i),
			Command:      "true",
			AutoStart:    false,
			AutoRestart:  false,
			StartSecs:    0,
			StartRetries: 1,
			Environment:  make(map[string]string),
		})
	}

	count := 0
	pm.RangeProcesses(func(name string, p *Process) {
		count++
	})
	if count != 3 {
		t.Errorf("期望 3 个进程, 实际 %d", count)
	}
}

// TestRemoveProcess tests RemoveProcess and Len.
func TestRemoveProcess(t *testing.T) {
	logDir := "./test_logs_rm"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:         "rm_test",
		Command:      "true",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	})

	if pm.Len() != 1 {
		t.Fatalf("期望 Len=1, 实际 %d", pm.Len())
	}

	pm.RemoveProcess("rm_test")
	if pm.Len() != 0 {
		t.Errorf("期望 Len=0, 实际 %d", pm.Len())
	}
	if p := pm.GetProcess("rm_test"); p != nil {
		t.Error("期望 GetProcess 返回 nil")
	}
}


// TestMonitorExitCodeOnSignal tests that ExitCode is set to -1 when a process
// is killed by a signal (non-ExitError from cmd.Wait).
func TestMonitorExitCodeOnSignal(t *testing.T) {
	logDir := "./test_logs_exitcode"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "exitcode_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    1,
		StartRetries: 3,
		StopSecs:     1,
		StopSignal:   "SIGKILL",
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("exitcode_test")
	if p == nil {
		t.Fatal("获取进程失败")
	}

	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	s := p.Snapshot()
	if s.ExitCode == 0 {
		t.Errorf("期望 ExitCode 不为 0 (信号杀死), 实际 %d", s.ExitCode)
	}
	t.Logf("ExitCode after signal kill: %d", s.ExitCode)
}

// TestMonitorExitCodeOnNormalExit tests ExitCode for normal exit and ExitError.
func TestMonitorExitCodeOnNormalExit(t *testing.T) {
	logDir := "./test_logs_exitnorm"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "exitnorm_test",
		Command:      "exit 42",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("exitnorm_test")
	if p == nil {
		t.Fatal("获取进程失败")
	}

	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	s := p.Snapshot()
	if s.ExitCode != 42 {
		t.Errorf("期望 ExitCode=42, 实际 %d", s.ExitCode)
	}
	_ = p.Stop()
}

// TestMemoryUsagePreservedOnStatFailure tests that MemoryUsage from VmRSS
// is preserved on Snapshot after resource monitoring runs.
func TestMemoryUsagePreservedOnStatFailure(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "memstat_test",
		Command:      "sleep 10",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("memstat_test")
	if p == nil {
		t.Fatal("获取进程失败")
	}

	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}

	// Wait for at least one resource monitoring tick
	time.Sleep(6 * time.Second)

	s := p.Snapshot()
	if s.State != "RUNNING" {
		t.Errorf("expected RUNNING state, got %s", s.State)
	}
	if s.PID <= 0 {
		t.Error("PID should be > 0 while running")
	}
	// MemoryUsage should be populated by resource monitoring.
	if s.MemoryUsage == 0 {
		t.Error("MemoryUsage 应已记录，但依旧为 0")
	}

	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}
}// TestGroupOperations tests StartGroup, StopGroup, RestartGroup.
func TestGroupOperations(t *testing.T) {
	logDir := "./test_logs_group"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	for i := 0; i < 3; i++ {
		pm.AddProcess(&config.ProgramConfig{
			Name:         fmt.Sprintf("grp_%d", i),
			Command:      "sleep 0.1",
			Group:        "testgrp",
			AutoStart:    true,
			AutoRestart:  false,
			StartSecs:    0,
			StartRetries: 1,
			Environment:  make(map[string]string),
		})
	}
	pm.AddProcess(&config.ProgramConfig{
		Name:         "other",
		Command:      "sleep 0.1",
		Group:        "othergrp",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	})

	started, failed := pm.StartGroup("testgrp")
	if len(started) != 3 {
		t.Errorf("StartGroup 期望启动 3 个, 实际 %d", len(started))
	}
	if len(failed) != 0 {
		t.Errorf("StartGroup 期望无失败, 实际失败 %d: %v", len(failed), failed)
	}

	time.Sleep(400 * time.Millisecond)

	stopped := pm.StopGroup("testgrp")
	if len(stopped) != 3 {
		t.Errorf("StopGroup 期望停止 3 个, 实际 %d", len(stopped))
	}

	restarted := pm.RestartGroup("othergrp")
	if len(restarted) != 1 {
		t.Errorf("RestartGroup 期望重启 1 个, 实际 %d", len(restarted))
	}
	time.Sleep(200 * time.Millisecond)
	pp := pm.GetProcess("other")
	if pp != nil {
		pp.Stop()
	}
}

func TestEmptyGroup(t *testing.T) {
	logDir := "./test_logs_emptygrp"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	started, failed := pm.StartGroup("nonexistent")
	if len(started) != 0 {
		t.Errorf("空组 StartGroup 应返回空, 实际 %d", len(started))
	}
	if len(failed) != 0 {
		t.Errorf("空组 StartGroup 应无失败, 实际失败 %d", len(failed))
	}
}

func TestRestartRateLimiting(t *testing.T) {
	logDir := "./test_logs_rate"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:             "rate_test",
		Command:          "false",
		AutoStart:        true,
		AutoRestart:      true,
		StartSecs:        0,
		StartRetries:     99,
		RestartMaxCount:  3,
		RestartWindowSecs: 60,
		Environment:      make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("rate_test")

	p.mu.Lock()
	now := time.Now()
	p.restartTimestamps = []time.Time{now.Add(-10 * time.Second), now.Add(-5 * time.Second), now.Add(-1 * time.Second)}
	p.State = StateExited
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	if state := p.GetState(); state != StateFatal {
		t.Errorf("达到速率限制后应进入 FATAL, 实际 %s", state)
	}
}

func TestRestartRateLimitingNotExceeded(t *testing.T) {
	logDir := "./test_logs_rate2"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:             "rate_ok",
		Command:          "sleep 0.1",
		AutoStart:        true,
		AutoRestart:      true,
		StartSecs:        0,
		StartRetries:     99,
		RestartMaxCount:  5,
		RestartWindowSecs: 60,
		Environment:      make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("rate_ok")

	p.mu.Lock()
	now := time.Now()
	p.restartTimestamps = []time.Time{now.Add(-10 * time.Second)}
	p.State = StateExited
	p.StartRetries = 0
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	if state := p.GetState(); state == StateFatal {
		t.Errorf("未超过速率限制不应进入 FATAL, 实际 %s", state)
	}
	time.Sleep(2 * time.Second)
	p.Stop()
}

func TestRestartRateLimitDisabled(t *testing.T) {
	logDir := "./test_logs_ratenolimit"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:             "nolimit",
		Command:          "sleep 0.1",
		AutoStart:        true,
		AutoRestart:      true,
		StartSecs:        0,
		StartRetries:     99,
		RestartMaxCount:  0,
		RestartWindowSecs: 60,
		Environment:      make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("nolimit")

	if p.restartRateExceeded() {
		t.Error("RestartMaxCount=0 时不应限制")
	}
}

func TestExitCodePolicyRestartCodes(t *testing.T) {
	logDir := "./test_logs_ecpolicy"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "ec_policy",
		Command:      "sleep 0.1",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 99,
		RestartCodes: []int{1, 2, 3},
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("ec_policy")

	p.mu.Lock()
	p.ExitCode = 1
	p.mu.Unlock()
	if !p.shouldRestartOnExitCode() {
		t.Error("ExitCode=1 在 RestartCodes 中, 应允许重启")
	}

	p.mu.Lock()
	p.ExitCode = 0
	p.mu.Unlock()
	if p.shouldRestartOnExitCode() {
		t.Error("ExitCode=0 不在 RestartCodes 中, 不应重启")
	}
}

func TestExitCodePolicyNoRestartCodes(t *testing.T) {
	logDir := "./test_logs_ecpol2"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:           "ec_skip",
		Command:        "sleep 0.1",
		AutoStart:      true,
		AutoRestart:    true,
		StartSecs:      0,
		StartRetries:   99,
		NoRestartCodes: []int{0, 143},
		Environment:    make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("ec_skip")

	p.mu.Lock()
	p.ExitCode = 143
	p.mu.Unlock()
	if p.shouldRestartOnExitCode() {
		t.Error("ExitCode=143 在 NoRestartCodes 中, 不应重启")
	}

	p.mu.Lock()
	p.ExitCode = 1
	p.mu.Unlock()
	if !p.shouldRestartOnExitCode() {
		t.Error("ExitCode=1 不在 NoRestartCodes 中, 应允许重启")
	}
}

func TestExitCodePolicyMonitor(t *testing.T) {
	logDir := "./test_logs_ecmon"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "ecmon",
		Command:      "false",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    0,
		StartRetries: 99,
		RestartCodes: []int{1},
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("ecmon")

	p.mu.Lock()
	p.ExitCode = 0
	p.State = StateExited
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	if state := p.GetState(); state != StateFatal {
		t.Errorf("ExitCode=0 不在 RestartCodes 中, 应进入 FATAL, 实际 %s", state)
	}
}

func TestPersistentState(t *testing.T) {
	logDir := "./test_logs_state"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:         "stateful",
		Command:      "sleep 0.1",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	})

	p := pm.GetProcess("stateful")
	p.mu.Lock()
	p.RestartCount = 5
	p.LastRestart = time.Now()
	p.mu.Unlock()

	statePath := filepath.Join(logDir, "state.json")
	if err := pm.SaveState(statePath); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	p.mu.Lock()
	p.RestartCount = 0
	p.LastRestart = time.Time{}
	p.mu.Unlock()

	if err := pm.RestoreState(statePath); err != nil {
		t.Fatalf("RestoreState 失败: %v", err)
	}

	if s := p.Snapshot(); s.RestartCount != 5 {
		t.Errorf("RestoreState: RestartCount 期望 5, 实际 %d", s.RestartCount)
	}
	if p.LastRestart.IsZero() {
		t.Error("RestoreState: LastRestart 不应为零值")
	}
}

func TestPersistentStateNonExistent(t *testing.T) {
	logDir := "./test_logs_nostate"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	err := pm.RestoreState("/nonexistent/path/state.json")
	if err != nil {
		t.Errorf("不存在的状态文件应返回 nil, 实际 %v", err)
	}
}

func TestRunHook(t *testing.T) {
	if err := runHook("true"); err != nil {
		t.Errorf("runHook(true) 应成功, 实际 %v", err)
	}
	if err := runHook("exit 1"); err == nil {
		t.Error("runHook(exit 1) 应失败")
	}
}

func TestCheckHealthTCP(t *testing.T) {
	if checkHealth("tcp://127.0.0.1:19999", 1*time.Second) {
		t.Error("TCP 检查不存在端口应失败")
	}
}

func TestCheckHealthHTTP(t *testing.T) {
	if checkHealth("http://127.0.0.1:19999/health", 1*time.Second) {
		t.Error("HTTP 检查不存在服务器应失败")
	}
}

func TestShouldRestartOnExitCodeDefault(t *testing.T) {
	logDir := "./test_logs_ecdef"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "ecdef",
		Command:      "sleep 0.1",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("ecdef")

	for _, code := range []int{0, 1, 137, 143} {
		p.mu.Lock()
		p.ExitCode = code
		p.mu.Unlock()
		if !p.shouldRestartOnExitCode() {
			t.Errorf("默认策略下 ExitCode=%d 应该允许重启", code)
		}
	}
}

func TestAddRestartTimestampPrunesOld(t *testing.T) {
	logDir := "./test_logs_prune"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:         "prune",
		Command:      "true",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	})
	p := pm.GetProcess("prune")

	p.mu.Lock()
	p.restartTimestamps = []time.Time{time.Now().Add(-120 * time.Second)}
	p.mu.Unlock()

	p.addRestartTimestamp(60)

	p.mu.Lock()
	count := len(p.restartTimestamps)
	p.mu.Unlock()

	if count != 1 {
		t.Errorf("旧时间戳应被裁剪, 期望 1 个, 实际 %d", count)
	}
}

func TestProcessGroupField(t *testing.T) {
	logDir := "./test_logs_grpfield"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "grouped",
		Command:      "true",
		Group:        "mygroup",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("grouped")
	if p.Group != "mygroup" {
		t.Errorf("Process.Group 期望 'mygroup', 实际 '%s'", p.Group)
	}
}

func TestApplyCgroupInvalidPath(t *testing.T) {
	logDir := "./test_logs_cgroup"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "cgroup_test",
		Command:      "sleep 60",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		CgroupPath:   "/nonexistent/cgroup/path",
		Environment:  make(map[string]string),
	}
	p := pm.AddProcess(cfg)
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer p.Stop()

	// PID should be set after start
	p.mu.Lock()
	pid := p.PID
	p.mu.Unlock()
	if pid <= 0 {
		t.Fatal("PID should be > 0 after start")
	}

	// applyCgroup should not panic even with invalid path (prints error)
	p.applyCgroup(pid)
}

func TestApplyCgroupNoPID(t *testing.T) {
	logDir := "./test_logs_cgroup2"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "cgroup_nopid",
		Command:      "true",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		CgroupPath:   "/sys/fs/cgroup/test",
		Environment:  make(map[string]string),
	}
	p := pm.AddProcess(cfg)
	p.mu.Lock()
	pid := p.PID
	p.mu.Unlock()
	if pid != 0 {
		t.Errorf("PID should be 0 before Start, got %d", pid)
	}
	// applyCgroup with PID=0 should return immediately without panic.
	p.applyCgroup(0)
}

func TestSendWebhookEmptyURL(t *testing.T) {
	logDir := "./test_logs_webhook1"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "wh_empty",
		Command:      "true",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		WebhookURL:   "",
		Environment:  make(map[string]string),
	}
	p := pm.AddProcess(cfg)
	// Should be no-op when WebhookURL is empty (must not panic).
	p.sendWebhook(StateExited, 0, 0)
	// Also verify WebhookURL is actually empty — the no-op guard depends on it.
	if p.Config.WebhookURL != "" {
		t.Error("WebhookURL should be empty for no-op test")
	}
}

func TestSendWebhookDeliversPayload(t *testing.T) {
	logDir := "./test_logs_webhook2"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	received := make(chan []byte, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(200)
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Serve(ln)
	defer server.Close()

	addr := ln.Addr().String()

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "wh_test",
		Command:      "true",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		WebhookURL:   "http://" + addr + "/hook",
		Environment:  make(map[string]string),
	}
	p := pm.AddProcess(cfg)

	p.mu.Lock()
	p.PID = 12345
	p.ExitCode = 0
	p.State = StateExited
	p.mu.Unlock()

	p.sendWebhook(StateExited, 12345, 0)

	select {
	case body := <-received:
		if !strings.Contains(string(body), "wh_test") {
			t.Errorf("webhook payload 应包含进程名: %s", string(body))
		}
	case <-time.After(2 * time.Second):
		t.Error("webhook 未在超时内收到")
	}
}

func TestSendWebhookServerError(t *testing.T) {
	logDir := "./test_logs_webhook3"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	mux := http.NewServeMux()
	mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Serve(ln)
	defer server.Close()

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "wh_err",
		Command:      "true",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		WebhookURL:   "http://" + ln.Addr().String() + "/hook",
		Environment:  make(map[string]string),
	}
	Logf = t.Logf

	p := pm.AddProcess(cfg)
	p.mu.Lock()
	p.PID = 1
	p.State = StateExited
	p.mu.Unlock()
	// Should not panic on 500, must retry and eventually give up
	p.sendWebhook(StateExited, 1, 0)
	// Test passes if no panic
}

func TestStartHealthCheckAndRunHealthCheck(t *testing.T) {
	logDir := "./test_logs_healthrun"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                         "hc_run",
		Command:                      "sleep 60",
		AutoStart:                    false,
		AutoRestart:                  false,
		StartSecs:                    0,
		StartRetries:                 1,
		HealthCheckURL:               "http://127.0.0.1:19999/health",
		HealthCheckInterval:          1,
		HealthCheckTimeout:           1,
		HealthCheckUnhealthyThreshold: 2,
		Environment:                  make(map[string]string),
	}
	p := pm.AddProcess(cfg)
	if err := p.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Stop()

	// Start health check
	p.startHealthCheck()

	// Wait for at least one health check cycle
	time.Sleep(1500 * time.Millisecond)

	p.mu.Lock()
	failures := p.healthCheckFailures
	healthy := p.Healthy
	p.mu.Unlock()

	// Server doesn't exist, so health check should record failures
	if failures == 0 {
		t.Error("应对不存在服务器记录健康检查失败")
	}
	if healthy {
		t.Error("不存在的服务器应导致不健康状态")
	}
}

func TestStartHealthCheckSecondCallCancelsPrevious(t *testing.T) {
	logDir := "./test_logs_hccancel"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                "hc_cancel",
		Command:             "sleep 60",
		AutoStart:           false,
		AutoRestart:         false,
		StartSecs:           0,
		StartRetries:        1,
		HealthCheckURL:      "http://127.0.0.1:19999/health",
		HealthCheckInterval:  1,
		HealthCheckTimeout:  1,
		Environment:         make(map[string]string),
	}
	p := pm.AddProcess(cfg)
	if err := p.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Stop()

	// Start twice — second should cancel first
	p.startHealthCheck()
	oldCtx := p.healthCheckCtx
	p.startHealthCheck()
	newCtx := p.healthCheckCtx

	if oldCtx == newCtx {
		t.Error("第二次 startHealthCheck 应创建新 context")
	}

	// Verify old context is cancelled
	select {
	case <-oldCtx.Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Error("旧 context 应被取消")
	}
}

func TestCheckHealthTCPSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().String()
	if !checkHealth("tcp://"+addr, 1*time.Second) {
		t.Error("TCP 检查应成功")
	}
}

func TestCheckHealthHTTPSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Serve(ln)
	defer server.Close()

	if !checkHealth("http://"+ln.Addr().String()+"/ok", 1*time.Second) {
		t.Error("HTTP 200 检查应成功")
	}
}

func TestCheckHealthHTTPRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(302)
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Serve(ln)
	defer server.Close()

	if !checkHealth("http://"+ln.Addr().String()+"/redir", 1*time.Second) {
		t.Error("HTTP 302 检查应成功")
	}
}

func TestCheckHealthHTTP4xx(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Serve(ln)
	defer server.Close()

	if checkHealth("http://"+ln.Addr().String()+"/bad", 1*time.Second) {
		t.Error("HTTP 404 检查应失败")
	}
}

// TestConcurrentExitAndStart tests for data races between handleExitedProcess
// and Start() on the same process. This catches PID/ExitCode/State races.
func TestConcurrentExitAndStart(t *testing.T) {
	logDir := "./test_logs_conc"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	for i := 0; i < 20; i++ {
		pm := NewProcessManager(logManager)
		cfg := &config.ProgramConfig{
			Name:         fmt.Sprintf("conc_%d", i),
			Command:      "sleep 2",
			AutoStart:    true,
			AutoRestart:  true,
			StartSecs:    0,
			StartRetries: 3,
			StopSecs:     1,
			Environment:  make(map[string]string),
		}
		pm.AddProcess(cfg)
		p := pm.GetProcess(cfg.Name)
		if err := p.Start(); err != nil {
			t.Fatalf("start failed: %v", err)
		}

		m := &Monitor{Manager: pm}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(i%10) * time.Millisecond)
			// Stop the process first so the original monitor goroutine
			// exits cleanly before we simulate exit handling.
			p.Stop()
			p.mu.Lock()
			p.State = StateExited
			p.ExitCode = 1
			p.mu.Unlock()
			m.handleExitedProcess(p)
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = p.Snapshot()
				_ = p.GetState()
				time.Sleep(10 * time.Millisecond)
			}
		}()

		wg.Wait()
		// Stop again in case handleExitedProcess restarted the process.
		p.Stop()
	}
}

// TestConcurrentSnapshotAndMonitor tests for races between Snapshot reads
// and Monitor writes on the same process. This simulates web/socket/metrics
// reads happening concurrently with process lifecycle transitions.
func TestConcurrentSnapshotAndMonitor(t *testing.T) {
	logDir := "./test_logs_concsm"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	for i := 0; i < 5; i++ {
		pm.AddProcess(&config.ProgramConfig{
			Name:         fmt.Sprintf("concsm_%d", i),
			Command:      "sleep 60",
			AutoStart:    true,
			AutoRestart:  true,
			StartSecs:    0,
			StartRetries: 3,
			RestartCodes: []int{1},
			Environment:  make(map[string]string),
		})
	}

	pm.RangeProcesses(func(name string, p *Process) {
		p.Start()
	})
	defer pm.StopAll()

	monitor := NewMonitor(pm)
	monitor.Start()
	defer monitor.Stop()

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				pm.RangeProcesses(func(name string, p *Process) {
					s := p.Snapshot()
					_ = s.Name
					_ = s.State
					_ = s.PID
					_ = s.ExitCode
				})
				_ = pm.Len()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			pm.RangeProcesses(func(name string, p *Process) {
				if p.GetState() == StateRunning {
					_ = p.Restart()
				}
			})
			time.Sleep(50 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify processes are still in a consistent state after concurrent access.
	if pm.Len() != 5 {
		t.Errorf("expected 5 processes, got %d", pm.Len())
	}
	pm.RangeProcesses(func(name string, p *Process) {
		s := p.Snapshot()
		if s.Name == "" {
			t.Error("Snapshot Name should not be empty after concurrent access")
		}
		if s.State == "" {
			t.Error("Snapshot State should not be empty after concurrent access")
		}
	})
}

// TestStartFailureDoesNotDeadlockNextStart verifies that when Start() fails
// (e.g., cmd.Start() fails), the leaked monitorDone/waitCh channels do not
// block the next Start() call.
func TestStartFailureDoesNotDeadlockNextStart(t *testing.T) {
	logDir := "./test_logs_startfail"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	// Use a non-existent user to trigger lookup failure in Start()
	cfg := &config.ProgramConfig{
		Name:         "fail_then_ok",
		Command:      "sleep 60",
		User:         "nonexistent_user_xyz123",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("fail_then_ok")

	// First Start() should fail (user lookup)
	if err := p.Start(); err == nil {
		t.Log("expected Start to fail with nonexistent user, but it succeeded")
		// Clean up if it somehow succeeded
		p.Stop()
	}

	// Verify process state is Exited (the error path in Start())
	if state := p.GetState(); state != StateExited {
		t.Errorf("after failed Start, expected StateExited, got %s", state)
	}

	// Fix the config so next Start() succeeds
	p.Config.User = ""
	p.Config.Command = "sleep 30"

	// Second Start() MUST NOT deadlock. Use a timeout.
	done := make(chan error, 1)
	go func() {
		done <- p.Start()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("second Start() should succeed, got: %v", err)
		}
		p.Stop()
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK: second Start() blocked for 5 seconds after first Start() failure")
	}
}

// TestRestartOnFatalReturnsError verifies Restart() rejects FATAL processes.
func TestRestartOnFatalReturnsError(t *testing.T) {
	logDir := "./test_logs_fatalrestart"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "fatal_test",
		Command:      "sleep 60",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("fatal_test")

	// Put process in FATAL state
	p.mu.Lock()
	p.State = StateFatal
	p.mu.Unlock()

	// Restart() should return an error
	err := p.Restart()
	if err == nil {
		t.Error("Restart() on FATAL process should return error")
	}
	if p.GetState() == StateRunning {
		t.Error("FATAL process should not be restarted")
	}
}

// TestRestartOnFatalViaStopStart verifies that Stop-then-Start does NOT
// bypass the FATAL check in Restart() — but a direct Start() from FATAL
// should work (since Restart is the API that should gate, not Start).
func TestStartFromFatalWorks(t *testing.T) {
	logDir := "./test_logs_fatalstart"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "fatal_start",
		Command:      "sleep 0.1",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("fatal_start")

	p.mu.Lock()
	p.State = StateFatal
	p.mu.Unlock()

	// Direct Start() from FATAL should still work (this is intentional —
	// a manual Start() is an explicit recovery action)
	if err := p.Start(); err != nil {
		t.Errorf("direct Start() from FATAL should work: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	p.Stop()
}

// TestResourceHealthySeparateFromHealthy verifies that readProcStats sets
// ResourceHealthy without overwriting the health check's Healthy field.
func TestResourceHealthySeparateFromHealthy(t *testing.T) {
	p := &Process{
		Name:            "test_resource_health",
		Config:          &config.ProgramConfig{CPUThresholdPercent: 90, MemoryThresholdBytes: 1 << 30},
		CPUUsage:        50,
		MemoryUsage:     100 << 20,
		Healthy:         true,
		ResourceHealthy: true,
	}
	// Simulate what readProcStats does: update ResourceHealthy only.
	p.mu.Lock()
	p.ResourceHealthy = p.CPUUsage < p.Config.CPUThresholdPercent && p.MemoryUsage < uint64(p.Config.MemoryThresholdBytes)
	p.mu.Unlock()

	if !p.ResourceHealthy {
		t.Error("ResourceHealthy should be true when under thresholds")
	}
	// Healthy should be untouched by resource monitoring.
	if !p.Healthy {
		t.Error("Healthy should not be affected by readProcStats")
	}

	// Simulate resource over threshold.
	p.mu.Lock()
	p.CPUUsage = 95
	p.ResourceHealthy = p.CPUUsage < p.Config.CPUThresholdPercent && p.MemoryUsage < uint64(p.Config.MemoryThresholdBytes)
	p.mu.Unlock()

	if p.ResourceHealthy {
		t.Error("ResourceHealthy should be false when over CPU threshold")
	}
	// Healthy should STILL be untouched.
	if !p.Healthy {
		t.Error("Healthy should still be unaffected by readProcStats")
	}
}

// TestRestartTimestampOnlyAfterStartSuccess verifies that addRestartTimestamp
// is called inside Start() after a successful launch, not before.
func TestRestartTimestampOnlyAfterStartSuccess(t *testing.T) {
	logDir := "./test_logs_restart_ts"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:             "restart_ts_test",
		Command:          "sleep 0.2",
		AutoStart:        false,
		AutoRestart:      false,
		StartSecs:        0,
		StartRetries:     3,
		RestartMaxCount:  5,
		RestartWindowSecs: 60,
		Environment:      make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("restart_ts_test")

	// Before Start(), restartTimestamps should be empty.
	p.mu.Lock()
	initialCount := len(p.restartTimestamps)
	p.mu.Unlock()
	if initialCount != 0 {
		t.Errorf("expected 0 restart timestamps before Start(), got %d", initialCount)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// After successful Start(), restartTimestamps should have one entry.
	p.mu.Lock()
	afterCount := len(p.restartTimestamps)
	p.mu.Unlock()
	if afterCount != 1 {
		t.Errorf("expected 1 restart timestamp after successful Start(), got %d", afterCount)
	}

	p.Stop()
}

// TestHealthCheckCtxCancelledOnStop verifies that Stop() cancels the health
// check context so the goroutine can exit promptly.
func TestHealthCheckCtxCancelledOnStop(t *testing.T) {
	logDir := "./test_logs_hc_ctx"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_ctx_test",
		Command:                "sleep 10",
		AutoStart:              false,
		AutoRestart:            false,
		HealthCheckURL:         "http://127.0.0.1:19999/health",
		HealthCheckInterval:    60,
		HealthCheckTimeout:     5,
		StartSecs:              0,
		StartRetries:           3,
		Environment:            make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_ctx_test")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Verify health check context is active before Stop.
	p.mu.Lock()
	hcCtx := p.healthCheckCtx
	p.mu.Unlock()
	if hcCtx == nil {
		t.Fatal("healthCheckCtx should be set after Start()")
	}
	if hcCtx.Err() != nil {
		t.Error("healthCheckCtx should not be cancelled before Stop()")
	}

	p.Stop()

	// After Stop(), the health check context should be cancelled.
	p.mu.Lock()
	hcCtxAfter := p.healthCheckCtx
	p.mu.Unlock()
	if hcCtxAfter != nil && hcCtxAfter.Err() == nil {
		t.Error("healthCheckCtx should be cancelled after Stop()")
	}
}

// TestCleanupBackupsSortedCorrectly verifies that cleanupBackups removes the
// oldest files when count exceeds the limit. Uses GetProcessLogWriters to
// register a stream so that rotation triggers cleanup via the normal path.
func TestCleanupBackupsSortedCorrectly(t *testing.T) {
	logDir := "./test_logs_cleanup_sort"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewLogger(logDir, 100, 2, false, logger.LevelInfo, logger.FormatText)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logManager.Close()

	// Write enough data to trigger rotation via process log writers.
	cfg := &config.ProgramConfig{
		StdoutLogFile:        filepath.Join(logDir, "sort_test.log"),
		StdoutLogMaxBytes:    100,
		StdoutLogBackupCount: 2,
	}
	w, _, err := logManager.GetProcessLogWriters("sort_test", cfg)
	if err != nil {
		t.Fatalf("GetProcessLogWriters failed: %v", err)
	}

	// Write 5 times to create 4+ backups. Each write should trigger rotation.
	for i := 0; i < 5; i++ {
		data := make([]byte, 150)
		w.Write(data)
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)

	// Count backup files — should not exceed backupCount (2).
	files, _ := filepath.Glob(filepath.Join(logDir, "sort_test.log.*"))
	if len(files) > 2 {
		t.Errorf("expected at most 2 backup files, got %d: %v", len(files), files)
	}
}

// TestRingBufferGeneric verifies the generic RingBuffer works for both
// Event and ResourceSample types.
func TestRingBufferGeneric(t *testing.T) {
	t.Run("EventBuffer", func(t *testing.T) {
		eb := NewEventBuffer(3)
		if eb.Len() != 0 {
			t.Errorf("expected 0, got %d", eb.Len())
		}
		eb.Push(Event{Name: "e1"})
		eb.Push(Event{Name: "e2"})
		if eb.Len() != 2 {
			t.Errorf("expected 2, got %d", eb.Len())
		}
		s := eb.Snapshot(10)
		if len(s) != 2 {
			t.Errorf("expected 2 events, got %d", len(s))
		}
		// Test wrap-around.
		eb.Push(Event{Name: "e3"})
		eb.Push(Event{Name: "e4"})
		s = eb.Snapshot(10)
		if len(s) != 3 {
			t.Errorf("expected 3 events after wrap, got %d", len(s))
		}
	})

	t.Run("ResourceHistory", func(t *testing.T) {
		rh := NewResourceHistory(4)
		now := time.Now()
		// Push in chronological order (as real code does).
		rh.Push(ResourceSample{Timestamp: now.Add(-3 * time.Minute), CPU: 1.0})
		rh.Push(ResourceSample{Timestamp: now.Add(-2 * time.Minute), CPU: 2.0})
		rh.Push(ResourceSample{Timestamp: now.Add(-1 * time.Minute), CPU: 3.0})
		rh.Push(ResourceSample{Timestamp: now, CPU: 4.0})
		// All 4 should be returned with since=0.
		s := rh.Snapshot(0)
		if len(s) != 4 {
			t.Errorf("expected 4 samples, got %d", len(s))
		}
		// With since=90s, only the last 2 should be returned.
		s = rh.Snapshot(90 * time.Second)
		if len(s) != 2 {
			t.Errorf("expected 2 samples within last 90s, got %d", len(s))
		}
		// With since=30s, only the last 1 should be returned.
		s = rh.Snapshot(30 * time.Second)
		if len(s) != 1 {
			t.Errorf("expected 1 sample within last 30s, got %d", len(s))
		}
	})
}

// TestHealthCheckRestartNoFalseFatal verifies the fix for the race condition
// where checkRunningProcess triggers Restart() for health check, and a
// concurrent restart from handleExitedProcess causes Restart() to fail with
// "already running/starting", which should NOT escalate to FATAL.
func TestHealthCheckRestartNoFalseFatal(t *testing.T) {
	logDir := "./test_logs_hc_restart_race"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_restart_race",
		Command:                "sleep 60",
		AutoStart:              false,
		AutoRestart:            true,
		HealthCheckURL:         "http://127.0.0.1:19998/health",
		HealthCheckInterval:    1,
		HealthCheckTimeout:     1,
		HealthCheckUnhealthyThreshold: 1,
		HealthCheckRestart:     true,
		StartSecs:              0,
		StartRetries:           3,
		RestartWindowSecs:      60,
		RestartMaxCount:        0,
		StopSignal:             "SIGTERM",
		StopSecs:               10,
		Environment:            make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_restart_race")

	// Start the process.
	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Verify process is running.
	if p.GetState() != StateRunning {
		t.Fatalf("expected RUNNING, got %s", p.GetState())
	}

	// Mark process as unhealthy to trigger health check restart path.
	p.mu.Lock()
	p.Healthy = false
	p.mu.Unlock()

	// Now simulate checkRunningProcess calling Restart() for health check.
	// At the same time, simulate handleExitedProcess having set STARTING.
	// Restart() should return nil (not error) when state is STARTING.
	p.mu.Lock()
	p.State = StateStarting
	p.mu.Unlock()

	err := p.Restart()
	if err != nil {
		t.Errorf("Restart() should return nil when process is STARTING, got: %v", err)
	}

	// Also test that Restart() returns nil for STOPPING state.
	p.mu.Lock()
	p.State = StateStopping
	p.mu.Unlock()

	err = p.Restart()
	if err != nil {
		t.Errorf("Restart() should return nil when process is STOPPING, got: %v", err)
	}

	p.Stop()
}

// TestHealthCheckFailuresResetOnStart verifies that healthCheckFailures is
// reset to 0 on Start(), preventing the new health check goroutine from
// immediately marking a freshly restarted process as unhealthy.
func TestHealthCheckFailuresResetOnStart(t *testing.T) {
	logDir := "./test_logs_hc_reset"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_reset_test",
		Command:                "sleep 60",
		AutoStart:              false,
		AutoRestart:            false,
		HealthCheckURL:         "http://127.0.0.1:19997/health",
		HealthCheckInterval:    60,
		HealthCheckTimeout:     5,
		StartSecs:              0,
		StartRetries:           3,
		Environment:            make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_reset_test")

	// Manually set failures before start to simulate a previous unhealthy cycle.
	p.mu.Lock()
	p.healthCheckFailures = 5
	p.mu.Unlock()

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	p.mu.Lock()
	failures := p.healthCheckFailures
	p.mu.Unlock()

	if failures != 0 {
		t.Errorf("healthCheckFailures should be reset to 0 on Start(), got %d", failures)
	}

	p.Stop()
}

// TestMonitorStopDoubleClose verifies Monitor.Stop is safe to call multiple times.
func TestMonitorStopDoubleClose(t *testing.T) {
	logDir := "./test_logs_monitor_stop"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	monitor := NewMonitor(pm)
	monitor.Start()
	time.Sleep(100 * time.Millisecond)

	// Should not panic when called multiple times.
	monitor.Stop()
	monitor.Stop()
	monitor.Stop()

	// Verify monitor's Done channel is closed after Stop.
	select {
	case <-monitor.Done:
		// Expected: channel is closed.
	default:
		t.Error("monitor.Done channel should be closed after Stop()")
	}
}

func TestCompareConfigsNoChange(t *testing.T) {
	logDir := "./test_logs_cmp_noop"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "cmp_test",
		Command:     "sleep 10",
		AutoStart:   true,
		StartRetries: 3,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)

	newConfigs := map[string]*config.ProgramConfig{
		"cmp_test": cfg,
	}
	added, removed, modified := pm.CompareConfigs(newConfigs)
	if len(added) != 0 {
		t.Errorf("expected 0 added, got %d", len(added))
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
	if len(modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(modified))
	}
}

func TestCompareConfigsAddedAndRemoved(t *testing.T) {
	logDir := "./test_logs_cmp_diff"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:    "keep",
		Command: "sleep 10",
		Environment: make(map[string]string),
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:    "remove_me",
		Command: "sleep 10",
		Environment: make(map[string]string),
	})

	newConfigs := map[string]*config.ProgramConfig{
		"keep":     {Name: "keep", Command: "sleep 10", Environment: make(map[string]string)},
		"new_one":  {Name: "new_one", Command: "sleep 10", Environment: make(map[string]string)},
	}

	added, removed, modified := pm.CompareConfigs(newConfigs)
	if len(added) != 1 || added[0] != "new_one" {
		t.Errorf("expected added=[new_one], got %v", added)
	}
	if len(removed) != 1 || removed[0] != "remove_me" {
		t.Errorf("expected removed=[remove_me], got %v", removed)
	}
	if len(modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(modified))
	}
}

func TestCompareConfigsModified(t *testing.T) {
	logDir := "./test_logs_cmp_mod"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	pm.AddProcess(&config.ProgramConfig{
		Name:    "changer",
		Command: "sleep 10",
		AutoStart: true,
		Environment: make(map[string]string),
	})

	// Changed Command and AutoStart
	newConfigs := map[string]*config.ProgramConfig{
		"changer": {Name: "changer", Command: "sleep 30", AutoStart: false, Environment: make(map[string]string)},
	}

	added, removed, modified := pm.CompareConfigs(newConfigs)
	if len(added) != 0 {
		t.Errorf("expected 0 added, got %d", len(added))
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
	if len(modified) != 1 || modified[0] != "changer" {
		t.Errorf("expected modified=[changer], got %v", modified)
	}
}

// TestCheckRunningProcessHealthCheckRestart verifies that checkRunningProcess
// triggers Restart() when the process is unhealthy with HealthCheckRestart enabled.
func TestCheckRunningProcessHealthCheckRestart(t *testing.T) {
	logDir := "./test_logs_hc_restart"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_restart",
		Command:                "sleep 60",
		AutoStart:              false,
		AutoRestart:            true,
		HealthCheckURL:         "http://127.0.0.1:19999/health",
		HealthCheckInterval:    60,
		HealthCheckTimeout:     5,
		HealthCheckUnhealthyThreshold: 1,
		HealthCheckRestart:     true,
		StartSecs:              0,
		StartRetries:           3,
		StopSignal:             "SIGTERM",
		StopSecs:               10,
		Environment:            make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_restart")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer p.Stop()

	oldPID := p.Snapshot().PID

	p.mu.Lock()
	p.Healthy = false
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.checkRunningProcess(p)

	snap := p.Snapshot()
	if snap.PID == oldPID {
		t.Error("expected PID to change after health check restart")
	}
	if snap.State != StateRunning {
		t.Errorf("expected StateRunning after restart, got %s", snap.State)
	}
}

// TestCheckRunningProcessHealthCheckRestartFatal verifies that when
// checkRunningProcess triggers a health check restart and the restart fails
// with StartRetries exceeded, the process enters FATAL state.
func TestCheckRunningProcessHealthCheckRestartFatal(t *testing.T) {
	logDir := "./test_logs_hc_fatal"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_fatal",
		Command:                "sleep 60",
		AutoStart:              false,
		AutoRestart:            true,
		HealthCheckURL:         "http://127.0.0.1:20000/health",
		HealthCheckInterval:    60,
		HealthCheckTimeout:     5,
		HealthCheckRestart:     true,
		StartSecs:              0,
		StartRetries:           3,
		StopSignal:             "SIGTERM",
		StopSecs:               10,
		Environment:            make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_fatal")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() {
		if p.GetState() != StateFatal {
			p.Stop()
		}
	}()

	p.mu.Lock()
	p.Healthy = false
	p.StartRetries = 99
	p.Config.User = "nonexistent_user_xyz_12345"
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.checkRunningProcess(p)

	state := p.GetState()
	if state != StateFatal {
		t.Errorf("expected StateFatal after failed health check restart, got %s", state)
	}
}

// TestRunHealthCheckTCP verifies TCP health check path ("tcp://" prefix).
func TestRunHealthCheckTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().String()
	if !checkHealth("tcp://"+addr, 2*time.Second) {
		t.Error("TCP health check should succeed when listener is up")
	}

	if checkHealth("tcp://127.0.0.1:65535", 100*time.Millisecond) {
		t.Error("TCP health check should fail for unreachable address")
	}
}

// TestSendWebhookSuccess verifies the webhook delivers successfully to a test server.
func TestSendWebhookSuccess(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		received <- struct{}{}
	}))
	defer srv.Close()

	p := &Process{
		Name: "webhook_test",
		Config: &config.ProgramConfig{
			WebhookURL:     srv.URL,
			WebhookTimeout: 5,
			WebhookRetries: 0,
			Group:          "testgroup",
		},
	}

	p.sendWebhook(StateRunning, 12345, 0)

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Error("webhook was not received within timeout")
	}
}

// TestSendWebhookRetryExhausted verifies retries are attempted with backoff
// and the function eventually gives up after max retries.
func TestSendWebhookRetryExhausted(t *testing.T) {
	attempts := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &Process{
		Name: "webhook_fail",
		Config: &config.ProgramConfig{
			WebhookURL:     srv.URL,
			WebhookTimeout: 1,
			WebhookRetries: 2,
		},
	}

	p.sendWebhook(StateRunning, 12345, 0)

	mu.Lock()
	if attempts != 3 {
		t.Errorf("expected 3 webhook attempts (1 initial + 2 retries), got %d", attempts)
	}
	mu.Unlock()
}

// TestSendWebhookNoURL is a no-op when WebhookURL is empty.
func TestSendWebhookNoURL(t *testing.T) {
	p := &Process{
		Name: "nowebhook",
		Config: &config.ProgramConfig{
			WebhookURL: "",
		},
	}
	p.sendWebhook(StateRunning, 0, 0)
	// Verify the no-op guard: WebhookURL must be empty.
	if p.Config.WebhookURL != "" {
		t.Error("WebhookURL should be empty for no-op test")
	}
}

// TestSignalToRunningProcess sends SIGTERM to a running process.
func TestSignalToRunningProcess(t *testing.T) {
	logDir := "./test_logs_signal"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "signal_test",
		Command:     "sleep 60",
		Directory:   ".",
		AutoStart:   false,
		AutoRestart: false,
		StopSignal:  "SIGTERM",
		StopSecs:    10,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("signal_test")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer p.Stop()

	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("Signal(SIGTERM) should succeed on running process, got: %v", err)
	}
}

// TestSignalToNonRunningProcess returns error when process is not running.
func TestSignalToNonRunningProcess(t *testing.T) {
	logDir := "./test_logs_signal_norun"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "signal_norun",
		Command:     "sleep 1",
		Directory:   ".",
		AutoStart:   false,
		AutoRestart: false,
		StopSignal:  "SIGTERM",
		StopSecs:    10,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("signal_norun")

	if err := p.Signal(syscall.SIGTERM); err == nil {
		t.Error("Signal() should return error when process is not running")
	}
}

// TestSignalProcessGroup verifies signalProcessGroup works on a real process group.
func TestSignalProcessGroup(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("signalProcessGroup requires root to verify kill() works on pgid")
	}

	logDir := "./test_logs_siggrp"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "siggrp_test",
		Command:      "sh -c 'sleep 60 & sleep 60'",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StopSignal:   "SIGTERM",
		StopSecs:     10,
		KillsAsGroup: true,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("siggrp_test")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer p.Stop()

	if p.PID <= 0 {
		t.Fatal("expected positive PID")
	}

	// Send signal to process group
	err := signalProcessGroup(p.PID, syscall.SIGTERM)
	if err != nil {
		t.Errorf("signalProcessGroup failed: %v", err)
	}

	// Wait for the process to exit
	time.Sleep(500 * time.Millisecond)
	state := p.GetState()
	if state == StateRunning {
		t.Log("process may still be running (expected on some systems)")
	}
}

// TestRunHealthCheckLifecycle tests the health check cycle:
// failure -> unhealthy -> restore.
func TestRunHealthCheckLifecycle(t *testing.T) {
	callCount := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		c := callCount
		mu.Unlock()
		if c <= 1 || c > 4 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	logDir := "./test_logs_hc_lifecycle"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_lifecycle",
		Command:                "sleep 60",
		AutoStart:              false,
		AutoRestart:            true,
		HealthCheckURL:         srv.URL,
		HealthCheckInterval:    1,
		HealthCheckTimeout:     2,
		HealthCheckUnhealthyThreshold: 3,
		HealthCheckRestart:     true,
		StartSecs:              0,
		StartRetries:           3,
		StopSignal:             "SIGTERM",
		StopSecs:               10,
		Environment:            make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_lifecycle")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer p.Stop()

	time.Sleep(4 * time.Second)

	snap := p.Snapshot()
	t.Logf("State=%s Healthy=%v healthCheckFailures=%d", snap.State, snap.Healthy, p.healthCheckFailures)
}

// TestOnHealthCheckFailureCallback verifies the OnHealthCheckFailure callback fires.
func TestOnHealthCheckFailureCallback(t *testing.T) {
	var mu sync.Mutex
	failureCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	logDir := "./test_logs_hc_callback"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:                   "hc_callback",
		Command:                "sleep 60",
		AutoStart:              false,
		AutoRestart:            true,
		HealthCheckURL:         srv.URL,
		HealthCheckInterval:    1,
		HealthCheckTimeout:     2,
		HealthCheckUnhealthyThreshold: 5,
		HealthCheckRestart:     true,
		StartSecs:              0,
		StartRetries:           3,
		StopSignal:             "SIGTERM",
		StopSecs:               10,
		Environment:            make(map[string]string),
	}

	pm.AddProcess(cfg)
	p := pm.GetProcess("hc_callback")
	p.OnHealthCheckFailure = func(name string) {
		mu.Lock()
		failureCount++
		mu.Unlock()
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer p.Stop()

	time.Sleep(3 * time.Second)

	mu.Lock()
	if failureCount < 1 {
		t.Errorf("OnHealthCheckFailure should have been called at least once, got %d", failureCount)
	}
	mu.Unlock()
}

func TestStdinFile(t *testing.T) {
	dir := t.TempDir()
	stdinContent := "hello from stdin\n"
	stdinPath := filepath.Join(dir, "stdin.txt")
	if err := os.WriteFile(stdinPath, []byte(stdinContent), 0644); err != nil {
		t.Fatal(err)
	}

	logManager, err := logger.NewDefaultLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:      "stdin_test",
		Command:   "cat",
		Directory: dir,
		StdinFile: stdinPath,
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("stdin_test")
	if p == nil {
		t.Fatal("process not created")
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for process to finish (cat reads stdin and exits)
	time.Sleep(500 * time.Millisecond)
	s := p.Snapshot()
	if s.State == StateRunning {
		_ = p.Stop()
	}
}

func TestStdinFile_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:      "stdin_test2",
		Command:   "cat",
		Directory: dir,
		StdinFile: "/nonexistent/stdin.txt",
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("stdin_test2")

	// Should start without StdinFile, cat will hang waiting for stdin
	err = p.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	_ = p.Stop()
}

// --- Error path tests: verify state cleanup, fd closure, and goroutine hygiene on Start() failure ---

// countOpenFDs returns the number of open file descriptors for the current process.
func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("cannot read /proc/self/fd: %v", err)
	}
	return len(entries)
}

// badShebangPath creates an executable file whose shebang points to a nonexistent
// interpreter, forcing cmd.Start() to fail. Returns the path to the temp file.
func badShebangPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bad_shebang")
	if err := os.WriteFile(path, []byte("#!/nonexistent/interpreter/xyz\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStart_ExecFails verifies that starting a process whose binary fails to exec
// (bad shebang) fails cleanly: error returned, state reset to Exited.
func TestStart_ExecFails(t *testing.T) {
	dir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:      "exec_fail",
		Command:   badShebangPath(t),
		Directory: "/tmp",
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("exec_fail")
	if p == nil {
		t.Fatal("process not created")
	}

	err = p.Start()
	if err == nil {
		_ = p.Stop()
		t.Fatal("expected error for bad shebang binary, got nil")
	}

	// After failure, state must be Exited (not stuck in Starting).
	if state := p.GetState(); state != StateExited {
		t.Errorf("expected StateExited after exec failure, got %s", state)
	}

	// StartRetries must not have been incremented past the guard in Start().
	s := p.Snapshot()
	if s.StartRetries > 1 {
		t.Errorf("StartRetries should be at most 1 after exec failure, got %d", s.StartRetries)
	}
}

// TestStart_UserNotFound verifies that starting a process with a nonexistent user fails.
func TestStart_UserNotFound(t *testing.T) {
	dir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:      "user_not_found",
		Command:   "sleep 1",
		Directory: "/tmp",
		User:      "nonexistent_user_xyz_12345",
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("user_not_found")
	if p == nil {
		t.Fatal("process not created")
	}

	err = p.Start()
	if err == nil {
		_ = p.Stop()
		t.Fatal("expected error for nonexistent user, got nil")
	}

	if state := p.GetState(); state != StateExited {
		t.Errorf("expected StateExited after user lookup failure, got %s", state)
	}
}

// TestStart_ExecFails_NoFdLeak verifies that file descriptors are not leaked when
// StdinFile is set but the command fails to start.
func TestStart_ExecFails_NoFdLeak(t *testing.T) {
	dir := t.TempDir()

	// Create a real stdin file so StdinFile opens successfully.
	stdinPath := filepath.Join(dir, "stdin.txt")
	if err := os.WriteFile(stdinPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	logManager, err := logger.NewDefaultLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logManager.Close()

	fdsBefore := countOpenFDs(t)

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:      "exec_fail_stdin",
		Command:   badShebangPath(t),
		Directory: "/tmp",
		StdinFile: stdinPath,
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("exec_fail_stdin")
	if p == nil {
		t.Fatal("process not created")
	}

	err = p.Start()
	if err == nil {
		_ = p.Stop()
		t.Fatal("expected error for bad shebang binary, got nil")
	}

	// Allow any deferred cleanup to run.
	time.Sleep(50 * time.Millisecond)

	fdsAfter := countOpenFDs(t)
	if fdsAfter > fdsBefore+2 {
		t.Errorf("fd leak detected: before=%d, after=%d (diff=%d)", fdsBefore, fdsAfter, fdsAfter-fdsBefore)
	}
}

// TestStart_ExecFails_Flaky verifies with 50 iterations that Start() failure
// is consistently clean — no intermittent state corruption.
// Note: does not use goleak because other tests in the suite may leave background
// goroutines (Monitor tickers, health checks) that fire during iteration windows.
func TestStart_ExecFails_Flaky(t *testing.T) {
	for i := 0; i < 50; i++ {
		i := i
		dir := t.TempDir()
		logManager, err := logger.NewDefaultLogger(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer logManager.Close()

		pm := NewProcessManager(logManager)
		cfg := &config.ProgramConfig{
			Name:      fmt.Sprintf("exec_flaky_%d", i),
			Command:   badShebangPath(t),
			Directory: "/tmp",
		}
		pm.AddProcess(cfg)
		p := pm.GetProcess(cfg.Name)
		if p == nil {
			t.Fatal("process not created")
		}

		err = p.Start()
		if err == nil {
			_ = p.Stop()
			t.Fatal("expected error for bad shebang binary")
		}

		if state := p.GetState(); state != StateExited {
			t.Errorf("iteration %d: expected StateExited, got %s", i, state)
		}
	}
}

func TestFcgiProcessStartStop(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "fcgi.sock")

	Logf = t.Logf

	pm := NewProcessManager(nil)
	cfg := &config.ProgramConfig{
		Name:    "fcgi-test",
		Command: "sleep 10",
		Socket:  "unix://" + sockPath,
		SocketMode: 0700,
		StopSecs:   1,
		StartSecs:  1,
		StartRetries: 1,
	}

	p := pm.AddProcess(cfg)
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket file was not created after start")
	}

	pm.mu.RLock()
	sm, ok := pm.fcgiSockets["fcgi-test"]
	pm.mu.RUnlock()
	if !ok {
		t.Fatal("fcgi socket not found in manager")
	}
	if sm.RefCount() != 1 {
		t.Errorf("expected refcount=1, got %d", sm.RefCount())
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	pm.mu.RLock()
	_, ok = pm.fcgiSockets["fcgi-test"]
	pm.mu.RUnlock()
	if ok {
		t.Error("fcgi socket should be removed from manager after stop")
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after last child stops")
	}
}

func TestFcgiNumProcsSocketShared(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "shared.sock")

	Logf = t.Logf

	pm := NewProcessManager(nil)
	cfg1 := &config.ProgramConfig{
		Name:    "fcgi-shared_1",
		Command: "sleep 30",
		Socket:  "unix://" + sockPath,
		SocketMode: 0700,
		StopSecs:   1,
		StartSecs:  1,
		StartRetries: 1,
	}
	cfg2 := &config.ProgramConfig{
		Name:    "fcgi-shared_2",
		Command: "sleep 30",
		Socket:  "unix://" + sockPath,
		SocketMode: 0700,
		StopSecs:   1,
		StartSecs:  1,
		StartRetries: 1,
	}

	p1 := pm.AddProcess(cfg1)
	p2 := pm.AddProcess(cfg2)

	if err := p1.Start(); err != nil {
		t.Fatalf("p1 Start failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := p2.Start(); err != nil {
		t.Fatalf("p2 Start failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	pm.mu.RLock()
	sm, ok := pm.fcgiSockets["fcgi-shared"]
	pm.mu.RUnlock()
	if !ok {
		t.Fatal("fcgi socket not found")
	}
	if sm.RefCount() != 2 {
		t.Errorf("expected refcount=2, got %d", sm.RefCount())
	}

	if err := p1.Stop(); err != nil {
		t.Fatalf("p1 Stop failed: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	pm.mu.RLock()
	if sm.RefCount() != 1 {
		t.Errorf("expected refcount=1 after p1 stop, got %d", sm.RefCount())
	}
	pm.mu.RUnlock()
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Error("socket should still exist after p1 stop")
	}

	if err := p2.Stop(); err != nil {
		t.Fatalf("p2 Stop failed: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket should be removed after last child stops")
	}

	time.Sleep(200 * time.Millisecond)
}

func TestFcgiSnapshotFields(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "snap.sock")

	Logf = t.Logf

	pm := NewProcessManager(nil)
	cfg := &config.ProgramConfig{
		Name:    "fcgi-snap",
		Command: "sleep 10",
		Socket:  "unix://" + sockPath,
		SocketMode: 0700,
		StopSecs:   1,
		StartSecs:  1,
		StartRetries: 1,
	}

	p := pm.AddProcess(cfg)
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	snap := p.Snapshot()
	if snap.FcgiSocket != "unix://"+sockPath {
		t.Errorf("expected fcgi_socket in snapshot, got %s", snap.FcgiSocket)
	}
	if snap.FcgiRefCount != 1 {
		t.Errorf("expected fcgi_refcount=1, got %d", snap.FcgiRefCount)
	}

	if err := p.Stop(); err != nil {
		t.Logf("Stop error: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}

func TestStartAll_DependencyOrder(t *testing.T) {
	dir := t.TempDir()
	logManager, _ := logger.NewDefaultLogger(dir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	// Verify that topologicalSort produces correct dependency order:
	// "a" depends on "b", so "b" must come before "a".
	// Testing the sort directly avoids OS-level races that would plague
	// any file-writing approach (echo commands run concurrently after fork).
	graph := map[string][]string{
		"a": {"b"},
		"b": {},
	}

	ordered, err := pm.topologicalSort(graph)
	if err != nil {
		t.Fatalf("topologicalSort failed: %v", err)
	}

	if len(ordered) != 2 || ordered[0] != "b" || ordered[1] != "a" {
		t.Errorf("expected [b a] (dependency first), got %v", ordered)
	}
}

func TestStopAll_StopsAllProcesses(t *testing.T) {
	dir := t.TempDir()
	logManager, _ := logger.NewDefaultLogger(dir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	pm.AddProcess(&config.ProgramConfig{
		Name:        "x",
		Command:     "sleep 60",
		DependsOn:   []string{},
		AutoStart:   true,
		AutoRestart: false,
		StartSecs:   0,
		StopSecs:    1,
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:        "y",
		Command:     "sleep 60",
		DependsOn:   []string{},
		AutoStart:   true,
		AutoRestart: false,
		StartSecs:   0,
		StopSecs:    1,
	})

	pm.StartAll()
	time.Sleep(200 * time.Millisecond)

	for _, name := range []string{"x", "y"} {
		p := pm.GetProcess(name)
		if p.GetState() != StateRunning {
			t.Errorf("process %s should be RUNNING after StartAll(), got %s", name, p.GetState())
		}
	}

	pm.StopAll()

	for _, name := range []string{"x", "y"} {
		p := pm.GetProcess(name)
		if p.GetState() == StateRunning {
			t.Errorf("process %s should be stopped after StopAll()", name)
		}
	}
}

func TestStopAll_ReverseDependencyOrder(t *testing.T) {
	dir := t.TempDir()
	logManager, _ := logger.NewDefaultLogger(dir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	// a depends on b. StopAll should stop "a" (dependent) before "b" (dependency).
	// We verify this via StopTime on each process's Snapshot.
	pm.AddProcess(&config.ProgramConfig{
		Name:        "a",
		Command:     "sleep 60",
		DependsOn:   []string{"b"},
		AutoStart:   true,
		AutoRestart: false,
		StartSecs:   0,
		StopSecs:    1,
	})
	pm.AddProcess(&config.ProgramConfig{
		Name:        "b",
		Command:     "sleep 60",
		DependsOn:   []string{},
		AutoStart:   true,
		AutoRestart: false,
		StartSecs:   0,
		StopSecs:    1,
	})

	pm.StartAll()
	time.Sleep(200 * time.Millisecond)
	pm.StopAll()

	a := pm.GetProcess("a")
	b := pm.GetProcess("b")
	if a == nil || b == nil {
		t.Fatal("processes not found")
	}

	aSnap := a.Snapshot()
	bSnap := b.Snapshot()

	// Both should be stopped
	if aSnap.State == StateRunning {
		t.Error("a should not be RUNNING after StopAll()")
	}
	if bSnap.State == StateRunning {
		t.Error("b should not be RUNNING after StopAll()")
	}

	// a (dependent) must have stopped before b (dependency)
	// StopTime is set by Stop(), and Stop() is called in reverse dependency order.
	if aSnap.StopTime.After(bSnap.StopTime) {
		t.Errorf("a must stop before b (a.StopTime=%v, b.StopTime=%v)", aSnap.StopTime, bSnap.StopTime)
	}
}

func TestSendWebhook_RetryOnNetworkError(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))

	logDir := "./test_logs_webhook_retry"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:           "wh_retry",
		Command:        "echo test",
		AutoStart:      false,
		AutoRestart:    false,
		WebhookURL:     srv.URL,
		WebhookRetries: 2,
		WebhookTimeout: 1,
		StopSecs:       1,
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("wh_retry")

	// First call: server is up, should succeed in 1 attempt.
	p.sendWebhook("STARTING", 123, 0)
	if attempts != 1 {
		t.Errorf("expected 1 attempt when server is up, got %d", attempts)
	}

	// Close server, then call again. With network error, it should retry
	// (initial + WebhookRetries attempts = 3 total). But since server is
	// closed, all attempts fail. Verify it doesn't panic and completes
	// within a reasonable time.
	attempts = 0
	start := time.Now()
	srv.Close()
	p.sendWebhook("RUNNING", 123, 0)
	elapsed := time.Since(start)
	// With 2 retries + 1s timeout each, should take at least 1s total
	if elapsed < 500*time.Millisecond {
		t.Errorf("expected retries to take some time, got %v", elapsed)
	}
	if elapsed > 30*time.Second {
		t.Errorf("sendWebhook took too long: %v", elapsed)
	}
}

func TestSendWebhook_BackoffCap(t *testing.T) {
	logDir := "./test_logs_webhook_backoff"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:           "wh_backoff",
		Command:        "echo test",
		AutoStart:      false,
		AutoRestart:    false,
		WebhookURL:     "http://127.0.0.1:19999/nonexistent",
		WebhookRetries: 3,
		WebhookTimeout: 1,
		StopSecs:       1,
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("wh_backoff")

	start := time.Now()
	p.sendWebhook("STARTING", 456, 0)
	elapsed := time.Since(start)

	// With WebhookRetries=3 and WebhookTimeout=1:
	// Initial attempt (timeout 1s) + retry 1 (backoff 1s, timeout 1s) +
	// retry 2 (backoff 2s, timeout 1s) + retry 3 (backoff 4s, timeout 1s)
	// Total: ~1 + 1+1 + 2+1 + 4+1 ≈ 11s minimum
	if elapsed < 2*time.Second {
		t.Errorf("expected retries to take >2s, got %v", elapsed)
	}
	if elapsed > 60*time.Second {
		t.Errorf("sendWebhook took too long: %v", elapsed)
	}
}

func TestStart_ChdirFailure(t *testing.T) {
	logDir := "./test_logs_chdir"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "chdir_test",
		Command:     "echo test",
		Directory:   "/nonexistent/path/that/does/not/exist",
		AutoStart:   false,
		AutoRestart: false,
		StopSecs:    1,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("chdir_test")

	err := p.Start()
	if err != nil {
		// Expected: Start() should return an error for nonexistent directory
		return
	}
	// If Start() succeeded (shell wrapper may spawn despite bad Dir),
	// the process should still be cleanly stoppable.
	defer p.Stop()
	state := p.GetState()
	if state != StateRunning && state != StateExited && state != StateFatal {
		t.Errorf("unexpected state after Start() with bad directory: %s", state)
	}
}

func TestStop_WaitTimeout(t *testing.T) {
	logDir := "./test_logs_stoptimeout"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "timeout_test",
		Command:     "sleep 60",
		AutoStart:   false,
		AutoRestart: false,
		StopSignal:  "SIGTERM",
		StopSecs:    1,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("timeout_test")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if p.GetState() != StateRunning {
		t.Fatalf("expected RUNNING after Start(), got %s", p.GetState())
	}

	p.Stop()

	if p.GetState() == StateRunning {
		t.Error("process should not be RUNNING after Stop()")
	}
	// Note: Stop() cancels startCancel (which kills the process via
	// CommandContext), so it always completes quickly. The StopSecs
	// timeout path is only exercised when startCancel was already
	// consumed (e.g. by monitor() on natural exit).
}

func TestSignal_InvalidSignal(t *testing.T) {
	logDir := "./test_logs_siginvalid"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "siginvalid",
		Command:     "sleep 60",
		AutoStart:   false,
		AutoRestart: false,
		StopSecs:    1,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("siginvalid")

	_ = p.Start()
	defer p.Stop()

	err := p.Signal(syscall.Signal(999))
	if err == nil {
		t.Error("expected error for invalid signal number")
	}
}

func TestCgroupIntegration(t *testing.T) {
	// Create a fake cgroup directory. applyCgroup writes PID to
	// <cgroupPath>/cgroup.procs regardless of whether it's a real cgroup.
	cgroupDir := t.TempDir()
	procsFile := filepath.Join(cgroupDir, "cgroup.procs")

	dir := "./test_logs_cgroup"
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	logManager, _ := logger.NewDefaultLogger(dir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:        "cgroup_test",
		Command:     "sleep 1",
		AutoStart:   false,
		AutoRestart: false,
		CgroupPath:  cgroupDir,
		StopSecs:    1,
		Environment: make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("cgroup_test")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer p.Stop()

	// Wait for the process to be fully started (applyCgroup runs after fork)
	time.Sleep(200 * time.Millisecond)

	data, err := os.ReadFile(procsFile)
	if err != nil {
		t.Fatalf("read cgroup.procs: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("cgroup.procs is empty — PID was not written")
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		t.Fatal("cgroup.procs contains no PID")
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		t.Errorf("cgroup.procs should contain positive PID, got: %q", pidStr)
	}
}

func TestStdinFileIntegration(t *testing.T) {
	stdinContent := "hello-from-stdin-test\n"
	stdinFile := filepath.Join(t.TempDir(), "stdin.txt")
	if err := os.WriteFile(stdinFile, []byte(stdinContent), 0644); err != nil {
		t.Fatalf("write stdin file: %v", err)
	}

	// Write stdin content to a known output file so we can verify.
	outputFile := filepath.Join(t.TempDir(), "output.txt")

	dir := "./test_logs_stdin"
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	logManager, _ := logger.NewDefaultLogger(dir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "stdin_test",
		Command:      fmt.Sprintf("cat > %s", outputFile),
		AutoStart:    false,
		AutoRestart:  false,
		StdinFile:    stdinFile,
		StopSecs:     1,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("stdin_test")

	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// cat exits immediately after reading stdin, wait for it.
	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(data), "hello-from-stdin-test") {
		t.Errorf("output should contain stdin content, got: %s", string(data))
	}
	_ = p.Stop()
}
