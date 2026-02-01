package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"
	"os/signal"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/metrics"
	"gosupervisor/internal/process"
	"gosupervisor/internal/web"
)

const (
	// 版本信息
	Version = "1.0.0"
	// 程序名称
	ProgramName = "gosupervisor"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("c", "gosupervisor.ini", "配置文件路径")
	logDir := flag.String("l", "./logs", "日志目录路径")
	command := flag.String("cmd", "start", "命令: start, stop, restart, status, reload, update")
	processName := flag.String("p", "", "进程名称")
	webEnable := flag.Bool("web", false, "启用Web界面")
	webAddr := flag.String("web-addr", ":8080", "Web界面地址")
	metricsEnable := flag.Bool("metrics", false, "启用Prometheus指标导出")
	metricsAddr := flag.String("metrics-addr", ":9090", "Prometheus指标导出地址")
	daemonMode := flag.Bool("d", false, "以守护进程模式运行")
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
		if err := testConfiguration(*configPath); err != nil {
			fmt.Printf("配置文件测试失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("配置文件测试通过!")
		os.Exit(0)
	}

	// 加载配置文件
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("加载配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 处理守护进程模式
	if *daemonMode {
		if err := runAsDaemon(); err != nil {
			fmt.Printf("启动守护进程失败: %v\n", err)
			os.Exit(1)
		}
	}

	// 初始化日志管理器
	logManager, err := logger.NewDefaultLogger(*logDir)
	if err != nil {
		fmt.Printf("初始化日志管理器失败: %v\n", err)
		os.Exit(1)
	}
	defer logManager.Close()

	// 初始化进程管理器
	processManager := process.NewProcessManager(logManager)

	// 初始化信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// 启动信号处理协程
	go handleSignals(sigChan, processManager, configPath, logManager)

	// 添加进程
	for _, programCfg := range cfg.Programs {
		processManager.AddProcess(programCfg)
	}

	// 初始化监控器
	monitor := process.NewMonitor(processManager)
	monitor.Start()
	defer monitor.Stop()

	// 处理命令
	switch *command {
	case "start":
		if *processName != "" {
			// 启动指定进程
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				os.Exit(1)
			}
			if err := p.Start(); err != nil {
				fmt.Printf("启动进程 %s 失败: %v\n", *processName, err)
				os.Exit(1)
			}
			fmt.Printf("进程 %s 启动成功\n", *processName)
		} else {
			// 启动所有进程
			processManager.StartAll()
			fmt.Println("所有进程启动成功")

			// 如果启用了Web界面，启动Web服务器
			if *webEnable {
				webServer, err := web.NewWebServer(processManager)
				if err != nil {
					fmt.Printf("初始化Web服务器失败: %v\n", err)
					os.Exit(1)
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
				metricsManager := metrics.NewMetricsManager(processManager)
				
				// 启动指标收集器
				metricsManager.StartMetricsCollector(5 * time.Second)
				
				// 在goroutine中启动指标服务器
				go func() {
					if err := metricsManager.StartMetricsServer(*metricsAddr); err != nil {
						fmt.Printf("启动指标服务器失败: %v\n", err)
					}
				}()
			}

			// 保持运行
			select {}
		}
	case "stop":
		if *processName != "" {
			// 停止指定进程
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				os.Exit(1)
			}
			if err := p.Stop(); err != nil {
				fmt.Printf("停止进程 %s 失败: %v\n", *processName, err)
				os.Exit(1)
			}
			fmt.Printf("进程 %s 停止成功\n", *processName)
		} else {
			// 停止所有进程
			processManager.StopAll()
			fmt.Println("所有进程停止成功")
		}
	case "restart":
		if *processName != "" {
			// 重启指定进程
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				os.Exit(1)
			}
			if err := p.Restart(); err != nil {
				fmt.Printf("重启进程 %s 失败: %v\n", *processName, err)
				os.Exit(1)
			}
			fmt.Printf("进程 %s 重启成功\n", *processName)
		} else {
			// 重启所有进程
			for _, p := range processManager.Processes {
				if err := p.Restart(); err != nil {
					fmt.Printf("重启进程 %s 失败: %v\n", p.Name, err)
				}
			}
			fmt.Println("所有进程重启成功")
		}
	case "status":
		// 显示进程状态
		if *processName != "" {
			// 显示指定进程状态
			p := processManager.GetProcess(*processName)
			if p == nil {
				fmt.Printf("进程 %s 不存在\n", *processName)
				os.Exit(1)
			}
			printProcessStatus(p)
		} else {
			// 显示所有进程状态
			for _, p := range processManager.Processes {
				printProcessStatus(p)
				fmt.Println()
			}
		}
	case "reload":
		// 重新加载配置
		if err := reloadConfiguration(processManager, *configPath, logManager); err != nil {
			fmt.Printf("重新加载配置失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("配置重新加载成功")
	case "update":
		// 更新进程配置
		if *processName != "" {
			// 更新指定进程
			if err := updateProcessConfig(processManager, *configPath, *processName, logManager); err != nil {
				fmt.Printf("更新进程配置失败: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("进程 %s 配置更新成功\n", *processName)
		} else {
			fmt.Println("更新命令需要指定进程名称")
			os.Exit(1)
		}
	default:
		fmt.Printf("未知命令: %s\n", *command)
		os.Exit(1)
	}
}

func printProcessStatus(p *process.Process) {
	fmt.Printf("进程名称: %s\n", p.Name)
	fmt.Printf("状态: %s\n", p.State)
	if p.PID > 0 {
		fmt.Printf("PID: %d\n", p.PID)
	}
	if !p.StartTime.IsZero() {
		fmt.Printf("启动时间: %s\n", p.StartTime.Format("2006-01-02 15:04:05"))
	}
	if !p.StopTime.IsZero() {
		fmt.Printf("停止时间: %s\n", p.StopTime.Format("2006-01-02 15:04:05"))
	}
	if p.ExitCode != 0 {
		fmt.Printf("退出码: %d\n", p.ExitCode)
	}
	fmt.Printf("启动重试次数: %d\n", p.StartRetries)
}

// printVersion 打印版本信息
func printVersion() {
	fmt.Printf("%s version %s\n", ProgramName, Version)
	fmt.Println("Go-based process supervisor")
	fmt.Println("Copyright (c) 2024 gosupervisor team")
}

// testConfiguration 测试配置文件有效性
func testConfiguration(configPath string) error {
	_, err := config.LoadConfig(configPath)
	return err
}

// runAsDaemon 以守护进程模式运行
func runAsDaemon() error {
	// 在实际实现中，这里会包含守护进程的创建逻辑
	// 由于Windows系统的特殊性，这里只做简单处理
	fmt.Println("以守护进程模式运行...")
	return nil
}

// handleSignals 处理系统信号
func handleSignals(sigChan chan os.Signal, processManager *process.ProcessManager, configPath *string, logManager *logger.Logger) {
	for {
		sig := <-sigChan
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			// 处理终止信号
			logManager.Info("收到终止信号，正在停止所有进程...")
			processManager.StopAll()
			logManager.Info("所有进程已停止，正在退出...")
			os.Exit(0)
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

	// 停止所有进程
	processManager.StopAll()

	// 清空现有进程
	processManager.Processes = make(map[string]*process.Process)

	// 添加新进程
	for _, programCfg := range cfg.Programs {
		processManager.AddProcess(programCfg)
	}

	// 重新启动所有进程
	processManager.StartAll()
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
	delete(processManager.Processes, processName)

	// 添加新进程
	processManager.AddProcess(programCfg)

	// 启动新进程
	newProcess := processManager.GetProcess(processName)
	if newProcess != nil {
		if err := newProcess.Start(); err != nil {
			return fmt.Errorf("启动进程 %s 失败: %v", processName, err)
		}
	}

	return nil
}
