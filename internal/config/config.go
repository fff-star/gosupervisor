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
	Name                 string            `yaml:"name" json:"name"`
	Command              string            `yaml:"command" json:"command"`
	Directory            string            `yaml:"directory" json:"directory"`
	AutoStart            bool              `yaml:"autostart" json:"autostart"`
	AutoRestart          bool              `yaml:"autorestart" json:"autorestart"`
	StartSecs            int               `yaml:"startsecs" json:"startsecs"`
	StartRetries         int               `yaml:"startretries" json:"startretries"`
	StopSecs             int               `yaml:"stopsecs" json:"stopsecs"`
	StopSignal           string            `yaml:"stopsignal" json:"stopsignal"`
	User                 string            `yaml:"user" json:"user"`
	Environment          map[string]string `yaml:"environment" json:"environment"`
	RedirectStdout       bool              `yaml:"redirectstdout" json:"redirectstdout"`
	RedirectStderr       bool              `yaml:"redirectstderr" json:"redirectstderr"`
	StdoutLogFile        string            `yaml:"stdoutlogfile" json:"stdoutlogfile"`
	StderrLogFile        string            `yaml:"stderrlogfile" json:"stderrlogfile"`
	StdoutLogMaxBytes    int64             `yaml:"stdoutlogmaxbytes" json:"stdoutlogmaxbytes"`
	StdoutLogBackupCount int               `yaml:"stdoutlogbackupcount" json:"stdoutlogbackupcount"`
	StderrLogMaxBytes    int64             `yaml:"stderrlogmaxbytes" json:"stderrlogmaxbytes"`
	StderrLogBackupCount int               `yaml:"stderrlogbackupcount" json:"stderrlogbackupcount"`
	Priority             int               `yaml:"priority" json:"priority"`
	Umask                int               `yaml:"umask" json:"umask"`
	DependsOn            []string          `yaml:"dependson" json:"dependson"`
	Group                string            `yaml:"group" json:"group"`

	// Health checks
	HealthCheckURL                string  `yaml:"healthcheckurl" json:"healthcheckurl"`
	HealthCheckInterval           int     `yaml:"healthcheckinterval" json:"healthcheckinterval"`
	HealthCheckTimeout            int     `yaml:"healthchecktimeout" json:"healthchecktimeout"`
	HealthCheckUnhealthyThreshold int     `yaml:"healthcheckunhealthythreshold" json:"healthcheckunhealthythreshold"`
	HealthCheckRestart            bool    `yaml:"healthcheckrestart" json:"healthcheckrestart"`
	CPUThresholdPercent           float64 `yaml:"cputhresholdpercent" json:"cputhresholdpercent"`
	MemoryThresholdBytes          int64   `yaml:"memorythresholdbytes" json:"memorythresholdbytes"`

	// Event hooks
	PreStartScript string `yaml:"prestartscript" json:"prestartscript"`
	PostStopScript string `yaml:"poststopscript" json:"poststopscript"`

	// Restart rate limiting
	RestartMaxCount  int `yaml:"restartmaxcount" json:"restartmaxcount"`
	RestartWindowSecs int `yaml:"restartwindowsecs" json:"restartwindowsecs"`

	// Exit code-based restart policy
	RestartCodes   []int `yaml:"restartcodes" json:"restartcodes"`
	NoRestartCodes []int `yaml:"norestartcodes" json:"norestartcodes"`

	// Cgroup v2
	CgroupPath string `yaml:"cgrouppath" json:"cgrouppath"`

	// Webhook notifications
	WebhookURL     string `yaml:"webhookurl" json:"webhookurl"`
	WebhookRetries int    `yaml:"webhookretries" json:"webhookretries"`
	WebhookTimeout int    `yaml:"webhooktimeout" json:"webhooktimeout"`

	// Stdin
	StdinFile string `yaml:"stdinfile" json:"stdinfile"`
}

// ServerConfig holds global server settings (typically from [supervisord] section).
type ServerConfig struct {
	WebAddr     string `yaml:"webaddr" json:"webaddr"`
	WebUser     string `yaml:"webuser" json:"webuser"`
	WebPass     string `yaml:"webpass" json:"webpass"`
	MetricsAddr string `yaml:"metricsaddr" json:"metricsaddr"`
	SocketPath  string `yaml:"socketpath" json:"socketpath"`
	StateFile   string `yaml:"statefile" json:"statefile"`
	LogDir      string `yaml:"logdir" json:"logdir"`
	CORSOrigin  string `yaml:"corsorigin" json:"corsorigin"`
	RateLimitRPS int   `yaml:"ratelimitrps" json:"ratelimitrps"`
}

// Config 表示整个配置文件的结构
type Config struct {
	// Programs 存储所有进程的配置，键为进程名称
	Programs map[string]*ProgramConfig
	// Server contains optional global server settings (from [supervisord] section)
	Server *ServerConfig
}

// YAMLConfig 用于解析YAML格式的配置文件
type YAMLConfig struct {
	Programs map[string]*ProgramConfig `yaml:"programs"`
	Server   *ServerConfig            `yaml:"supervisord"`
}

// JSONConfig 用于解析JSON格式的配置文件
type JSONConfig struct {
	Programs map[string]*ProgramConfig `json:"programs"`
	Server   *ServerConfig            `json:"supervisord"`
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
	var currentSection string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			currentSection = section
			currentProgram = nil
			if strings.HasPrefix(section, "program:") {
				programName := strings.TrimPrefix(section, "program:")
				currentProgram = &ProgramConfig{
					Name:                         programName,
					AutoStart:                    true,
					AutoRestart:                  true,
					StartSecs:                    1,
					StartRetries:                 3,
					StopSecs:                     10,
					StopSignal:                   "SIGTERM",
					Environment:                  make(map[string]string),
					RedirectStdout:               true,
					RedirectStderr:               true,
					StdoutLogMaxBytes:            50 * 1024 * 1024, // 50MB
					StdoutLogBackupCount:         10,
					StderrLogMaxBytes:            50 * 1024 * 1024, // 50MB
					StderrLogBackupCount:         10,
					Priority:                     999,
					Umask:                        022,
					DependsOn:                    []string{},
					RestartCodes:                 []int{},
					NoRestartCodes:               []int{},
					HealthCheckInterval:           30,
					HealthCheckTimeout:            5,
					HealthCheckUnhealthyThreshold: 3,
					HealthCheckRestart:            false,
					CPUThresholdPercent:           90.0,
					MemoryThresholdBytes:          2 * 1024 * 1024 * 1024, // 2GB
					RestartWindowSecs:             60,
				}
				config.Programs[programName] = currentProgram
			} else if section == "supervisord" {
				config.Server = &ServerConfig{}
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
				case "dependson":
					// 解析依赖关系，格式如: prog1,prog2,prog3
					deps := strings.Split(value, ",")
					for _, dep := range deps {
						dep = strings.TrimSpace(dep)
						if dep != "" {
							currentProgram.DependsOn = append(currentProgram.DependsOn, dep)
						}
					}
				case "group":
					currentProgram.Group = value
				case "healthcheckurl":
					currentProgram.HealthCheckURL = value
				case "healthcheckinterval":
					fmt.Sscanf(value, "%d", &currentProgram.HealthCheckInterval)
				case "healthchecktimeout":
					fmt.Sscanf(value, "%d", &currentProgram.HealthCheckTimeout)
				case "healthcheckunhealthythreshold":
					fmt.Sscanf(value, "%d", &currentProgram.HealthCheckUnhealthyThreshold)
				case "healthcheckrestart":
					currentProgram.HealthCheckRestart = value == "true"
				case "cputhresholdpercent":
					fmt.Sscanf(value, "%f", &currentProgram.CPUThresholdPercent)
				case "memorythresholdbytes":
					fmt.Sscanf(value, "%d", &currentProgram.MemoryThresholdBytes)
				case "prestartscript":
					currentProgram.PreStartScript = value
				case "poststopscript":
					currentProgram.PostStopScript = value
				case "restartmaxcount":
					fmt.Sscanf(value, "%d", &currentProgram.RestartMaxCount)
				case "restartwindowsecs":
					fmt.Sscanf(value, "%d", &currentProgram.RestartWindowSecs)
				case "restartcodes":
					for _, s := range strings.Split(value, ",") {
						var code int
						fmt.Sscanf(strings.TrimSpace(s), "%d", &code)
						currentProgram.RestartCodes = append(currentProgram.RestartCodes, code)
					}
				case "norestartcodes":
					for _, s := range strings.Split(value, ",") {
						var code int
						fmt.Sscanf(strings.TrimSpace(s), "%d", &code)
						currentProgram.NoRestartCodes = append(currentProgram.NoRestartCodes, code)
					}
				case "cgrouppath":
					currentProgram.CgroupPath = value
				case "webhookurl":
					currentProgram.WebhookURL = value
				case "webhookretries":
					fmt.Sscanf(value, "%d", &currentProgram.WebhookRetries)
				case "webhooktimeout":
					fmt.Sscanf(value, "%d", &currentProgram.WebhookTimeout)
				case "stdinfile":
					currentProgram.StdinFile = value
				}
			}
		} else if config.Server != nil && currentSection == "supervisord" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				switch key {
				case "webaddr":
					config.Server.WebAddr = value
				case "webuser":
					config.Server.WebUser = value
				case "webpass":
					config.Server.WebPass = value
				case "metricsaddr":
					config.Server.MetricsAddr = value
				case "socketpath":
					config.Server.SocketPath = value
				case "statefile":
					config.Server.StateFile = value
				case "logdir":
					config.Server.LogDir = value
				case "corsorigin":
					config.Server.CORSOrigin = value
				case "ratelimitrps":
					fmt.Sscanf(value, "%d", &config.Server.RateLimitRPS)
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
func loadYAMLConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件: %v", err)
	}

	// First pass: detect which keys are present for each program
	var rawDoc map[string]map[string]map[string]interface{}
	if err := yaml.Unmarshal(data, &rawDoc); err != nil {
		rawDoc = nil
	}
	rawPrograms := rawDoc["programs"]

	var yamlConfig YAMLConfig
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, fmt.Errorf("解析YAML配置文件错误: %v", err)
	}

	cfg, err := applyDefaults(yamlConfig.Programs, rawPrograms)
	if err != nil {
		return nil, err
	}
	cfg.Server = yamlConfig.Server
	return cfg, nil
}

func applyDefaults(programs map[string]*ProgramConfig, rawPrograms map[string]map[string]interface{}) (*Config, error) {
	config := &Config{
		Programs: make(map[string]*ProgramConfig),
	}

	for name, prog := range programs {
		if prog == nil {
			prog = &ProgramConfig{Name: name}
			programs[name] = prog
		}
		if prog.Name == "" {
			prog.Name = name
		}
		if prog.Environment == nil {
			prog.Environment = make(map[string]string)
		}
		if prog.DependsOn == nil {
			prog.DependsOn = []string{}
		}
		if prog.RestartCodes == nil {
			prog.RestartCodes = []int{}
		}
		if prog.NoRestartCodes == nil {
			prog.NoRestartCodes = []int{}
		}

		raw := rawPrograms[name]

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
			prog.StdoutLogMaxBytes = 50 * 1024 * 1024
		}
		if prog.StdoutLogBackupCount == 0 {
			prog.StdoutLogBackupCount = 10
		}
		if prog.StderrLogMaxBytes == 0 {
			prog.StderrLogMaxBytes = 50 * 1024 * 1024
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
		if prog.HealthCheckInterval == 0 {
			prog.HealthCheckInterval = 30
		}
		if prog.HealthCheckTimeout == 0 {
			prog.HealthCheckTimeout = 5
		}
		if prog.HealthCheckUnhealthyThreshold == 0 {
			prog.HealthCheckUnhealthyThreshold = 3
		}
		if prog.CPUThresholdPercent == 0 {
			prog.CPUThresholdPercent = 90.0
		}
		if prog.MemoryThresholdBytes == 0 {
			prog.MemoryThresholdBytes = 2 * 1024 * 1024 * 1024 // 2GB
		}
		if prog.RestartWindowSecs == 0 {
			prog.RestartWindowSecs = 60
		}

		// Bool defaults: use raw map to distinguish "absent" from "explicitly false"
		if raw == nil {
			prog.AutoStart = true
			prog.AutoRestart = true
			prog.RedirectStdout = true
			prog.RedirectStderr = true
		} else {
			if _, ok := raw["autostart"]; !ok {
				prog.AutoStart = true
			}
			if _, ok := raw["autorestart"]; !ok {
				prog.AutoRestart = true
			}
			if _, ok := raw["redirectstdout"]; !ok {
				prog.RedirectStdout = true
			}
			if _, ok := raw["redirectstderr"]; !ok {
				prog.RedirectStderr = true
			}
		}

		config.Programs[name] = prog
	}

	return config, nil
}

// loadJSONConfig 加载JSON格式的配置文件
func loadJSONConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件: %v", err)
	}

	// First pass: detect which keys are present
	var rawDoc map[string]map[string]map[string]interface{}
	if err := json.Unmarshal(data, &rawDoc); err != nil {
		rawDoc = nil
	}
	rawPrograms := rawDoc["programs"]

	var jsonConfig JSONConfig
	if err := json.Unmarshal(data, &jsonConfig); err != nil {
		return nil, fmt.Errorf("解析JSON配置文件错误: %v", err)
	}

	cfg, err := applyDefaults(jsonConfig.Programs, rawPrograms)
	if err != nil {
		return nil, err
	}
	cfg.Server = jsonConfig.Server
	return cfg, nil
}

// ValidateConfig validates config consistency — missing DependsOn references, etc.
func (c *Config) ValidateConfig() []string {
	var warnings []string
	for name, prog := range c.Programs {
		for _, dep := range prog.DependsOn {
			if _, ok := c.Programs[dep]; !ok {
				warnings = append(warnings, fmt.Sprintf("进程 %s 依赖了不存在的进程 %s", name, dep))
			}
		}
		if prog.Name == "" {
			warnings = append(warnings, fmt.Sprintf("进程 %s 配置缺少 name 字段", name))
		}
	}
	return warnings
}
