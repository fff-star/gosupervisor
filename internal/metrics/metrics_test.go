package metrics

import (
	"os"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// 初始化测试环境
func setupMetricsTestEnvironment() (*process.ProcessManager, error) {
	// 创建日志目录
	logDir := "./test_logs"

	// 初始化日志管理器
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		return nil, err
	}

	// 创建进程管理器
	processManager := process.NewProcessManager(logManager)

	// 创建测试进程配置
	programCfg := &config.ProgramConfig{
		Name:        "test_process",
		Command:     "echo \"Hello, World!\"",
		Directory:   ".",
		AutoStart:   true,
		AutoRestart: true,
		StartSecs:   1,
		StartRetries: 3,
		User:        "",
		Environment: make(map[string]string),
	}

	// 添加进程
	processManager.AddProcess(programCfg)

	return processManager, nil
}

// 清理测试环境
func cleanupMetricsTestEnvironment() {
	// 删除测试日志目录
	os.RemoveAll("./test_logs")
}

// TestNewMetricsManager 测试创建指标管理器
func TestNewMetricsManager(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupMetricsTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupMetricsTestEnvironment()

	// 创建指标管理器
	metricsManager := NewMetricsManager(processManager)
	if metricsManager == nil {
		t.Fatalf("创建指标管理器失败")
	}

	// 检查指标管理器是否初始化成功
	if metricsManager.processManager != processManager {
		t.Errorf("进程管理器未正确设置")
	}

	if metricsManager.registry == nil {
		t.Errorf("注册表未正确初始化")
	}
}

// TestRegisterMetrics 测试指标注册
func TestRegisterMetrics(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupMetricsTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupMetricsTestEnvironment()

	// 创建指标管理器
	metricsManager := NewMetricsManager(processManager)

	// 检查指标是否注册成功
	if metricsManager.processCount == nil {
		t.Errorf("processCount 指标未注册")
	}

	if metricsManager.processStatus == nil {
		t.Errorf("processStatus 指标未注册")
	}

	if metricsManager.processUptime == nil {
		t.Errorf("processUptime 指标未注册")
	}

	if metricsManager.processRestarts == nil {
		t.Errorf("processRestarts 指标未注册")
	}

	if metricsManager.processCPUUsage == nil {
		t.Errorf("processCPUUsage 指标未注册")
	}

	if metricsManager.processMemUsage == nil {
		t.Errorf("processMemUsage 指标未注册")
	}
}

// TestUpdateMetrics 测试更新指标
func TestUpdateMetrics(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupMetricsTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupMetricsTestEnvironment()

	// 创建指标管理器
	metricsManager := NewMetricsManager(processManager)

	// 更新指标
	metricsManager.UpdateMetrics()

	// 检查指标是否更新成功
	// 这里我们主要检查方法是否执行成功，不检查具体值
	// 因为具体值依赖于进程状态
	t.Log("指标更新成功")
}

// TestStartMetricsCollector 测试启动指标收集器
func TestStartMetricsCollector(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupMetricsTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupMetricsTestEnvironment()

	// 创建指标管理器
	metricsManager := NewMetricsManager(processManager)

	// 启动指标收集器
	metricsManager.StartMetricsCollector(1 * time.Second)

	// 等待一段时间，确保收集器启动
	time.Sleep(2 * time.Second)

	// 检查收集器是否正常运行
	// 这里我们主要检查方法是否执行成功
	t.Log("指标收集器启动成功")
}

// TestGetRegistry 测试获取注册表
func TestGetRegistry(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupMetricsTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupMetricsTestEnvironment()

	// 创建指标管理器
	metricsManager := NewMetricsManager(processManager)

	// 获取注册表
	registry := metricsManager.GetRegistry()

	if registry == nil {
		t.Errorf("获取注册表失败")
	}

	if registry != metricsManager.registry {
		t.Errorf("获取的注册表与内部注册表不一致")
	}
}

// TestMetricsIntegration 集成测试
func TestMetricsIntegration(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupMetricsTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupMetricsTestEnvironment()

	// 创建指标管理器
	metricsManager := NewMetricsManager(processManager)

	// 启动指标收集器
	metricsManager.StartMetricsCollector(500 * time.Millisecond)

	// 等待指标更新
	time.Sleep(1 * time.Second)

	// 手动更新指标
	metricsManager.UpdateMetrics()

	// 检查指标是否更新成功
	t.Log("集成测试成功")
}
