package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gosupervisor/internal/process"
)

// MetricsManager 指标管理器
type MetricsManager struct {
	processManager *process.ProcessManager
	registry       *prometheus.Registry

	// 指标定义
	processCount    prometheus.Gauge
	processStatus   *prometheus.GaugeVec
	processUptime   *prometheus.GaugeVec
	processRestarts *prometheus.GaugeVec
	processCPUUsage *prometheus.GaugeVec
	processMemUsage *prometheus.GaugeVec

	// 内部状态
	mu sync.Mutex
}

// NewMetricsManager 创建新的指标管理器
func NewMetricsManager(processManager *process.ProcessManager) *MetricsManager {
	registry := prometheus.NewRegistry()

	mm := &MetricsManager{
		processManager: processManager,
		registry:       registry,
	}

	// 注册指标
	mm.registerMetrics()

	return mm
}

// registerMetrics 注册所有指标
func (mm *MetricsManager) registerMetrics() {
	// 进程数量
	mm.processCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gosupervisor_process_count",
		Help: "当前管理的进程总数",
	})

	// 进程状态
	mm.processStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_process_status",
		Help: "进程状态 (0=stopped, 1=starting, 2=running, 3=stopping, 4=exited, 5=fatal)",
	}, []string{"name"})

	// 进程运行时间
	mm.processUptime = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_process_uptime_seconds",
		Help: "进程运行时间（秒）",
	}, []string{"name"})

	// 进程重启次数
	mm.processRestarts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_process_restarts_total",
		Help: "进程重启总次数",
	}, []string{"name"})

	// 进程CPU使用率
	mm.processCPUUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_process_cpu_usage_percent",
		Help: "进程CPU使用率（百分比）",
	}, []string{"name"})

	// 进程内存使用量
	mm.processMemUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_process_memory_usage_bytes",
		Help: "进程内存使用量（字节）",
	}, []string{"name"})

	// 注册到注册表
	mm.registry.MustRegister(
		mm.processCount,
		mm.processStatus,
		mm.processUptime,
		mm.processRestarts,
		mm.processCPUUsage,
		mm.processMemUsage,
	)
}

// UpdateMetrics 更新所有指标
func (mm *MetricsManager) UpdateMetrics() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 更新进程数量
	mm.processCount.Set(float64(len(mm.processManager.Processes)))

	// 更新每个进程的指标
	for name, proc := range mm.processManager.Processes {
		// 进程状态
		var status float64
		switch proc.State {
		case process.StateStopped:
			status = 0
		case process.StateStarting:
			status = 1
		case process.StateRunning:
			status = 2
		case process.StateStopping:
			status = 3
		case process.StateExited:
			status = 4
		case process.StateFatal:
			status = 5
		}
		mm.processStatus.WithLabelValues(name).Set(status)

		// 进程运行时间
		if proc.State == process.StateRunning {
			uptime := time.Since(proc.StartTime).Seconds()
			mm.processUptime.WithLabelValues(name).Set(uptime)
		} else {
			mm.processUptime.WithLabelValues(name).Set(0)
		}

		// 进程重启次数
		mm.processRestarts.WithLabelValues(name).Set(float64(proc.RestartCount))

		// 进程CPU使用率
		mm.processCPUUsage.WithLabelValues(name).Set(proc.CPUUsage)

		// 进程内存使用量
		mm.processMemUsage.WithLabelValues(name).Set(float64(proc.MemoryUsage))
	}
}

// StartMetricsServer 启动指标服务器
func (mm *MetricsManager) StartMetricsServer(addr string) error {
	// 注册HTTP处理器
	http.Handle("/metrics", promhttp.HandlerFor(mm.registry, promhttp.HandlerOpts{}))

	// 启动HTTP服务器
	fmt.Printf("Prometheus指标服务器启动在 %s\n", addr)
	return http.ListenAndServe(addr, nil)
}

// StartMetricsCollector 启动指标收集器
func (mm *MetricsManager) StartMetricsCollector(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			<-ticker.C
			mm.UpdateMetrics()
		}
	}()
}

// GetRegistry 获取Prometheus注册表
func (mm *MetricsManager) GetRegistry() *prometheus.Registry {
	return mm.registry
}
