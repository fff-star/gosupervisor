// Package config 提供配置文件解析功能，支持INI、YAML和JSON格式
package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// parseIntWarn 解析整数并在失败时输出警告
func parseIntWarn(value, field string, dst any) {
	if _, err := fmt.Sscanf(value, "%d", dst); err != nil {
		fmt.Printf("警告: 无法解析 %s 的值 '%s'，使用默认值\n", field, value)
	}
}

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
	// Umask: the Go default (022) is octal=18 decimal, but config files must use decimal.
	// In INI/YAML/JSON: umask=18 corresponds to traditional octal 022 (write umask: 18 not 022).
	Umask     int      `yaml:"umask" json:"umask"`
	DependsOn []string `yaml:"dependson" json:"dependson"`
	Group     string   `yaml:"group" json:"group"`

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
	RestartMaxCount   int `yaml:"restartmaxcount" json:"restartmaxcount"`
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

	// Process templates
	NumProcs    int    `yaml:"numprocs" json:"numprocs"`
	ProcessName string `yaml:"processname" json:"processname"`

	// Process group signaling
	KillsAsGroup bool `yaml:"killasgroup" json:"killasgroup"`
	StopAsGroup  bool `yaml:"stopasgroup" json:"stopasgroup"`

	// Resource limits (0 = not set, value applied to both soft and hard limit)
	RlimitAs     uint64 `yaml:"rlimitas" json:"rlimitas"`
	RlimitCore   uint64 `yaml:"rlimitcore" json:"rlimitcore"`
	RlimitCpu    uint64 `yaml:"rlimitcpu" json:"rlimitcpu"`
	RlimitData   uint64 `yaml:"rlimitdata" json:"rlimitdata"`
	RlimitFsize  uint64 `yaml:"rlimitfsize" json:"rlimitfsize"`
	RlimitNofile uint64 `yaml:"rlimitnofile" json:"rlimitnofile"`
	RlimitNproc  uint64 `yaml:"rlimitnproc" json:"rlimitnproc"`
	RlimitStack  uint64 `yaml:"rlimitstack" json:"rlimitstack"`

	// FastCGI socket configuration (non-empty Socket means this is an fcgi-program)
	Socket        string `yaml:"socket" json:"socket"`
	SocketBacklog int    `yaml:"socket_backlog" json:"socket_backlog"`
	SocketOwner   string `yaml:"socket_owner" json:"socket_owner"`
	SocketMode    uint32 `yaml:"socket_mode" json:"socket_mode"`

	// Exit codes considered "expected" — they do not exhaust StartRetries
	ExitCodes []int `yaml:"exitcodes" json:"exitcodes"`
}

// EventListenerConfig represents the configuration for an event listener process.
// These are defined in [eventlistener:x] sections and implement the supervisord
// event listener protocol (READY/RESULT) over stdin/stdout.
type EventListenerConfig struct {
	Name       string   `yaml:"name" json:"name"`
	Command    string   `yaml:"command" json:"command"`
	Events     []string `yaml:"events" json:"events"`
	BufferSize int      `yaml:"buffersize" json:"buffersize"`

	// Standard process fields
	Directory            string            `yaml:"directory" json:"directory"`
	User                 string            `yaml:"user" json:"user"`
	Environment          map[string]string `yaml:"environment" json:"environment"`
	AutoStart            bool              `yaml:"autostart" json:"autostart"`
	AutoRestart          bool              `yaml:"autorestart" json:"autorestart"`
	StartSecs            int               `yaml:"startsecs" json:"startsecs"`
	StartRetries         int               `yaml:"startretries" json:"startretries"`
	StopSecs             int               `yaml:"stopsecs" json:"stopsecs"`
	StopSignal           string            `yaml:"stopsignal" json:"stopsignal"`
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
	Group                string            `yaml:"group" json:"group"`
}

// ServerConfig holds global server settings (typically from [supervisord] section).
type ServerConfig struct {
	WebAddr      string `yaml:"webaddr" json:"webaddr"`
	WebUser      string `yaml:"webuser" json:"webuser"`
	WebPass      string `yaml:"webpass" json:"webpass"`
	MetricsAddr  string `yaml:"metricsaddr" json:"metricsaddr"`
	SocketPath   string `yaml:"socketpath" json:"socketpath"`
	StateFile    string `yaml:"statefile" json:"statefile"`
	LogDir       string `yaml:"logdir" json:"logdir"`
	LogFormat    string `yaml:"logformat" json:"logformat"` // "text" or "json", default "text"
	LogLevel     string `yaml:"loglevel" json:"loglevel"`   // "debug", "info", "warn", "error", default "info"
	CORSOrigin   string `yaml:"corsorigin" json:"corsorigin"`
	RateLimitRPS int    `yaml:"ratelimitrps" json:"ratelimitrps"`
	WebCert      string `yaml:"webcert" json:"webcert"`
	WebKey       string `yaml:"webkey" json:"webkey"`
	SocketMode   uint32 `yaml:"socketmode" json:"socketmode"`   // octal, e.g. 0644
	SocketOwner  string `yaml:"socketowner" json:"socketowner"` // "uid:gid" or "user:group"
}

// Config 表示整个配置文件的结构
type Config struct {
	// Programs 存储所有进程的配置，键为进程名称
	Programs map[string]*ProgramConfig
	// EventListeners stores event listener configs (from [eventlistener:x] sections).
	EventListeners map[string]*EventListenerConfig
	// FcgiPrograms stores fcgi-program configs (from [fcgi-program:x] sections).
	FcgiPrograms map[string]*ProgramConfig
	// Server contains optional global server settings (from [supervisord] section)
	Server *ServerConfig

	// includeFiles stores include glob patterns collected during parsing (private).
	includeFiles []string
}

// YAMLConfig 用于解析YAML格式的配置文件
type YAMLConfig struct {
	Programs       map[string]*ProgramConfig       `yaml:"programs"`
	EventListeners map[string]*EventListenerConfig `yaml:"eventlisteners"`
	FcgiPrograms   map[string]*ProgramConfig       `yaml:"fcgiprograms"`
	Server         *ServerConfig                   `yaml:"supervisord"`
	Includes       []string                        `yaml:"includes"`
}

// JSONConfig 用于解析JSON格式的配置文件
type JSONConfig struct {
	Programs       map[string]*ProgramConfig       `json:"programs"`
	EventListeners map[string]*EventListenerConfig `json:"eventlisteners"`
	FcgiPrograms   map[string]*ProgramConfig       `json:"fcgiprograms"`
	Server         *ServerConfig                   `json:"supervisord"`
	Includes       []string                        `json:"includes"`
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
	return loadConfigWithIncludes(configPath, make(map[string]bool))
}

// loadConfigWithIncludes loads a config file and recursively processes includes.
func loadConfigWithIncludes(configPath string, visited map[string]bool) (*Config, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法获取配置文件绝对路径: %v", err)
	}
	if visited[absPath] {
		return nil, fmt.Errorf("循环包含配置文件: %s", absPath)
	}
	visited[absPath] = true

	// 根据文件扩展名选择解析器
	ext := strings.ToLower(filepath.Ext(configPath))
	var cfg *Config
	switch ext {
	case ".yaml", ".yml":
		cfg, err = loadYAMLConfig(configPath)
	case ".json":
		cfg, err = loadJSONConfig(configPath)
	default:
		cfg, err = loadINIConfig(configPath)
	}
	if err != nil {
		return nil, err
	}

	// Process includes
	baseDir := filepath.Dir(absPath)
	for _, pattern := range cfg.includeFiles {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(baseDir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("无效的include模式 %s: %v", pattern, err)
		}
		for _, match := range matches {
			incCfg, err := loadConfigWithIncludes(match, visited)
			if err != nil {
				return nil, fmt.Errorf("包含文件 %s 错误: %v", match, err)
			}
			// Merge programs, warn on duplicates
			for name, prog := range incCfg.Programs {
				if _, exists := cfg.Programs[name]; exists {
					fmt.Printf("警告: 进程 %s 在include文件 %s 中重复定义，将被覆盖\n", name, match)
				}
				cfg.Programs[name] = prog
			}
			// Merge event listeners from included file
			for name, el := range incCfg.EventListeners {
				if _, exists := cfg.EventListeners[name]; exists {
					fmt.Printf("警告: 事件监听器 %s 在include文件 %s 中重复定义，将被覆盖\n", name, match)
				}
				if cfg.EventListeners == nil {
					cfg.EventListeners = make(map[string]*EventListenerConfig)
				}
				cfg.EventListeners[name] = el
			}
			// Merge fcgi programs from included file
			for name, prog := range incCfg.FcgiPrograms {
				if _, exists := cfg.FcgiPrograms[name]; exists {
					fmt.Printf("警告: fcgi程序 %s 在include文件 %s 中重复定义，将被覆盖\n", name, match)
				}
				if cfg.FcgiPrograms == nil {
					cfg.FcgiPrograms = make(map[string]*ProgramConfig)
				}
				cfg.FcgiPrograms[name] = prog
			}
			// Merge server config from included file (first one wins if not already set)
			if incCfg.Server != nil && cfg.Server == nil {
				cfg.Server = incCfg.Server
			}
		}
	}

	// Don't return includeFiles to caller
	cfg.includeFiles = nil
	return cfg, nil
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
	var currentListener *EventListenerConfig
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
			currentListener = nil
			if strings.HasPrefix(section, "program:") {
				programName := strings.TrimPrefix(section, "program:")
				currentProgram = &ProgramConfig{
					Name:                          programName,
					AutoStart:                     true,
					AutoRestart:                   true,
					StartSecs:                     1,
					StartRetries:                  3,
					StopSecs:                      10,
					StopSignal:                    "SIGTERM",
					Environment:                   make(map[string]string),
					RedirectStdout:                true,
					RedirectStderr:                true,
					StdoutLogMaxBytes:             50 * 1024 * 1024, // 50MB
					StdoutLogBackupCount:          10,
					StderrLogMaxBytes:             50 * 1024 * 1024, // 50MB
					StderrLogBackupCount:          10,
					Priority:                      999,
					Umask:                         022,
					DependsOn:                     []string{},
					RestartCodes:                  []int{},
					NoRestartCodes:                []int{},
					ExitCodes:                     []int{},
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
				if config.Server == nil {
					config.Server = &ServerConfig{}
				}
			} else if section == "inet_http_server" {
				if config.Server == nil {
					config.Server = &ServerConfig{}
				}
			} else if section == "include" {
				// [include] section — patterns are collected on the Config;
				// nothing to do here, the patterns are parsed in processIncludeSection.
				_ = section
			} else if strings.HasPrefix(section, "eventlistener:") {
				listenerName := strings.TrimPrefix(section, "eventlistener:")
				currentListener = &EventListenerConfig{
					Name:                 listenerName,
					AutoStart:            true,
					AutoRestart:          true,
					BufferSize:           100,
					StartSecs:            1,
					StartRetries:         3,
					StopSecs:             10,
					StopSignal:           "SIGTERM",
					Environment:          make(map[string]string),
					RedirectStdout:       true,
					RedirectStderr:       true,
					StdoutLogMaxBytes:    50 * 1024 * 1024,
					StdoutLogBackupCount: 10,
					StderrLogMaxBytes:    50 * 1024 * 1024,
					StderrLogBackupCount: 10,
					Priority:             999,
					Umask:                022,
				}
				if config.EventListeners == nil {
					config.EventListeners = make(map[string]*EventListenerConfig)
				}
				config.EventListeners[listenerName] = currentListener
			} else if strings.HasPrefix(section, "fcgi-program:") {
				fcgiName := strings.TrimPrefix(section, "fcgi-program:")
				currentProgram = &ProgramConfig{
					Name:                 fcgiName,
					AutoStart:            true,
					AutoRestart:          true,
					StartSecs:            1,
					StartRetries:         3,
					StopSecs:             10,
					StopSignal:           "SIGTERM",
					Environment:          make(map[string]string),
					RedirectStdout:       true,
					RedirectStderr:       true,
					StdoutLogMaxBytes:    50 * 1024 * 1024,
					StdoutLogBackupCount: 10,
					StderrLogMaxBytes:    50 * 1024 * 1024,
					StderrLogBackupCount: 10,
					Priority:             999,
					Umask:                022,
					DependsOn:            []string{},
					RestartCodes:         []int{},
					NoRestartCodes:       []int{},
					ExitCodes:            []int{},
					HealthCheckInterval:           30,
					HealthCheckTimeout:            5,
					HealthCheckUnhealthyThreshold: 3,
					HealthCheckRestart:            false,
					CPUThresholdPercent:           90.0,
					MemoryThresholdBytes:          2 * 1024 * 1024 * 1024,
					RestartWindowSecs:             60,
					SocketMode:                    0700,
					SocketBacklog:                 -1,
				}
				if config.FcgiPrograms == nil {
					config.FcgiPrograms = make(map[string]*ProgramConfig)
				}
				config.FcgiPrograms[fcgiName] = currentProgram
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
					parseIntWarn(value, "startsecs", &currentProgram.StartSecs)
				case "startretries":
					parseIntWarn(value, "startretries", &currentProgram.StartRetries)
				case "stopsecs":
					parseIntWarn(value, "stopsecs", &currentProgram.StopSecs)
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
					parseIntWarn(value, "stdoutlogmaxbytes", &currentProgram.StdoutLogMaxBytes)
				case "stdoutlogbackupcount":
					parseIntWarn(value, "stdoutlogbackupcount", &currentProgram.StdoutLogBackupCount)
				case "stderrlogmaxbytes":
					parseIntWarn(value, "stderrlogmaxbytes", &currentProgram.StderrLogMaxBytes)
				case "stderrlogbackupcount":
					parseIntWarn(value, "stderrlogbackupcount", &currentProgram.StderrLogBackupCount)
				case "priority":
					parseIntWarn(value, "priority", &currentProgram.Priority)
				case "umask":
					parseIntWarn(value, "umask", &currentProgram.Umask)
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
					parseIntWarn(value, "healthcheckinterval", &currentProgram.HealthCheckInterval)
				case "healthchecktimeout":
					parseIntWarn(value, "healthchecktimeout", &currentProgram.HealthCheckTimeout)
				case "healthcheckunhealthythreshold":
					parseIntWarn(value, "healthcheckunhealthythreshold", &currentProgram.HealthCheckUnhealthyThreshold)
				case "healthcheckrestart":
					currentProgram.HealthCheckRestart = value == "true"
				case "cputhresholdpercent":
					if _, err := fmt.Sscanf(value, "%f", &currentProgram.CPUThresholdPercent); err != nil {
						fmt.Printf("警告: 无法解析 cputhresholdpercent 的值 '%s'，使用默认值\n", value)
					}
				case "memorythresholdbytes":
					parseIntWarn(value, "memorythresholdbytes", &currentProgram.MemoryThresholdBytes)
				case "prestartscript":
					currentProgram.PreStartScript = value
				case "poststopscript":
					currentProgram.PostStopScript = value
				case "restartmaxcount":
					parseIntWarn(value, "restartmaxcount", &currentProgram.RestartMaxCount)
				case "restartwindowsecs":
					parseIntWarn(value, "restartwindowsecs", &currentProgram.RestartWindowSecs)
				case "restartcodes":
					for _, s := range strings.Split(value, ",") {
						var code int
						if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &code); err != nil {
							fmt.Printf("警告: 无法解析 restartcodes 值 '%s'，跳过\n", s)
							continue
						}
						currentProgram.RestartCodes = append(currentProgram.RestartCodes, code)
					}
				case "norestartcodes":
					for _, s := range strings.Split(value, ",") {
						var code int
						if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &code); err != nil {
							fmt.Printf("警告: 无法解析 norestartcodes 值 '%s'，跳过\n", s)
							continue
						}
						currentProgram.NoRestartCodes = append(currentProgram.NoRestartCodes, code)
					}
				case "cgrouppath":
					currentProgram.CgroupPath = value
				case "webhookurl":
					currentProgram.WebhookURL = value
				case "webhookretries":
					parseIntWarn(value, "webhookretries", &currentProgram.WebhookRetries)
				case "webhooktimeout":
					parseIntWarn(value, "webhooktimeout", &currentProgram.WebhookTimeout)
				case "stdinfile":
					currentProgram.StdinFile = value
				case "numprocs":
					parseIntWarn(value, "numprocs", &currentProgram.NumProcs)
				case "process_name":
					currentProgram.ProcessName = value
				case "killasgroup":
					currentProgram.KillsAsGroup = value == "true"
				case "stopasgroup":
					currentProgram.StopAsGroup = value == "true"
				case "rlimitas":
					parseIntWarn(value, "rlimitas", &currentProgram.RlimitAs)
				case "rlimitcore":
					parseIntWarn(value, "rlimitcore", &currentProgram.RlimitCore)
				case "rlimitcpu":
					parseIntWarn(value, "rlimitcpu", &currentProgram.RlimitCpu)
				case "rlimitdata":
					parseIntWarn(value, "rlimitdata", &currentProgram.RlimitData)
				case "rlimitfsize":
					parseIntWarn(value, "rlimitfsize", &currentProgram.RlimitFsize)
				case "rlimitnofile":
					parseIntWarn(value, "rlimitnofile", &currentProgram.RlimitNofile)
				case "rlimitnproc":
					parseIntWarn(value, "rlimitnproc", &currentProgram.RlimitNproc)
				case "rlimitstack":
					parseIntWarn(value, "rlimitstack", &currentProgram.RlimitStack)
				case "socket":
					currentProgram.Socket = value
				case "socket_backlog":
					parseIntWarn(value, "socket_backlog", &currentProgram.SocketBacklog)
				case "socket_owner":
					currentProgram.SocketOwner = value
				case "socket_mode":
					mode, err := strconv.ParseUint(value, 8, 32)
					if err != nil {
						fmt.Printf("警告: 无法解析 socket_mode 的值 '%s'，使用默认值\n", value)
					} else {
						currentProgram.SocketMode = uint32(mode)
					}
				case "exitcodes":
					for _, s := range strings.Split(value, ",") {
						var code int
						if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &code); err != nil {
							fmt.Printf("警告: 无法解析 exitcodes 值 '%s'，跳过\n", s)
							continue
						}
						currentProgram.ExitCodes = append(currentProgram.ExitCodes, code)
					}
				}
			}
		} else if currentListener != nil {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				switch key {
				case "command":
					currentListener.Command = value
				case "events":
					for _, e := range strings.Split(value, ",") {
						e = strings.TrimSpace(e)
						if e != "" {
							currentListener.Events = append(currentListener.Events, e)
						}
					}
				case "buffer_size":
					parseIntWarn(value, "buffer_size", &currentListener.BufferSize)
				case "directory":
					currentListener.Directory = value
				case "autostart":
					currentListener.AutoStart = value == "true"
				case "autorestart":
					currentListener.AutoRestart = value == "true"
				case "startsecs":
					parseIntWarn(value, "startsecs", &currentListener.StartSecs)
				case "startretries":
					parseIntWarn(value, "startretries", &currentListener.StartRetries)
				case "stopsecs":
					parseIntWarn(value, "stopsecs", &currentListener.StopSecs)
				case "stopsignal":
					currentListener.StopSignal = value
				case "user":
					currentListener.User = value
				case "environment":
					envVars := strings.Split(value, ",")
					for _, envVar := range envVars {
						envParts := strings.SplitN(envVar, "=", 2)
						if len(envParts) == 2 {
							envKey := strings.TrimSpace(envParts[0])
							envValue := strings.TrimSpace(envParts[1])
							currentListener.Environment[envKey] = envValue
						}
					}
				case "redirectstdout":
					currentListener.RedirectStdout = value == "true"
				case "redirectstderr":
					currentListener.RedirectStderr = value == "true"
				case "stdoutlogfile":
					currentListener.StdoutLogFile = value
				case "stderrlogfile":
					currentListener.StderrLogFile = value
				case "stdoutlogmaxbytes":
					parseIntWarn(value, "stdoutlogmaxbytes", &currentListener.StdoutLogMaxBytes)
				case "stdoutlogbackupcount":
					parseIntWarn(value, "stdoutlogbackupcount", &currentListener.StdoutLogBackupCount)
				case "stderrlogmaxbytes":
					parseIntWarn(value, "stderrlogmaxbytes", &currentListener.StderrLogMaxBytes)
				case "stderrlogbackupcount":
					parseIntWarn(value, "stderrlogbackupcount", &currentListener.StderrLogBackupCount)
				case "priority":
					parseIntWarn(value, "priority", &currentListener.Priority)
				case "umask":
					parseIntWarn(value, "umask", &currentListener.Umask)
				case "group":
					currentListener.Group = value
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
					parseIntWarn(value, "ratelimitrps", &config.Server.RateLimitRPS)
				case "webcert":
					config.Server.WebCert = value
				case "webkey":
					config.Server.WebKey = value
				case "socketmode":
					var v uint32
					if _, err := fmt.Sscanf(value, "%o", &v); err != nil {
						fmt.Printf("警告: 无法解析 socketmode 的值 '%s'，使用默认值\n", value)
					} else {
						config.Server.SocketMode = v
					}
				case "socketowner":
					config.Server.SocketOwner = value
				case "logformat":
					config.Server.LogFormat = value
				case "loglevel":
					config.Server.LogLevel = value
				}
			}
		} else if config.Server != nil && currentSection == "inet_http_server" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				switch key {
				case "port":
					config.Server.WebAddr = value
				case "username":
					config.Server.WebUser = value
				case "password":
					config.Server.WebPass = value
				case "cert":
					config.Server.WebCert = value
				case "key":
					config.Server.WebKey = value
				}
			}
		} else if currentSection == "include" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				if key == "files" {
					for _, pattern := range strings.Split(value, ",") {
						pattern = strings.TrimSpace(pattern)
						if pattern != "" {
							config.includeFiles = append(config.includeFiles, pattern)
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
	applyEventListenerDefaults(yamlConfig.EventListeners, rawDoc["eventlisteners"])
	cfg.EventListeners = yamlConfig.EventListeners
	// Merge fcgi programs — apply defaults and add to FcgiPrograms
	cfg.FcgiPrograms = make(map[string]*ProgramConfig)
	for name, prog := range yamlConfig.FcgiPrograms {
		if prog.Name == "" {
			prog.Name = name
		}
		if prog.Environment == nil {
			prog.Environment = make(map[string]string)
		}
		if prog.NumProcs == 0 {
			prog.NumProcs = 1
		}
		if prog.ProcessName == "" {
			prog.ProcessName = "%(program_name)s"
		}
		if prog.StopSecs == 0 {
			prog.StopSecs = 10
		}
		if prog.StartSecs == 0 {
			prog.StartSecs = 1
		}
		if prog.StartRetries == 0 {
			prog.StartRetries = 3
		}
		if prog.SocketMode == 0 {
			prog.SocketMode = 0700
		}
		if prog.SocketBacklog == 0 {
			prog.SocketBacklog = -1 // SOMAXCONN sentinel
		}
		// AutoStart/AutoRestart defaults handled by applyDefaults via raw map
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
		if prog.DependsOn == nil {
			prog.DependsOn = []string{}
		}
		if prog.RestartCodes == nil {
			prog.RestartCodes = []int{}
		}
		if prog.NoRestartCodes == nil {
			prog.NoRestartCodes = []int{}
		}
		if prog.ExitCodes == nil {
			prog.ExitCodes = []int{}
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
			prog.MemoryThresholdBytes = 2 * 1024 * 1024 * 1024
		}
		if prog.RestartWindowSecs == 0 {
			prog.RestartWindowSecs = 60
		}
		prog.RedirectStdout = true
		prog.RedirectStderr = true
		cfg.FcgiPrograms[name] = prog
	}
	cfg.Server = yamlConfig.Server
	cfg.includeFiles = yamlConfig.Includes
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
		if prog.ExitCodes == nil {
			prog.ExitCodes = []int{}
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
		if prog.NumProcs == 0 {
			prog.NumProcs = 1
		}
		if prog.ProcessName == "" {
			prog.ProcessName = "%(program_name)s"
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
	applyEventListenerDefaults(jsonConfig.EventListeners, rawDoc["eventlisteners"])
	cfg.EventListeners = jsonConfig.EventListeners
	// Merge fcgi programs — apply defaults and add to FcgiPrograms
	cfg.FcgiPrograms = make(map[string]*ProgramConfig)
	for name, prog := range jsonConfig.FcgiPrograms {
		if prog.Name == "" {
			prog.Name = name
		}
		if prog.Environment == nil {
			prog.Environment = make(map[string]string)
		}
		if prog.NumProcs == 0 {
			prog.NumProcs = 1
		}
		if prog.ProcessName == "" {
			prog.ProcessName = "%(program_name)s"
		}
		if prog.StopSecs == 0 {
			prog.StopSecs = 10
		}
		if prog.StartSecs == 0 {
			prog.StartSecs = 1
		}
		if prog.StartRetries == 0 {
			prog.StartRetries = 3
		}
		if prog.SocketMode == 0 {
			prog.SocketMode = 0700
		}
		if prog.SocketBacklog == 0 {
			prog.SocketBacklog = -1 // SOMAXCONN sentinel
		}
		// AutoStart/AutoRestart defaults handled by applyDefaults via raw map
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
		if prog.DependsOn == nil {
			prog.DependsOn = []string{}
		}
		if prog.RestartCodes == nil {
			prog.RestartCodes = []int{}
		}
		if prog.NoRestartCodes == nil {
			prog.NoRestartCodes = []int{}
		}
		if prog.ExitCodes == nil {
			prog.ExitCodes = []int{}
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
			prog.MemoryThresholdBytes = 2 * 1024 * 1024 * 1024
		}
		if prog.RestartWindowSecs == 0 {
			prog.RestartWindowSecs = 60
		}
		prog.RedirectStdout = true
		prog.RedirectStderr = true
		cfg.FcgiPrograms[name] = prog
	}
	cfg.Server = jsonConfig.Server
	cfg.includeFiles = jsonConfig.Includes
	return cfg, nil
}

// applyEventListenerDefaults fills in defaults for event listener configs.
// rawListeners carries the original YAML/JSON values so explicit false bools are preserved.
func applyEventListenerDefaults(listeners map[string]*EventListenerConfig, rawListeners map[string]map[string]interface{}) {
	for name, el := range listeners {
		if el == nil {
			listeners[name] = &EventListenerConfig{Name: name}
			el = listeners[name]
		}
		if el.Name == "" {
			el.Name = name
		}
		raw := rawListeners[name]
		if el.BufferSize == 0 {
			el.BufferSize = 100
		}
		if el.StartSecs == 0 {
			el.StartSecs = 1
		}
		if el.StartRetries == 0 {
			el.StartRetries = 3
		}
		if el.StopSecs == 0 {
			el.StopSecs = 10
		}
		if el.StopSignal == "" {
			el.StopSignal = "SIGTERM"
		}
		if el.Environment == nil {
			el.Environment = make(map[string]string)
		}
		if el.Priority == 0 {
			el.Priority = 999
		}
		if el.Umask == 0 {
			el.Umask = 022
		}
		if el.StdoutLogMaxBytes == 0 {
			el.StdoutLogMaxBytes = 50 * 1024 * 1024
		}
		if el.StderrLogMaxBytes == 0 {
			el.StderrLogMaxBytes = 50 * 1024 * 1024
		}
		// Bool defaults: match INI parser defaults (true), but only if not explicitly set
		if _, ok := raw["autostart"]; !ok {
			el.AutoStart = true
		}
		if _, ok := raw["autorestart"]; !ok {
			el.AutoRestart = true
		}
		if _, ok := raw["redirectstdout"]; !ok {
			el.RedirectStdout = true
		}
		if _, ok := raw["redirectstderr"]; !ok {
			el.RedirectStderr = true
		}
	}
}

var knownSignals = map[string]bool{
	"SIGTERM": true, "SIGKILL": true, "SIGHUP": true, "SIGINT": true,
	"SIGQUIT": true, "SIGUSR1": true, "SIGUSR2": true, "SIGSTOP": true,
	"SIGCONT": true, "SIGABRT": true, "SIGALRM": true,
	"SIGCHLD": true, "SIGPIPE": true, "SIGIO": true, "SIGTRAP": true,
	"SIGURG": true, "SIGWINCH": true, "SIGTSTP": true, "SIGTTIN": true,
	"SIGTTOU": true, "SIGXCPU": true, "SIGXFSZ": true, "SIGVTALRM": true,
	"SIGPROF": true, "SIGPWR": true, "SIGSYS": true,
}

// ValidateConfig validates config consistency — missing DependsOn references, etc.
func (c *Config) ValidateConfig() []string {
	var warnings []string
	for name, prog := range c.Programs {
		if prog.Command == "" {
			warnings = append(warnings, fmt.Sprintf("进程 %s 配置缺少 command 字段", name))
		}
		for _, dep := range prog.DependsOn {
			if _, ok := c.Programs[dep]; !ok {
				warnings = append(warnings, fmt.Sprintf("进程 %s 依赖了不存在的进程 %s", name, dep))
			}
			if dep == name {
				warnings = append(warnings, fmt.Sprintf("进程 %s 不能依赖自身", name))
			}
		}
		if prog.Name == "" {
			warnings = append(warnings, fmt.Sprintf("进程 %s 配置缺少 name 字段", name))
		}
		if prog.StopSignal != "" && !knownSignals[prog.StopSignal] {
			warnings = append(warnings, fmt.Sprintf("进程 %s 的 stopsignal '%s' 可能无效", name, prog.StopSignal))
		}
	}
	// Validate event listeners
	for name, el := range c.EventListeners {
		if el.Command == "" {
			warnings = append(warnings, fmt.Sprintf("事件监听器 %s 配置缺少 command 字段", name))
		}
		if len(el.Events) == 0 {
			warnings = append(warnings, fmt.Sprintf("事件监听器 %s 配置缺少 events 字段", name))
		}
		if el.BufferSize <= 0 {
			warnings = append(warnings, fmt.Sprintf("事件监听器 %s 的 buffer_size 必须大于0", name))
		}
		if el.StopSignal != "" && !knownSignals[el.StopSignal] {
			warnings = append(warnings, fmt.Sprintf("事件监听器 %s 的 stopsignal '%s' 可能无效", name, el.StopSignal))
		}
	}
	// Validate fcgi programs
	for name, prog := range c.FcgiPrograms {
		if prog.Socket == "" {
			warnings = append(warnings, fmt.Sprintf("fcgi-program %s 配置缺少 socket 字段", name))
		} else if !strings.HasPrefix(prog.Socket, "unix://") && !strings.HasPrefix(prog.Socket, "tcp://") {
			warnings = append(warnings, fmt.Sprintf("fcgi-program %s 的 socket '%s' 格式无效，需要 unix:// 或 tcp:// 前缀", name, prog.Socket))
		}
		if prog.Command == "" {
			warnings = append(warnings, fmt.Sprintf("fcgi-program %s 配置缺少 command 字段", name))
		}
		for _, dep := range prog.DependsOn {
			if _, ok := c.Programs[dep]; !ok {
				if _, ok2 := c.FcgiPrograms[dep]; !ok2 {
					warnings = append(warnings, fmt.Sprintf("fcgi-program %s 依赖了不存在的进程 %s", name, dep))
				}
			}
			if dep == name {
				warnings = append(warnings, fmt.Sprintf("fcgi-program %s 不能依赖自身", name))
			}
		}
	}
	// Detect dependency cycles (include fcgi programs)
	allProgs := make(map[string]*ProgramConfig)
	for k, v := range c.Programs {
		allProgs[k] = v
	}
	for k, v := range c.FcgiPrograms {
		allProgs[k] = v
	}
	if cycle := findDependencyCycle(allProgs); cycle != "" {
		warnings = append(warnings, cycle)
	}
	return warnings
}

// findDependencyCycle detects a cycle in DependsOn using DFS.
func findDependencyCycle(programs map[string]*ProgramConfig) string {
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully explored
	)
	color := make(map[string]int)
	parent := make(map[string]string)

	var dfs func(name string) string
	dfs = func(name string) string {
		color[name] = gray
		prog, ok := programs[name]
		if !ok {
			color[name] = black
			return ""
		}
		for _, dep := range prog.DependsOn {
			if color[dep] == gray {
				return fmt.Sprintf("检测到循环依赖: %s -> %s", name, dep)
			}
			if color[dep] == white {
				parent[dep] = name
				if cycle := dfs(dep); cycle != "" {
					return cycle
				}
			}
		}
		color[name] = black
		return ""
	}

	for name := range programs {
		color[name] = white
	}
	for name := range programs {
		if color[name] == white {
			if cycle := dfs(name); cycle != "" {
				return cycle
			}
		}
	}
	return ""
}

// expandTemplate expands %(program_name)s and %(process_num)Xd in a string.
func expandTemplate(tmpl string, programName string, processNum int) string {
	s := strings.ReplaceAll(tmpl, "%(program_name)s", programName)

	// Replace %(process_num)Xd patterns: %(process_num)d, %(process_num)02d, etc.
	// The marker is "%(process_num)" — the ")" is part of the marker.
	// After the marker comes the format like "d", "02d", etc.
	const marker = "%(process_num)"
	for {
		start := strings.Index(s, marker)
		if start < 0 {
			break
		}
		afterMarker := s[start+len(marker):]
		dIdx := strings.Index(afterMarker, "d")
		if dIdx < 0 {
			break
		}
		fmtPart := afterMarker[:dIdx+1] // "d" or "02d"
		if !isValidIntFormat(fmtPart) {
			break
		}
		fmtStr := "%" + fmtPart
		s = s[:start] + fmt.Sprintf(fmtStr, processNum) + s[start+len(marker)+dIdx+1:]
	}
	return s
}

// isValidIntFormat checks that s is of the form "d" or "02d" (digits followed by "d").
func isValidIntFormat(s string) bool {
	if len(s) < 1 || s[len(s)-1] != 'd' {
		return false
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// copyProgramConfig returns a deep copy of the given ProgramConfig.
func copyProgramConfig(cfg *ProgramConfig) *ProgramConfig {
	copyCfg := *cfg
	// Deep copy maps
	if cfg.Environment != nil {
		copyCfg.Environment = make(map[string]string, len(cfg.Environment))
		for k, v := range cfg.Environment {
			copyCfg.Environment[k] = v
		}
	}
	// Deep copy slices
	if cfg.DependsOn != nil {
		copyCfg.DependsOn = make([]string, len(cfg.DependsOn))
		copy(copyCfg.DependsOn, cfg.DependsOn)
	}
	if cfg.RestartCodes != nil {
		copyCfg.RestartCodes = make([]int, len(cfg.RestartCodes))
		copy(copyCfg.RestartCodes, cfg.RestartCodes)
	}
	if cfg.NoRestartCodes != nil {
		copyCfg.NoRestartCodes = make([]int, len(cfg.NoRestartCodes))
		copy(copyCfg.NoRestartCodes, cfg.NoRestartCodes)
	}
	if cfg.ExitCodes != nil {
		copyCfg.ExitCodes = make([]int, len(cfg.ExitCodes))
		copy(copyCfg.ExitCodes, cfg.ExitCodes)
	}
	return &copyCfg
}

// ExpandProgramConfig expands a single ProgramConfig into multiple instances
// based on NumProcs and ProcessName template. Returns a slice with one element
// when NumProcs <= 1 (no expansion needed).
func ExpandProgramConfig(cfg *ProgramConfig) []*ProgramConfig {
	if cfg.NumProcs <= 1 {
		cfg.NumProcs = 1
		return []*ProgramConfig{cfg}
	}

	var result []*ProgramConfig
	for i := 0; i < cfg.NumProcs; i++ {
		processNum := i + 1 // 1-based numbering like supervisord
		copyCfg := copyProgramConfig(cfg)

		// Expand name from template
		copyCfg.Name = expandTemplate(cfg.ProcessName, cfg.Name, processNum)

		// Expand templates in other string fields
		copyCfg.Command = expandTemplate(cfg.Command, cfg.Name, processNum)
		copyCfg.Directory = expandTemplate(cfg.Directory, cfg.Name, processNum)
		copyCfg.StdoutLogFile = expandTemplate(cfg.StdoutLogFile, cfg.Name, processNum)
		copyCfg.StderrLogFile = expandTemplate(cfg.StderrLogFile, cfg.Name, processNum)
		copyCfg.StdinFile = expandTemplate(cfg.StdinFile, cfg.Name, processNum)

		// Expand environment values
		for k, v := range copyCfg.Environment {
			copyCfg.Environment[k] = expandTemplate(v, cfg.Name, processNum)
		}

		// Reset NumProcs on the copy
		copyCfg.NumProcs = 1

		result = append(result, copyCfg)
	}
	return result
}
