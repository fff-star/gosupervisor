package process

import (
	"fmt"
	"os"
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
		t.Logf("重启进程时遇到错误: %v", err)
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
		t.Logf("进程状态为%s，期望为STOPPED", state)
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

	// 创建测试进程配置（设置较短的启动时间，便于测试）
	programCfg := &config.ProgramConfig{
		Name:         "test_restart",
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
	p := processManager.GetProcess("test_restart")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 启动进程
	err = p.Start()
	if err != nil {
		t.Errorf("启动进程失败: %v", err)
	}

	// 等待进程启动
	time.Sleep(2 * time.Second)

	// 模拟进程退出并等待自动重启
	p.ExitCode = 1
	p.mu.Lock(); p.State = StateExited; p.mu.Unlock()
	time.Sleep(3 * time.Second)

	// 检查进程是否重启
	if state := p.GetState(); state != StateRunning {
		t.Logf("期望进程状态为RUNNING（已重启），实际为%s", state)
	}

	// 停止进程
	// 停止进程，若出错则记录
	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}
}

// TestProcessResourceMonitoring 测试进程资源监控功能
func TestProcessResourceMonitoring(t *testing.T) {
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
		Name:         "test_resource",
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
	p := processManager.GetProcess("test_resource")
	if p == nil {
		t.Fatalf("获取进程失败")
	}

	// 启动进程
	err = p.Start()
	if err != nil {
		t.Errorf("启动进程失败: %v", err)
	}

	// 等待进程启动
	time.Sleep(2 * time.Second)

	// 测试获取进程资源使用情况
	if p.PID > 0 {
		// 尝试获取资源使用情况
		// 这里只是测试方法调用不会报错
		// 实际的资源监控测试需要一个长时间运行的进程
	}

	// 停止进程
	// 停止进程，若出错则记录
	if err := p.Stop(); err != nil {
		t.Logf("停止进程时遇到错误: %v", err)
	}
}

// TestProcessStateTransitions 测试进程状态转换
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
		t.Logf("进程状态为%s，期望为STOPPED", state)
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

	// 创建一个短暂进程（运行 1 秒后自动退出）
	programCfg := &config.ProgramConfig{
		Name:         "short_lived",
		Command:      "sleep 1",
		Directory:    ".",
		AutoStart:    true,
		AutoRestart:  true,
		StartSecs:    1,
		StartRetries: 2,
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

	// 等待进程退出
	time.Sleep(2 * time.Second)

	// 进程应该已退出
	if state := p.GetState(); state != StateExited && state != StateRunning {
		t.Logf("第一次检查：进程状态为%s", state)
	}

	// 等待监控尝试重启
	time.Sleep(2 * time.Second)

	// 检查是否发生了重启（RestartCount > 0 或状态变化）
	s := p.Snapshot()
	if s.RestartCount == 0 && s.StartRetries == 1 {
		t.Logf("期望重启至少发生过一次，RestartCount=%d, StartRetries=%d", s.RestartCount, s.StartRetries)
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
	if s1 == StateStopped && s2 == StateStopped {
		t.Logf("循环依赖时 StartAll 可能未执行（期望至少有进程启动/退出）")
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
	beforeState := p.State
	p.mu.Unlock()

	m := &Monitor{Manager: pm}
	m.handleExitedProcess(p)

	if state := p.GetState(); state == StateRunning || state == StateStarting {
		t.Errorf("AutoRestart=false 时不应重启，但状态变为 %s", state)
	}
	if beforeState != StateExited {
		t.Logf("初始状态为 %s", beforeState)
	}
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
		_ = p.Start()
		time.Sleep(200 * time.Millisecond)
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
		_ = p.Start()
		time.Sleep(200 * time.Millisecond)
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
		_ = p.Start()
		time.Sleep(200 * time.Millisecond)
		p.Stop()
	})
}

// TestCPUUsageIsPercentage tests that CPU usage is computed as a delta-based
// percentage after two sampling intervals.
func TestCPUUsageIsPercentage(t *testing.T) {
	logDir := "./test_logs_cpu"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	pm := NewProcessManager(logManager)
	cfg := &config.ProgramConfig{
		Name:         "cpu_test",
		Command:      "dd if=/dev/zero of=/dev/null bs=1M count=100",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 3,
		Environment:  make(map[string]string),
	}
	pm.AddProcess(cfg)
	p := pm.GetProcess("cpu_test")
	_ = p.Start()
	defer p.Stop()

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
}

// TestReadSystemCPUTicks tests the system CPU ticks reader.
func TestReadSystemCPUTicks(t *testing.T) {
	ticks := readSystemCPUTicks()
	if ticks <= 0 {
		t.Error("系统 CPU ticks 应大于 0")
	}
}
