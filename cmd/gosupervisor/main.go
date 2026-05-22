package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/eventlistener"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/metrics"
	"gosupervisor/internal/process"
	"gosupervisor/internal/socket"
	"gosupervisor/internal/web"
)

const (
	// 版本信息
	Version = "1.0.0"
	// 程序名称
	ProgramName = "gosupervisor"
)

// metricsManagerRef holds the current metrics manager for use by reloadConfiguration.
var metricsManagerRef *metrics.MetricsManager

// eventListenerManagerRef holds the current event listener manager for use by reloadConfiguration.
var eventListenerManagerRef *eventlistener.EventListenerManager

func main() {
	var exitCode int
	defer func() { os.Exit(exitCode) }()

	// 解析命令行参数
	configPath := flag.String("c", "gosupervisor.ini", "配置文件路径")
	logDir := flag.String("l", "./logs", "日志目录路径")
	command := flag.String("cmd", "start", "命令: start, stop, restart, status, reload, update")
	processName := flag.String("p", "", "进程名称")
	groupName := flag.String("g", "", "进程组名称")
	webEnable := flag.Bool("web", false, "启用Web界面")
	webAddr := flag.String("web-addr", ":8080", "Web界面地址")
	webUser := flag.String("web-user", "", "Web界面HTTP Basic Auth用户名")
	webPass := flag.String("web-pass", "", "Web界面HTTP Basic Auth密码")
	webAPIAuth := flag.Bool("web-api-auth", true, "对API v1启用HTTP Basic Auth认证（默认开启）")
	webCert := flag.String("web-cert", "", "TLS证书文件路径")
	webKey := flag.String("web-key", "", "TLS私钥文件路径")
	metricsEnable := flag.Bool("metrics", false, "启用Prometheus指标导出")
	metricsAddr := flag.String("metrics-addr", ":9090", "Prometheus指标导出地址")
	socketPath := flag.String("socket", "", "Unix socket CLI 路径")
	daemonMode := flag.Bool("d", false, "以守护进程模式运行")
	stateFile := flag.String("state-file", "", "持久化状态文件路径")
	testConfig := flag.Bool("t", false, "测试配置文件有效性")
	version := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	// 处理版本信息
	if *version {
		printVersion()
		os.Exit(0)
	}

	// 处理配置文件测试
	if *testConfig {
		cfg, err := config.LoadConfig(*configPath)
		if err != nil {
			fmt.Printf("配置文件测试失败: %v\n", err)
			exitCode = 1
			return
		}
		warnings := cfg.ValidateConfig()
		if len(warnings) > 0 {
			fmt.Println("配置警告:")
			for _, w := range warnings {
				fmt.Printf("  - %s\n", w)
			}
			exitCode = 1
			return
		}
		fmt.Println("配置文件测试通过!")
		os.Exit(0)
	}

	// 加载配置文件
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("加载配置文件失败: %v\n", err)
		exitCode = 1
		return
	}

	// Validate config
	for _, warn := range cfg.ValidateConfig() {
		fmt.Printf("配置警告: %s\n", warn)
	}

	// Apply server config from [supervisord] section (CLI flags take precedence)
	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { explicitFlags[f.Name] = true })
	if cfg.Server != nil {
		if !explicitFlags["web-addr"] && cfg.Server.WebAddr != "" {
			*webAddr = cfg.Server.WebAddr
		}
		if !explicitFlags["web-user"] && cfg.Server.WebUser != "" {
			*webUser = cfg.Server.WebUser
		}
		if !explicitFlags["web-pass"] && cfg.Server.WebPass != "" {
			*webPass = cfg.Server.WebPass
		}
		if !explicitFlags["metrics-addr"] && cfg.Server.MetricsAddr != "" {
			*metricsAddr = cfg.Server.MetricsAddr
		}
		if !explicitFlags["socket"] && cfg.Server.SocketPath != "" {
			*socketPath = cfg.Server.SocketPath
		}
		if !explicitFlags["state-file"] && cfg.Server.StateFile != "" {
			*stateFile = cfg.Server.StateFile
		}
		if !explicitFlags["l"] && cfg.Server.LogDir != "" {
			*logDir = cfg.Server.LogDir
		}
		if !explicitFlags["web-cert"] && cfg.Server.WebCert != "" {
			*webCert = cfg.Server.WebCert
		}
		if !explicitFlags["web-key"] && cfg.Server.WebKey != "" {
			*webKey = cfg.Server.WebKey
		}
	}

	// 处理守护进程模式
	if *daemonMode {
		if err := runAsDaemon(); err != nil {
			fmt.Printf("启动守护进程失败: %v\n", err)
			exitCode = 1
			return
		}
	}

	// 初始化日志管理器
	logManager, err := logger.NewDefaultLogger(*logDir)
	if err != nil {
		fmt.Printf("初始化日志管理器失败: %v\n", err)
		exitCode = 1
		return
	}
	defer logManager.Close()

	// 初始化进程管理器
	processManager := process.NewProcessManager(logManager)

	// 初始化信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// done channel for clean shutdown (avoids os.Exit bypassing defers)
	done := make(chan struct{})

	// 启动信号处理协程
	go handleSignals(sigChan, done, processManager, configPath, logManager, stateFile)

	// 添加进程
	for _, programCfg := range cfg.Programs {
		for _, expandedCfg := range config.ExpandProgramConfig(programCfg) {
			processManager.AddProcess(expandedCfg)
		}
	}

	// Restore persistent state if configured
	if *stateFile != "" {
		if err := processManager.RestoreState(*stateFile); err != nil {
			fmt.Printf("恢复进程状态失败: %v\n", err)
		}
	}

	// 初始化监控器
	monitor := process.NewMonitor(processManager)
	monitor.Start()
	defer monitor.Stop()

	// Initialize event listener manager if configured
	var eventListenerManager *eventlistener.EventListenerManager
	// Wire SSE broker (always on when web is enabled)
	if *webEnable {
		web.InitSSEBroker()
	}
	if len(cfg.EventListeners) > 0 {
		eventListenerManager = eventlistener.NewManager(cfg, logManager.Info)
		eventListenerManager.Start()
		process.SetOnEvent(eventListenerManager.EmitEvent)
		eventListenerManagerRef = eventListenerManager
		// Re-wire SSE after event listener manager to maintain chain
		if *webEnable {
			web.InitSSEBroker()
		}
	}

	// Deferred cleanup for web server, event listeners, and metrics manager
	var webServer *web.WebServer
	defer func() {
		if webServer != nil {
			webServer.Stop()
		}
		if eventListenerManager != nil {
			eventListenerManager.Stop()
			process.SetOnEvent(nil)
		}
		if metricsManagerRef != nil {
			metricsManagerRef.Stop()
		}
	}()

	// 处理命令
	switch *command {
	case "start":
		if *groupName != "" {
			started := processManager.StartGroup(*groupName)
			fmt.Printf("组 %s 中启动了 %d 个进程\n", *groupName, len(started))
		} else if *processName != "" {
			// 启动指定进程
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				exitCode = 1
				return
			}
			if err := p.Start(); err != nil {
				fmt.Printf("启动进程 %s 失败: %v\n", *processName, err)
				exitCode = 1
				return
			}
			fmt.Printf("进程 %s 启动成功\n", *processName)
		} else {
			// 启动所有进程
			processManager.StartAll()
			fmt.Println("所有进程启动成功")

			// 如果启用了Unix socket CLI
			if *socketPath != "" {
				socketServer := socket.NewSocketServer(processManager)
				if err := socketServer.Start(*socketPath); err != nil {
					fmt.Printf("启动Unix socket CLI失败: %v\n", err)
					exitCode = 1
					return
				}
				defer socketServer.Stop()
			}

			// 如果启用了Web界面，启动Web服务器
			if *webEnable {
				corsOrigin := ""
				rateLimitRPS := 0
				if cfg.Server != nil {
					if cfg.Server.CORSOrigin != "" {
						corsOrigin = cfg.Server.CORSOrigin
					}
					if cfg.Server.RateLimitRPS > 0 {
						rateLimitRPS = cfg.Server.RateLimitRPS
					}
				}
				var err error
				webServer, err = web.NewWebServerFullTLS(processManager, *logDir, *webUser, *webPass, *webAPIAuth, corsOrigin, rateLimitRPS, *webCert, *webKey)
				if err != nil {
					fmt.Printf("初始化Web服务器失败: %v\n", err)
					exitCode = 1
					return
				}

				// 在goroutine中启动Web服务器
				go func() {
					if err := webServer.Start(*webAddr); err != nil {
						fmt.Printf("启动Web服务器失败: %v\n", err)
					}
				}()
			}

			// 如果启用了Prometheus指标导出
			if *metricsEnable {
				// 创建指标管理器
				metricsManagerRef = metrics.NewMetricsManager(processManager)

				// Wire health check failure callback
				processManager.RangeProcesses(func(name string, p *process.Process) {
					p.OnHealthCheckFailure = metricsManagerRef.RecordHealthCheckFailure
				})

				// 启动指标收集器
				metricsManagerRef.StartMetricsCollector(5 * time.Second)

				// 在goroutine中启动指标服务器
				go func() {
					if err := metricsManagerRef.StartMetricsServer(*metricsAddr); err != nil {
						fmt.Printf("启动指标服务器失败: %v\n", err)
					}
				}()
			}

			// 保持运行
			<-done
		}
	case "stop":
		if *groupName != "" {
			stopped := processManager.StopGroup(*groupName)
			fmt.Printf("组 %s 中停止了 %d 个进程\n", *groupName, len(stopped))
		} else if *processName != "" {
			// 停止指定进程
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				exitCode = 1
				return
			}
			if err := p.Stop(); err != nil {
				fmt.Printf("停止进程 %s 失败: %v\n", *processName, err)
				exitCode = 1
				return
			}
			fmt.Printf("进程 %s 停止成功\n", *processName)
		} else {
			// 停止所有进程
			processManager.StopAll()
			fmt.Println("所有进程停止成功")
		}
	case "restart":
		if *groupName != "" {
			restarted := processManager.RestartGroup(*groupName)
			fmt.Printf("组 %s 中重启了 %d 个进程\n", *groupName, len(restarted))
		} else if *processName != "" {
			// 重启指定进程
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				exitCode = 1
				return
			}
			if err := p.Restart(); err != nil {
				fmt.Printf("重启进程 %s 失败: %v\n", *processName, err)
				exitCode = 1
				return
			}
			fmt.Printf("进程 %s 重启成功\n", *processName)
		} else {
			// 重启所有进程
			processManager.RangeProcesses(func(name string, p *process.Process) {
				if err := p.Restart(); err != nil {
					fmt.Printf("重启进程 %s 失败: %v\n", name, err)
				}
			})
			fmt.Println("所有进程重启成功")
		}
	case "status":
		// 显示进程状态
		if *processName != "" {
			// 显示指定进程状态
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				exitCode = 1
				return
			}
			printProcessStatus(p)
		} else {
			// 显示所有进程状态
			processManager.RangeProcesses(func(name string, p *process.Process) {
				printProcessStatus(p)
				fmt.Println()
			})
		}
	case "reload":
		// 重新加载配置
		if err := reloadConfiguration(processManager, *configPath, logManager); err != nil {
			fmt.Printf("重新加载配置失败: %v\n", err)
			exitCode = 1
			return
		}
		fmt.Println("配置重新加载成功")
	case "update":
		// 更新进程配置
		if *processName != "" {
			// 更新指定进程
			if err := updateProcessConfig(processManager, *configPath, *processName, logManager); err != nil {
				fmt.Printf("更新进程配置失败: %v\n", err)
				exitCode = 1
				return
			}
			fmt.Printf("进程 %s 配置更新成功\n", *processName)
		} else {
			fmt.Println("更新命令需要指定进程名称")
			exitCode = 1
			return
		}
	default:
		fmt.Printf("未知命令: %s\n", *command)
		exitCode = 1
		return
	}
}

func printProcessStatus(p *process.Process) {
	s := p.Snapshot()
	fmt.Printf("进程名称: %s\n", s.Name)
	fmt.Printf("状态: %s\n", s.State)
	if s.PID > 0 {
		fmt.Printf("PID: %d\n", s.PID)
	}
	if !s.StartTime.IsZero() {
		fmt.Printf("启动时间: %s\n", s.StartTime.Format("2006-01-02 15:04:05"))
	}
	if !s.StopTime.IsZero() {
		fmt.Printf("停止时间: %s\n", s.StopTime.Format("2006-01-02 15:04:05"))
	}
	if s.ExitCode != 0 {
		fmt.Printf("退出码: %d\n", s.ExitCode)
	}
	fmt.Printf("启动重试次数: %d\n", s.RestartCount)
}

// printVersion 打印版本信息
func printVersion() {
	fmt.Printf("%s version %s\n", ProgramName, Version)
	fmt.Println("Go-based process supervisor")
	fmt.Println("Copyright (c) 2024 gosupervisor team")
}

// runAsDaemon 以守护进程模式运行
func runAsDaemon() error {
	// Already daemonized (parent is init)
	if os.Getppid() == 1 {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %v", err)
	}

	// Strip -d flag to prevent infinite re-daemonization
	args := make([]string, 0, len(os.Args))
	for _, a := range os.Args[1:] {
		if a == "-d" {
			continue
		}
		args = append(args, a)
	}

	attr := &os.ProcAttr{
		Dir:   "/",
		Files: []*os.File{nil, nil, nil},
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	}

	child, err := os.StartProcess(exe, append([]string{exe}, args...), attr)
	if err != nil {
		return fmt.Errorf("fork 子进程失败: %v", err)
	}

	fmt.Printf("守护进程已启动，PID: %d\n", child.Pid)
	os.Exit(0)
	return nil
}

// handleSignals 处理系统信号
func handleSignals(sigChan chan os.Signal, done chan struct{}, processManager *process.ProcessManager, configPath *string, logManager *logger.Logger, stateFile *string) {
	var doneOnce sync.Once
	for {
		sig := <-sigChan
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			// 处理终止信号
			logManager.Info("收到终止信号，正在停止所有进程...")
			processManager.StopAll()
			if *stateFile != "" {
				processManager.SaveState(*stateFile)
			}
			logManager.Info("所有进程已停止，正在退出...")
			doneOnce.Do(func() { close(done) })
			return
		case syscall.SIGHUP:
			// 处理重载信号
			logManager.Info("收到重载信号，正在重新加载配置...")
			if err := reloadConfiguration(processManager, *configPath, logManager); err != nil {
				logManager.Error("重新加载配置失败: %v", err)
			} else {
				logManager.Info("配置重新加载成功")
			}
		}
	}
}

// reloadConfiguration 重新加载配置文件
func reloadConfiguration(processManager *process.ProcessManager, configPath string, logManager *logger.Logger) error {
	// 加载新配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}

	// Build expanded config map
	newConfigs := make(map[string]*config.ProgramConfig)
	for _, programCfg := range cfg.Programs {
		for _, expandedCfg := range config.ExpandProgramConfig(programCfg) {
			newConfigs[expandedCfg.Name] = expandedCfg
		}
	}

	// Diff against current processes
	added, removed, modified := processManager.CompareConfigs(newConfigs)

	// Stop and remove processes that no longer exist
	for _, name := range removed {
		if p := processManager.GetProcess(name); p != nil {
			_ = p.Stop()
			if p.CancelFunc != nil {
				p.CancelFunc()
			}
		}
		processManager.RemoveProcess(name)
		logManager.Info("进程 %s 已移除", name)
	}

	// Stop, remove, re-add, and restart modified processes
	for _, name := range modified {
		if p := processManager.GetProcess(name); p != nil {
			_ = p.Stop()
			if p.CancelFunc != nil {
				p.CancelFunc()
			}
		}
		processManager.RemoveProcess(name)
		processManager.AddProcess(newConfigs[name])
		if newConfigs[name].AutoStart {
			if newP := processManager.GetProcess(name); newP != nil {
				_ = newP.Start()
			}
		}
		logManager.Info("进程 %s 已修改", name)
	}

	// Add and start new processes
	for _, name := range added {
		processManager.AddProcess(newConfigs[name])
		if newConfigs[name].AutoStart {
			if newP := processManager.GetProcess(name); newP != nil {
				_ = newP.Start()
			}
		}
		logManager.Info("进程 %s 已添加", name)
	}

	if len(added) == 0 && len(removed) == 0 && len(modified) == 0 {
		logManager.Info("配置重载完成: 无变更")
	}

	// Re-wire metrics callback
	if metricsManagerRef != nil {
		processManager.RangeProcesses(func(name string, p *process.Process) {
			p.OnHealthCheckFailure = metricsManagerRef.RecordHealthCheckFailure
		})
		metricsManagerRef.ResetTracking()
		metricsManagerRef.RecordConfigReload()
	}

	// Reload event listeners
	if eventListenerManagerRef != nil {
		eventListenerManagerRef.Reload(cfg)
		process.SetOnEvent(eventListenerManagerRef.EmitEvent)
	}
	// Re-wire SSE broker after reload (BUGFIX: SSE was lost after SIGHUP)
	web.InitSSEBroker()

	return nil
}

// updateProcessConfig 更新指定进程的配置
func updateProcessConfig(processManager *process.ProcessManager, configPath string, processName string, logManager *logger.Logger) error {
	// 加载新配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}

	// 查找指定进程的配置
	var programCfg *config.ProgramConfig
	for _, cfg := range cfg.Programs {
		if cfg.Name == processName {
			programCfg = cfg
			break
		}
	}

	if programCfg == nil {
		return fmt.Errorf("进程 %s 的配置不存在", processName)
	}

	// 停止指定进程
	p := processManager.GetProcess(processName)
	if p != nil {
		if err := p.Stop(); err != nil {
			logManager.Warning("停止进程 %s 失败: %v", processName, err)
		}
	}

	// 移除旧进程
	processManager.RemoveProcess(processName)

	// 添加新进程
	for _, expandedCfg := range config.ExpandProgramConfig(programCfg) {
		processManager.AddProcess(expandedCfg)
	}

	// Re-wire metrics callback for updated processes
	if metricsManagerRef != nil {
		for _, expandedCfg := range config.ExpandProgramConfig(programCfg) {
			if p := processManager.GetProcess(expandedCfg.Name); p != nil {
				p.OnHealthCheckFailure = metricsManagerRef.RecordHealthCheckFailure
			}
		}
	}

	// 启动新进程（仅当 AutoStart 为 true 时）
	newProcess := processManager.GetProcess(processName)
	if newProcess != nil && programCfg.AutoStart {
		if err := newProcess.Start(); err != nil {
			return fmt.Errorf("启动进程 %s 失败: %v", processName, err)
		}
	}

	return nil
}
