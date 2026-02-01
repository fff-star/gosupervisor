// Package config 提供配置文件解析功能，支持INI、YAML和JSON格式
package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProgramConfig 表示单个进程的配置
type ProgramConfig struct {
	// Name 进程名称
	Name string
	// Command 要执行的命令
	Command string
	// Directory 命令执行的工作目录
	Directory string
	// AutoStart 是否自动启动
	AutoStart bool
	// AutoRestart 是否自动重启
	AutoRestart bool
	// StartSecs 启动后等待多少秒确认进程正常运行
	StartSecs int
	// StartRetries 启动失败后重试的次数
	StartRetries int
	// StopSecs 停止进程前等待的秒数
	StopSecs int
	// StopSignal 停止信号
	StopSignal string
	// User 以哪个用户身份运行进程
	User string
	// Environment 环境变量
	Environment map[string]string
	// RedirectStdout 是否重定向标准输出到日志
	RedirectStdout bool
	// RedirectStderr 是否重定向标准错误到日志
	RedirectStderr bool
	// StdoutLogFile 标准输出日志文件路径
	StdoutLogFile string
	// StderrLogFile 标准错误日志文件路径
	StderrLogFile string
	// StdoutLogMaxBytes 标准输出日志文件最大大小
	StdoutLogMaxBytes int64
	// StdoutLogBackupCount 标准输出日志文件备份数量
	StdoutLogBackupCount int
	// StderrLogMaxBytes 标准错误日志文件最大大小
	StderrLogMaxBytes int64
	// StderrLogBackupCount 标准错误日志文件备份数量
	StderrLogBackupCount int
	// Priority 启动优先级
	Priority int
	// Umask 文件权限掩码
	Umask int
	// ServerURL 服务器URL
	ServerURL string
	// DependsOn 依赖的其他进程
	DependsOn []string
}

// Config 表示整个配置文件的结构
type Config struct {
	// Programs 存储所有进程的配置，键为进程名称
	Programs map[string]*ProgramConfig
}

// YAMLConfig 用于解析YAML格式的配置文件
type YAMLConfig struct {
	// Programs 存储所有进程的配置，键为进程名称
	Programs map[string]*ProgramConfig `yaml:"programs"`
}

// JSONConfig 用于解析JSON格式的配置文件
type JSONConfig struct {
	// Programs 存储所有进程的配置，键为进程名称
	Programs map[string]*ProgramConfig `json:"programs"`
}

// LoadConfig 加载配置文件，根据文件扩展名自动检测格式
// configPath 配置文件路径
// 返回配置对象和错误信息
func LoadConfig(configPath string) (*Config, error) {
	if !filepath.IsAbs(configPath) {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			return nil, fmt.Errorf("无法获取配置文件绝对路径: %v", err)
		}
		configPath = absPath
	}

	// 根据文件扩展名选择解析器
	ext := strings.ToLower(filepath.Ext(configPath))
	switch ext {
	case ".yaml", ".yml":
		return loadYAMLConfig(configPath)
	case ".json":
		return loadJSONConfig(configPath)
	default:
		// 默认使用INI格式
		return loadINIConfig(configPath)
	}
}

// loadINIConfig 加载INI格式的配置文件
// configPath 配置文件路径
// 返回配置对象和错误信息
func loadINIConfig(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件: %v", err)
	}
	defer file.Close()

	config := &Config{
		Programs: make(map[string]*ProgramConfig),
	}

	scanner := bufio.NewScanner(file)
	var currentProgram *ProgramConfig

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			if strings.HasPrefix(section, "program:") {
				programName := strings.TrimPrefix(section, "program:")
				currentProgram = &ProgramConfig{
					Name:                programName,
					AutoStart:           true,
					AutoRestart:         true,
					StartSecs:           1,
					StartRetries:        3,
					StopSecs:            10,
					StopSignal:          "SIGTERM",
					Environment:         make(map[string]string),
					RedirectStdout:      true,
					RedirectStderr:      true,
					StdoutLogMaxBytes:   50 * 1024 * 1024, // 50MB
					StdoutLogBackupCount: 10,
					StderrLogMaxBytes:   50 * 1024 * 1024, // 50MB
					StderrLogBackupCount: 10,
					Priority:            999,
					Umask:               022,
					DependsOn:           []string{},
				}
				config.Programs[programName] = currentProgram
			}
			continue
		}

		if currentProgram != nil {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])

				switch key {
					case "command":
						currentProgram.Command = value
					case "directory":
						currentProgram.Directory = value
					case "autostart":
						currentProgram.AutoStart = value == "true"
					case "autorestart":
						currentProgram.AutoRestart = value == "true"
					case "startsecs":
						fmt.Sscanf(value, "%d", &currentProgram.StartSecs)
					case "startretries":
						fmt.Sscanf(value, "%d", &currentProgram.StartRetries)
					case "stopsecs":
						fmt.Sscanf(value, "%d", &currentProgram.StopSecs)
					case "stopsignal":
						currentProgram.StopSignal = value
					case "user":
						currentProgram.User = value
					case "environment":
						// 解析环境变量，格式如: KEY1=value1,KEY2=value2
						envVars := strings.Split(value, ",")
						for _, envVar := range envVars {
							envParts := strings.SplitN(envVar, "=", 2)
							if len(envParts) == 2 {
								envKey := strings.TrimSpace(envParts[0])
								envValue := strings.TrimSpace(envParts[1])
								currentProgram.Environment[envKey] = envValue
							}
						}
					case "redirectstdout":
						currentProgram.RedirectStdout = value == "true"
					case "redirectstderr":
						currentProgram.RedirectStderr = value == "true"
					case "stdoutlogfile":
						currentProgram.StdoutLogFile = value
					case "stderrlogfile":
						currentProgram.StderrLogFile = value
					case "stdoutlogmaxbytes":
						fmt.Sscanf(value, "%d", &currentProgram.StdoutLogMaxBytes)
					case "stdoutlogbackupcount":
						fmt.Sscanf(value, "%d", &currentProgram.StdoutLogBackupCount)
					case "stderrlogmaxbytes":
						fmt.Sscanf(value, "%d", &currentProgram.StderrLogMaxBytes)
					case "stderrlogbackupcount":
						fmt.Sscanf(value, "%d", &currentProgram.StderrLogBackupCount)
					case "priority":
						fmt.Sscanf(value, "%d", &currentProgram.Priority)
					case "umask":
						fmt.Sscanf(value, "%d", &currentProgram.Umask)
					case "serverurl":
						currentProgram.ServerURL = value
					case "dependson":
						// 解析依赖关系，格式如: prog1,prog2,prog3
						deps := strings.Split(value, ",")
						for _, dep := range deps {
							dep = strings.TrimSpace(dep)
							if dep != "" {
								currentProgram.DependsOn = append(currentProgram.DependsOn, dep)
							}
						}
					}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取配置文件错误: %v", err)
	}

	return config, nil
}

// loadYAMLConfig 加载YAML格式的配置文件
// configPath 配置文件路径
// 返回配置对象和错误信息
func loadYAMLConfig(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件: %v", err)
	}
	defer file.Close()

	var yamlConfig YAMLConfig
	if err := yaml.NewDecoder(file).Decode(&yamlConfig); err != nil {
		return nil, fmt.Errorf("解析YAML配置文件错误: %v", err)
	}

	// 设置默认值
	config := &Config{
		Programs: make(map[string]*ProgramConfig),
	}

	for name, prog := range yamlConfig.Programs {
		if prog.Name == "" {
			prog.Name = name
		}
		if prog.Environment == nil {
			prog.Environment = make(map[string]string)
		}
		if prog.DependsOn == nil {
			prog.DependsOn = []string{}
		}
		if prog.StartSecs == 0 {
			prog.StartSecs = 1
		}
		if prog.StartRetries == 0 {
			prog.StartRetries = 3
		}
		if prog.StopSecs == 0 {
			prog.StopSecs = 10
		}
		if prog.StopSignal == "" {
			prog.StopSignal = "SIGTERM"
		}
		if prog.StdoutLogMaxBytes == 0 {
			prog.StdoutLogMaxBytes = 50 * 1024 * 1024 // 50MB
		}
		if prog.StdoutLogBackupCount == 0 {
			prog.StdoutLogBackupCount = 10
		}
		if prog.StderrLogMaxBytes == 0 {
			prog.StderrLogMaxBytes = 50 * 1024 * 1024 // 50MB
		}
		if prog.StderrLogBackupCount == 0 {
			prog.StderrLogBackupCount = 10
		}
		if prog.Priority == 0 {
			prog.Priority = 999
		}
		if prog.Umask == 0 {
			prog.Umask = 022
		}
		config.Programs[name] = prog
	}

	return config, nil
}

// loadJSONConfig 加载JSON格式的配置文件
// configPath 配置文件路径
// 返回配置对象和错误信息
func loadJSONConfig(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件: %v", err)
	}
	defer file.Close()

	var jsonConfig JSONConfig
	if err := json.NewDecoder(file).Decode(&jsonConfig); err != nil {
		return nil, fmt.Errorf("解析JSON配置文件错误: %v", err)
	}

	// 设置默认值
	config := &Config{
		Programs: make(map[string]*ProgramConfig),
	}

	for name, prog := range jsonConfig.Programs {
		if prog.Name == "" {
			prog.Name = name
		}
		if prog.Environment == nil {
			prog.Environment = make(map[string]string)
		}
		if prog.DependsOn == nil {
			prog.DependsOn = []string{}
		}
		if prog.StartSecs == 0 {
			prog.StartSecs = 1
		}
		if prog.StartRetries == 0 {
			prog.StartRetries = 3
		}
		if prog.StopSecs == 0 {
			prog.StopSecs = 10
		}
		if prog.StopSignal == "" {
			prog.StopSignal = "SIGTERM"
		}
		if prog.StdoutLogMaxBytes == 0 {
			prog.StdoutLogMaxBytes = 50 * 1024 * 1024 // 50MB
		}
		if prog.StdoutLogBackupCount == 0 {
			prog.StdoutLogBackupCount = 10
		}
		if prog.StderrLogMaxBytes == 0 {
			prog.StderrLogMaxBytes = 50 * 1024 * 1024 // 50MB
		}
		if prog.StderrLogBackupCount == 0 {
			prog.StderrLogBackupCount = 10
		}
		if prog.Priority == 0 {
			prog.Priority = 999
		}
		if prog.Umask == 0 {
			prog.Umask = 022
		}
		config.Programs[name] = prog
	}

	return config, nil
}
