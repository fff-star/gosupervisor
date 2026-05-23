package metrics

import (
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// 初始化测试环境
func setupMetricsTestEnvironment(t *testing.T) *process.ProcessManager {
	t.Helper()
	logManager, err := logger.NewDefaultLogger(t.TempDir())
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	t.Cleanup(func() { logManager.Close() })

	processManager := process.NewProcessManager(logManager)
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
	processManager.AddProcess(programCfg)
	return processManager
}

// TestNewMetricsManager 测试创建指标管理器
func TestNewMetricsManager(t *testing.T) {
	// 初始化测试环境
	processManager := setupMetricsTestEnvironment(t)

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
	processManager := setupMetricsTestEnvironment(t)

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
	processManager := setupMetricsTestEnvironment(t)

	metricsManager := NewMetricsManager(processManager)

	// Start a process so there's something to measure
	p := processManager.GetProcess("test_process")
	if p == nil {
		t.Fatal("test_process not found")
	}
	_ = p.Start()
	time.Sleep(500 * time.Millisecond)
	defer func() { _ = p.Stop() }()

	// UpdateMetrics should not panic
	metricsManager.UpdateMetrics()

	// processCount gauge should be registered and non-nil
	if metricsManager.processCount == nil {
		t.Error("processCount gauge is nil after UpdateMetrics")
	}
	if metricsManager.processStatus == nil {
		t.Error("processStatus gauge is nil after UpdateMetrics")
	}
	if metricsManager.processCPUUsage == nil {
		t.Error("processCPUUsage gauge is nil after UpdateMetrics")
	}
	if metricsManager.processMemUsage == nil {
		t.Error("processMemUsage gauge is nil after UpdateMetrics")
	}
}

// TestStartMetricsCollector 测试启动指标收集器
func TestStartMetricsCollector(t *testing.T) {
	processManager := setupMetricsTestEnvironment(t)

	metricsManager := NewMetricsManager(processManager)

	// StartMetricsCollector should not panic
	metricsManager.StartMetricsCollector(100 * time.Millisecond)

	// Let it tick at least once
	time.Sleep(300 * time.Millisecond)
	metricsManager.Stop()

	// Verify we can start again (no double-start issues)
	metricsManager.StartMetricsCollector(100 * time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	metricsManager.Stop()
}

// TestGetRegistry 测试获取注册表
func TestGetRegistry(t *testing.T) {
	// 初始化测试环境
	processManager := setupMetricsTestEnvironment(t)

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
	processManager := setupMetricsTestEnvironment(t)

	metricsManager := NewMetricsManager(processManager)

	// Start a process
	p := processManager.GetProcess("test_process")
	if p == nil {
		t.Fatal("test_process not found")
	}
	_ = p.Start()
	defer func() { _ = p.Stop() }()
	time.Sleep(300 * time.Millisecond)

	// Run collector and update metrics
	metricsManager.StartMetricsCollector(200 * time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	metricsManager.UpdateMetrics()
	metricsManager.Stop()

	// Verify registry is still accessible after stop
	if metricsManager.GetRegistry() == nil {
		t.Error("registry should not be nil after stop")
	}
}

// TestMetricsStopCollector tests that the collector can be stopped cleanly.
func TestMetricsStopCollector(t *testing.T) {
	processManager := setupMetricsTestEnvironment(t)

	mm := NewMetricsManager(processManager)
	mm.StartMetricsCollector(100 * time.Millisecond)

	time.Sleep(300 * time.Millisecond)
	mm.Stop()

	// Verify stop channel is closed (Stop is complete)
	if mm.stop != nil {
		select {
		case <-mm.stop:
			// Expected: channel is closed
		default:
			t.Error("stop channel should be closed after Stop()")
		}
	}
}

// TestMetricsStopDoubleClose verifies Stop is safe to call multiple times.
func TestMetricsStopDoubleClose(t *testing.T) {
	processManager := setupMetricsTestEnvironment(t)

	mm := NewMetricsManager(processManager)
	mm.StartMetricsCollector(100 * time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	// Should not panic when called multiple times.
	mm.Stop()
	mm.Stop()
	mm.Stop()
}

func TestRecordConfigReload(t *testing.T) {
	processManager := setupMetricsTestEnvironment(t)
	mm := NewMetricsManager(processManager)

	mm.RecordConfigReload()
	mm.RecordConfigReload()

	metrics, err := mm.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	found := false
	for _, mf := range metrics {
		if mf.GetName() == "gosupervisor_config_reloads_total" {
			if len(mf.Metric) > 0 && mf.Metric[0].Counter != nil {
				if mf.Metric[0].Counter.GetValue() != 2 {
					t.Errorf("expected config_reloads_total=2, got %v", mf.Metric[0].Counter.GetValue())
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("config_reloads_total metric not found")
	}
}

func TestResetTracking(t *testing.T) {
	processManager := setupMetricsTestEnvironment(t)
	mm := NewMetricsManager(processManager)

	mm.prevRestarts["test"] = 5
	mm.prevStartCounts["test"] = 3

	mm.ResetTracking()

	if len(mm.prevRestarts) != 0 {
		t.Errorf("prevRestarts should be empty after ResetTracking, got %d entries", len(mm.prevRestarts))
	}
	if len(mm.prevStartCounts) != 0 {
		t.Errorf("prevStartCounts should be empty after ResetTracking, got %d entries", len(mm.prevStartCounts))
	}
}

func TestRecordHealthCheckFailure(t *testing.T) {
	processManager := setupMetricsTestEnvironment(t)
	mm := NewMetricsManager(processManager)

	mm.RecordHealthCheckFailure("test_process")
	mm.RecordHealthCheckFailure("test_process")

	metrics, err := mm.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range metrics {
		if mf.GetName() == "gosupervisor_healthcheck_failures_total" {
			for _, m := range mf.Metric {
				for _, l := range m.Label {
					if l.GetName() == "name" && l.GetValue() == "test_process" {
						if m.Counter.GetValue() != 2 {
							t.Errorf("expected healthcheck_failures_total=2, got %v", m.Counter.GetValue())
						}
					}
				}
			}
		}
	}
}
