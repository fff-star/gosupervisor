package metrics

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
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
	processCount         prometheus.Gauge
	processStatus        *prometheus.GaugeVec
	processUptime        *prometheus.GaugeVec
	processRestarts      *prometheus.CounterVec
	processCPUUsage      *prometheus.GaugeVec
	processMemUsage      *prometheus.GaugeVec
	healthCheckStatus    *prometheus.GaugeVec
	healthCheckFailures  *prometheus.CounterVec
	supervisorUptime     prometheus.Gauge
	supervisorGoroutines prometheus.Gauge
	supervisorMemory     prometheus.Gauge
	configReloads        prometheus.Counter
	processLogSize       *prometheus.GaugeVec
	healthCheckLastOK    *prometheus.GaugeVec
	processStartCount    *prometheus.CounterVec

	// Track previous restart counts for CounterVec delta
	prevRestarts    map[string]float64
	prevStartCounts map[string]float64
	startTime       time.Time

	// 内部状态
	mu   sync.Mutex
	stop chan struct{}
}

// NewMetricsManager 创建新的指标管理器
func NewMetricsManager(processManager *process.ProcessManager) *MetricsManager {
	registry := prometheus.NewRegistry()

	mm := &MetricsManager{
		processManager:  processManager,
		registry:        registry,
		prevRestarts:    make(map[string]float64),
		prevStartCounts: make(map[string]float64),
		startTime:       time.Now(),
		stop:            make(chan struct{}),
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

	// 进程重启次数 (CounterVec for proper rate() support)
	mm.processRestarts = prometheus.NewCounterVec(prometheus.CounterOpts{
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

	// 健康检查状态 (1=healthy, 0=unhealthy)
	mm.healthCheckStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_healthcheck_status",
		Help: "进程健康检查状态 (1=healthy, 0=unhealthy)",
	}, []string{"name"})

	// 健康检查失败次数
	mm.healthCheckFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gosupervisor_healthcheck_failures_total",
		Help: "进程健康检查失败总次数",
	}, []string{"name"})

	// Supervisor 运行时间
	mm.supervisorUptime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gosupervisor_uptime_seconds",
		Help: "Supervisor自身运行时间（秒）",
	})

	// Supervisor goroutine 数量
	mm.supervisorGoroutines = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gosupervisor_goroutines",
		Help: "当前goroutine数量",
	})

	// Supervisor 内存使用量
	mm.supervisorMemory = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gosupervisor_memory_bytes",
		Help: "Supervisor自身内存使用量（字节）",
	})

	// 配置重载次数
	mm.configReloads = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gosupervisor_config_reloads_total",
		Help: "配置重载总次数",
	})

	// 进程日志大小
	mm.processLogSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_process_log_size_bytes",
		Help: "进程日志文件大小（字节）",
	}, []string{"name"})

	// 上次健康检查成功时间戳
	mm.healthCheckLastOK = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gosupervisor_healthcheck_last_success_timestamp",
		Help: "进程上次健康检查成功的Unix时间戳",
	}, []string{"name"})

	// 进程启动总次数（包括首次启动和重启）
	mm.processStartCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gosupervisor_process_start_count_total",
		Help: "进程启动总次数（包括首次启动和重启）",
	}, []string{"name"})

	// 注册到注册表
	mm.registry.MustRegister(
		mm.processCount,
		mm.processStatus,
		mm.processUptime,
		mm.processRestarts,
		mm.processCPUUsage,
		mm.processMemUsage,
		mm.healthCheckStatus,
		mm.healthCheckFailures,
		mm.supervisorUptime,
		mm.supervisorGoroutines,
		mm.supervisorMemory,
		mm.configReloads,
		mm.processLogSize,
		mm.healthCheckLastOK,
		mm.processStartCount,
	)
}

// UpdateMetrics 更新所有指标
func (mm *MetricsManager) UpdateMetrics() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.processCount.Set(float64(mm.processManager.Len()))

	// Supervisor self metrics
	mm.supervisorUptime.Set(time.Since(mm.startTime).Seconds())
	mm.supervisorGoroutines.Set(float64(runtime.NumGoroutine()))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mm.supervisorMemory.Set(float64(m.Alloc))

	mm.processManager.RangeProcesses(func(name string, proc *process.Process) {
		s := proc.Snapshot()

		var status float64
		switch s.State {
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

		if s.State == process.StateRunning {
			uptime := time.Since(s.StartTime).Seconds()
			mm.processUptime.WithLabelValues(name).Set(uptime)
		} else {
			mm.processUptime.WithLabelValues(name).Set(0)
		}

		// CounterVec: increment by delta from previous value
		current := float64(s.RestartCount)
		prev := mm.prevRestarts[name]
		if current > prev {
			delta := current - prev
			mm.processRestarts.WithLabelValues(name).Add(delta)
		}
		mm.prevRestarts[name] = current

		mm.processCPUUsage.WithLabelValues(name).Set(s.CPUUsage)
		mm.processMemUsage.WithLabelValues(name).Set(float64(s.MemoryUsage))

		// Health check metrics
		if s.Healthy {
			mm.healthCheckStatus.WithLabelValues(name).Set(1)
			mm.healthCheckLastOK.WithLabelValues(name).Set(float64(time.Now().Unix()))
		} else {
			mm.healthCheckStatus.WithLabelValues(name).Set(0)
		}

		// Process start count (delta-based CounterVec)
		currentStarts := float64(s.RestartCount + 1) // initial start + restarts
		prevStarts := mm.prevStartCounts[name]
		if currentStarts > prevStarts {
			mm.processStartCount.WithLabelValues(name).Add(currentStarts - prevStarts)
		}
		mm.prevStartCounts[name] = currentStarts

		// Log size: approximate from configured log path
		if s.Config.StdoutLogFile != "" {
			if fi, err := os.Stat(s.Config.StdoutLogFile); err == nil {
				mm.processLogSize.WithLabelValues(name).Set(float64(fi.Size()))
			}
		}
	})
}

// RecordConfigReload increments the config reload counter.
func (mm *MetricsManager) RecordConfigReload() {
	mm.configReloads.Inc()
}

// ResetTracking clears the delta-tracking maps. Call on config reload to avoid
// stale previous values blocking counter increments for processes with the same name.
func (mm *MetricsManager) ResetTracking() {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.prevRestarts = make(map[string]float64)
	mm.prevStartCounts = make(map[string]float64)
}

// RecordHealthCheckFailure records a health check failure for a process.
func (mm *MetricsManager) RecordHealthCheckFailure(name string) {
	mm.healthCheckFailures.WithLabelValues(name).Inc()
}

// StartMetricsServer 启动指标服务器
func (mm *MetricsManager) StartMetricsServer(addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(mm.registry, promhttp.HandlerOpts{}))
	fmt.Printf("Prometheus指标服务器启动在 %s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// StartMetricsCollector 启动指标收集器
func (mm *MetricsManager) StartMetricsCollector(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mm.UpdateMetrics()
			case <-mm.stop:
				return
			}
		}
	}()
}

// Stop stops the metrics collector.
func (mm *MetricsManager) Stop() {
	close(mm.stop)
}

// GetRegistry 获取Prometheus注册表
func (mm *MetricsManager) GetRegistry() *prometheus.Registry {
	return mm.registry
}
