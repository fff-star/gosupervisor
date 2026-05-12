package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestLoadConfig(t *testing.T) {
	// 测试INI配置文件
	testINIConfigPath := filepath.Join("testdata", "test_config.ini")
	testConfig(t, testINIConfigPath)

	// 测试YAML配置文件
	testYAMLConfigPath := filepath.Join("testdata", "test_config.yaml")
	testConfig(t, testYAMLConfigPath)

	// 测试JSON配置文件
	testJSONConfigPath := filepath.Join("testdata", "test_config.json")
	testConfig(t, testJSONConfigPath)
}

func testConfig(t *testing.T, configPath string) {
	// 加载配置文件
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("加载配置文件 %s 失败: %v", configPath, err)
	}

	// 检查进程数量
	if len(cfg.Programs) != 2 {
		t.Errorf("期望有2个进程，实际有%d个", len(cfg.Programs))
	}

	// 检查第一个进程
	test1 := cfg.Programs["test1"]
	if test1 == nil {
		t.Fatalf("进程test1不存在")
	}

	if test1.Command != "echo \"Hello, World!\"" {
		t.Errorf("期望command为'echo \"Hello, World!\"'，实际为'%s'", test1.Command)
	}

	if test1.Directory != "." {
		t.Errorf("期望directory为'.'，实际为'%s'", test1.Directory)
	}

	if !test1.AutoStart {
		t.Errorf("期望autostart为true，实际为false")
	}

	if !test1.AutoRestart {
		t.Errorf("期望autorestart为true，实际为false")
	}

	if test1.StartSecs != 1 {
		t.Errorf("期望startsecs为1，实际为%d", test1.StartSecs)
	}

	if test1.StartRetries != 3 {
		t.Errorf("期望startretries为3，实际为%d", test1.StartRetries)
	}

	if test1.User != "administrator" {
		t.Errorf("期望user为'administrator'，实际为'%s'", test1.User)
	}

	if test1.Environment["TEST_VAR"] != "test_value" {
		t.Errorf("期望TEST_VAR为'test_value'，实际为'%s'", test1.Environment["TEST_VAR"])
	}

	// 检查第二个进程
	test2 := cfg.Programs["test2"]
	if test2 == nil {
		t.Fatalf("进程test2不存在")
	}

	if test2.Command != "ping localhost -t" {
		t.Errorf("期望command为'ping localhost -t'，实际为'%s'", test2.Command)
	}

	if !test2.AutoRestart {
		t.Errorf("期望autorestart为true，实际为false")
	}

	if test2.AutoStart {
		t.Errorf("期望autostart为false，实际为true")
	}

	if test2.StartSecs != 2 {
		t.Errorf("期望startsecs为2，实际为%d", test2.StartSecs)
	}

	if test2.StartRetries != 5 {
		t.Errorf("期望startretries为5，实际为%d", test2.StartRetries)
	}
}

// TestLoadConfigWithError 测试加载配置文件时的错误处理
func TestLoadConfigWithError(t *testing.T) {
	// 测试不存在的文件
	nonExistentFile := "non_existent_config.ini"
	_, err := LoadConfig(nonExistentFile)
	if err == nil {
		t.Errorf("期望加载不存在的文件失败，但成功了")
	}

	// 测试创建一个无效的YAML文件
	invalidYAMLFile := "invalid_config.yaml"
	invalidYAMLContent := "programs: test1: command: echo \"Hello\"" // 无效的YAML格式
	if err := os.WriteFile(invalidYAMLFile, []byte(invalidYAMLContent), 0644); err != nil {
		t.Fatalf("创建无效YAML文件失败: %v", err)
	}
	defer os.Remove(invalidYAMLFile)

	_, err = LoadConfig(invalidYAMLFile)
	if err == nil {
		t.Errorf("期望加载无效的YAML文件失败，但成功了")
	}

	// 测试创建一个无效的JSON文件
	invalidJSONFile := "invalid_config.json"
	invalidJSONContent := "{programs: {test1: {command: \"echo Hello\"}}}" // 无效的JSON格式
	if err := os.WriteFile(invalidJSONFile, []byte(invalidJSONContent), 0644); err != nil {
		t.Fatalf("创建无效JSON文件失败: %v", err)
	}
	defer os.Remove(invalidJSONFile)

	_, err = LoadConfig(invalidJSONFile)
	if err == nil {
		t.Errorf("期望加载无效的JSON文件失败，但成功了")
	}
}

// TestLoadConfigWithDefaultValues 测试配置文件的默认值
func TestLoadConfigWithDefaultValues(t *testing.T) {
	// 创建一个只有基本配置的测试文件
	minimalConfigFile := "minimal_config.ini"
	minimalConfigContent := `[program:minimal]
command=echo "Hello"
`
	if err := os.WriteFile(minimalConfigFile, []byte(minimalConfigContent), 0644); err != nil {
		t.Fatalf("创建最小配置文件失败: %v", err)
	}
	defer os.Remove(minimalConfigFile)

	// 加载配置文件
	cfg, err := LoadConfig(minimalConfigFile)
	if err != nil {
		t.Fatalf("加载最小配置文件失败: %v", err)
	}

	// 检查进程是否创建成功
	minimal := cfg.Programs["minimal"]
	if minimal == nil {
		t.Fatalf("进程minimal不存在")
	}

	// 检查默认值
	if minimal.AutoStart != true {
		t.Errorf("期望AutoStart默认为true，实际为%v", minimal.AutoStart)
	}

	if minimal.AutoRestart != true {
		t.Errorf("期望AutoRestart默认为true，实际为%v", minimal.AutoRestart)
	}

	if minimal.StartSecs != 1 {
		t.Errorf("期望StartSecs默认为1，实际为%d", minimal.StartSecs)
	}

	if minimal.StartRetries != 3 {
		t.Errorf("期望StartRetries默认为3，实际为%d", minimal.StartRetries)
	}

	if minimal.StopSecs != 10 {
		t.Errorf("期望StopSecs默认为10，实际为%d", minimal.StopSecs)
	}

	if minimal.StopSignal != "SIGTERM" {
		t.Errorf("期望StopSignal默认为SIGTERM，实际为%s", minimal.StopSignal)
	}

	if minimal.Environment == nil {
		t.Errorf("期望Environment默认为空map，实际为nil")
	}

	if minimal.DependsOn == nil {
		t.Errorf("期望DependsOn默认为空slice，实际为nil")
	}
}

// TestLoadConfigWithEnvironmentVariables 测试环境变量的解析
func TestLoadConfigWithEnvironmentVariables(t *testing.T) {
	// 创建一个包含复杂环境变量的测试文件
	envConfigFile := "env_config.ini"
	envConfigContent := `[program:test_env]
command=echo "Hello"
environment=VAR1=value1,VAR2=value2 with spaces,VAR3=123
`
	if err := os.WriteFile(envConfigFile, []byte(envConfigContent), 0644); err != nil {
		t.Fatalf("创建环境变量配置文件失败: %v", err)
	}
	defer os.Remove(envConfigFile)

	// 加载配置文件
	cfg, err := LoadConfig(envConfigFile)
	if err != nil {
		t.Fatalf("加载环境变量配置文件失败: %v", err)
	}

	// 检查进程
	testEnv := cfg.Programs["test_env"]
	if testEnv == nil {
		t.Fatalf("进程test_env不存在")
	}

	// 检查环境变量
	if testEnv.Environment["VAR1"] != "value1" {
		t.Errorf("期望VAR1为'value1'，实际为'%s'", testEnv.Environment["VAR1"])
	}

	if testEnv.Environment["VAR2"] != "value2 with spaces" {
		t.Errorf("期望VAR2为'value2 with spaces'，实际为'%s'", testEnv.Environment["VAR2"])
	}

	if testEnv.Environment["VAR3"] != "123" {
		t.Errorf("期望VAR3为'123'，实际为'%s'", testEnv.Environment["VAR3"])
	}
}

// TestLoadConfigWithDependsOn 测试依赖关系的解析
func TestLoadConfigWithDependsOn(t *testing.T) {
	// 创建一个包含依赖关系的测试文件
	depConfigFile := "dep_config.ini"
	depConfigContent := `[program:app]
command=echo "App"
dependson=db,cache

[program:db]
command=echo "DB"

[program:cache]
command=echo "Cache"
`
	if err := os.WriteFile(depConfigFile, []byte(depConfigContent), 0644); err != nil {
		t.Fatalf("创建依赖关系配置文件失败: %v", err)
	}
	defer os.Remove(depConfigFile)

	// 加载配置文件
	cfg, err := LoadConfig(depConfigFile)
	if err != nil {
		t.Fatalf("加载依赖关系配置文件失败: %v", err)
	}

	// 检查进程
	app := cfg.Programs["app"]
	if app == nil {
		t.Fatalf("进程app不存在")
	}

	// 检查依赖关系
	if len(app.DependsOn) != 2 {
		t.Errorf("期望有2个依赖，实际有%d个", len(app.DependsOn))
	}

	if app.DependsOn[0] != "db" {
		t.Errorf("期望第一个依赖为'db'，实际为'%s'", app.DependsOn[0])
	}

	if app.DependsOn[1] != "cache" {
		t.Errorf("期望第二个依赖为'cache'，实际为'%s'", app.DependsOn[1])
	}
}
// TestYAMLJSONBoolDefaults tests that explicit false values are preserved
// and absent keys default to true in YAML/JSON configs.
func TestYAMLJSONBoolDefaults(t *testing.T) {
	for _, ext := range []string{".yaml", ".json"} {
		t.Run(ext, func(t *testing.T) {
			cfg, err := LoadConfig(filepath.Join("testdata", "test_config"+ext))
			if err != nil {
				t.Fatalf("加载配置失败: %v", err)
			}

			// test2 has autostart: false explicitly -> should stay false
			test2 := cfg.Programs["test2"]
			if test2.AutoStart {
				t.Errorf("%s: test2.autostart 显式设为 false，但被默认值覆盖为 true", ext)
			}
			if !test2.AutoRestart {
				t.Errorf("%s: test2.autorestart 显式设为 true，应为 true", ext)
			}

			// test2 does NOT have redirectstdout/redirectstderr -> should default to true
			if !test2.RedirectStdout {
				t.Errorf("%s: test2.redirectstdout 未设置，应默认为 true", ext)
			}
			if !test2.RedirectStderr {
				t.Errorf("%s: test2.redirectstderr 未设置，应默认为 true", ext)
			}

			// test1 has autostart: true explicitly
			test1 := cfg.Programs["test1"]
			if !test1.AutoStart {
				t.Errorf("%s: test1.autostart 显式设为 true，应为 true", ext)
			}
		})
	}
}

// TestProgramConfigStructTags tests that YAML/JSON struct tags are present
// and correct for all fields.
func TestProgramConfigStructTags(t *testing.T) {
	// Verify that YAML and JSON parsing work correctly with struct tags
	cfg, err := LoadConfig("testdata/test_config.yaml")
	if err != nil {
		t.Fatalf("加载 YAML 配置失败: %v", err)
	}

	test1 := cfg.Programs["test1"]
	if test1.Command == "" {
		t.Error("command 标签导致字段未正确解析")
	}
	if test1.StdoutLogMaxBytes == 0 {
		t.Error("stdoutlogmaxbytes 标签导致字段未正确解析")
	}
	if test1.StderrLogBackupCount == 0 {
		t.Error("stderrlogbackupcount 标签导致字段未正确解析")
	}
	if len(test1.DependsOn) != 0 {
		t.Error("dependson 标签导致字段未正确解析")
	}
}

// TestServerURLRemoved tests that the dead ServerURL field no longer exists.
func TestServerURLRemoved(t *testing.T) {
	cfg, err := LoadConfig("testdata/test_config.ini")
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	// Verify that all programs load correctly without the ServerURL field
	test1 := cfg.Programs["test1"]
	if test1 == nil {
		t.Fatal("test1 应为 nil")
	}
	// No ServerURL to check — it was removed
}

// TestNewConfigFieldsINI tests parsing of all new config fields from INI format.
func TestNewConfigFieldsINI(t *testing.T) {
	configFile := "new_fields_config.ini"
	content := `[program:full]
command=sleep 60
group=web
healthcheckurl=http://localhost:8080/health
healthcheckinterval=10
healthchecktimeout=3
healthcheckunhealthythreshold=5
prestartscript=echo "starting"
poststopscript=echo "stopped"
restartmaxcount=5
restartwindowsecs=120
restartcodes=1,2,3
norestartcodes=0,143
cgrouppath=/sys/fs/cgroup/myapp
webhookurl=http://hooks.example.com/notify
stdinfile=/tmp/stdin.txt
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}
	defer os.Remove(configFile)

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	p := cfg.Programs["full"]
	if p == nil {
		t.Fatal("进程 full 不存在")
	}

	if p.Group != "web" {
		t.Errorf("Group 期望 'web', 实际 '%s'", p.Group)
	}
	if p.HealthCheckURL != "http://localhost:8080/health" {
		t.Errorf("HealthCheckURL 不匹配: '%s'", p.HealthCheckURL)
	}
	if p.HealthCheckInterval != 10 {
		t.Errorf("HealthCheckInterval 期望 10, 实际 %d", p.HealthCheckInterval)
	}
	if p.HealthCheckTimeout != 3 {
		t.Errorf("HealthCheckTimeout 期望 3, 实际 %d", p.HealthCheckTimeout)
	}
	if p.HealthCheckUnhealthyThreshold != 5 {
		t.Errorf("HealthCheckUnhealthyThreshold 期望 5, 实际 %d", p.HealthCheckUnhealthyThreshold)
	}
	if p.PreStartScript != "echo \"starting\"" {
		t.Errorf("PreStartScript 不匹配: '%s'", p.PreStartScript)
	}
	if p.PostStopScript != "echo \"stopped\"" {
		t.Errorf("PostStopScript 不匹配: '%s'", p.PostStopScript)
	}
	if p.RestartMaxCount != 5 {
		t.Errorf("RestartMaxCount 期望 5, 实际 %d", p.RestartMaxCount)
	}
	if p.RestartWindowSecs != 120 {
		t.Errorf("RestartWindowSecs 期望 120, 实际 %d", p.RestartWindowSecs)
	}
	if len(p.RestartCodes) != 3 || p.RestartCodes[0] != 1 {
		t.Errorf("RestartCodes 期望 [1,2,3], 实际 %v", p.RestartCodes)
	}
	if len(p.NoRestartCodes) != 2 || p.NoRestartCodes[1] != 143 {
		t.Errorf("NoRestartCodes 期望 [0,143], 实际 %v", p.NoRestartCodes)
	}
	if p.CgroupPath != "/sys/fs/cgroup/myapp" {
		t.Errorf("CgroupPath 不匹配: '%s'", p.CgroupPath)
	}
	if p.WebhookURL != "http://hooks.example.com/notify" {
		t.Errorf("WebhookURL 不匹配: '%s'", p.WebhookURL)
	}
	if p.StdinFile != "/tmp/stdin.txt" {
		t.Errorf("StdinFile 不匹配: '%s'", p.StdinFile)
	}
}

// TestNewConfigFieldsDefaults tests default values for new config fields.
func TestNewConfigFieldsDefaults(t *testing.T) {
	configFile := "defaults_config.ini"
	content := `[program:minimal]
command=echo hi
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}
	defer os.Remove(configFile)

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	p := cfg.Programs["minimal"]
	if p == nil {
		t.Fatal("进程 minimal 不存在")
	}

	// Health check defaults
	if p.HealthCheckInterval != 30 {
		t.Errorf("HealthCheckInterval 默认应为 30, 实际 %d", p.HealthCheckInterval)
	}
	if p.HealthCheckTimeout != 5 {
		t.Errorf("HealthCheckTimeout 默认应为 5, 实际 %d", p.HealthCheckTimeout)
	}
	if p.HealthCheckUnhealthyThreshold != 3 {
		t.Errorf("HealthCheckUnhealthyThreshold 默认应为 3, 实际 %d", p.HealthCheckUnhealthyThreshold)
	}
	if p.RestartWindowSecs != 60 {
		t.Errorf("RestartWindowSecs 默认应为 60, 实际 %d", p.RestartWindowSecs)
	}
	if p.RestartCodes == nil {
		t.Error("RestartCodes 不应为 nil")
	}
	if p.NoRestartCodes == nil {
		t.Error("NoRestartCodes 不应为 nil")
	}
}

// TestValidateConfig tests the config validation for DependsOn references.
func TestValidateConfig(t *testing.T) {
	configFile := "validate_config.ini"
	content := `[program:app]
command=echo app
dependson=db,cache

[program:db]
command=echo db
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}
	defer os.Remove(configFile)

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	warnings := cfg.ValidateConfig()
	if len(warnings) != 1 {
		t.Fatalf("期望 1 个警告, 实际 %d", len(warnings))
	}
	if !strings.Contains(warnings[0], "cache") {
		t.Errorf("警告应包含缺失的依赖 'cache', 实际: %s", warnings[0])
	}
}

// TestValidateConfigNoWarnings tests validation with all dependencies present.
func TestValidateConfigNoWarnings(t *testing.T) {
	configFile := "valid_config.ini"
	content := `[program:app]
command=echo app
dependson=db

[program:db]
command=echo db
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建配置文件失败: %v", err)
	}
	defer os.Remove(configFile)

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	warnings := cfg.ValidateConfig()
	if len(warnings) != 0 {
		t.Errorf("期望无警告, 实际 %d: %v", len(warnings), warnings)
	}
}

// --- Fuzz tests ---

func FuzzLoadINI(f *testing.F) {
	f.Add(`[program:test]
command=echo hello
autostart=true
`)
	f.Add(`[program:a]
command=sleep 1
dependson=b,c
[program:b]
command=echo b
[program:c]
command=echo c
`)

	f.Fuzz(func(t *testing.T, data string) {
		path := filepath.Join(t.TempDir(), "fuzz_config.ini")
		os.WriteFile(path, []byte(data), 0644)
		cfg, err := LoadConfig(path)
		if err != nil && cfg != nil {
			t.Errorf("error 返回但 cfg 非 nil")
		}
		if cfg != nil {
			cfg.ValidateConfig()
		}
	})
}

func FuzzLoadYAML(f *testing.F) {
	f.Add(`programs:
  test:
    command: echo hello
    autostart: true
`)
	f.Add(`programs:
  a:
    command: "sleep 1"
    dependson: [b, c]
  b:
    command: echo b
`)

	f.Fuzz(func(t *testing.T, data string) {
		path := filepath.Join(t.TempDir(), "fuzz_config.yaml")
		os.WriteFile(path, []byte(data), 0644)
		cfg, err := LoadConfig(path)
		if err != nil && cfg != nil {
			t.Errorf("error 返回但 cfg 非 nil")
		}
		if cfg != nil {
			cfg.ValidateConfig()
		}
	})
}

func FuzzLoadJSON(f *testing.F) {
	f.Add(`{"programs":{"test":{"command":"echo hello","autostart":true}}}`)
	f.Add(`{"programs":{"a":{"command":"sleep 1"},"b":{"command":"echo b"}}}`)

	f.Fuzz(func(t *testing.T, data string) {
		path := filepath.Join(t.TempDir(), "fuzz_config.json")
		os.WriteFile(path, []byte(data), 0644)
		cfg, err := LoadConfig(path)
		if err != nil && cfg != nil {
			t.Errorf("error 返回但 cfg 非 nil")
		}
		if cfg != nil {
			cfg.ValidateConfig()
		}
	})
}

func TestLoadINISupervisordSection(t *testing.T) {
	tmpDir := t.TempDir()
	iniPath := filepath.Join(tmpDir, "test_supervisord.ini")
	content := `[supervisord]
webaddr = :9000
webuser = admin
webpass = secret
metricsaddr = :9100
socketpath = /tmp/test.sock
statefile = /tmp/state.json
logdir = /var/log/gosupervisor

[program:app]
command = sleep 60
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test INI: %v", err)
	}

	cfg, err := LoadConfig(iniPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server == nil {
		t.Fatal("expected Server config, got nil")
	}
	if cfg.Server.WebAddr != ":9000" {
		t.Errorf("expected WebAddr :9000, got %s", cfg.Server.WebAddr)
	}
	if cfg.Server.WebUser != "admin" {
		t.Errorf("expected WebUser admin, got %s", cfg.Server.WebUser)
	}
	if cfg.Server.WebPass != "secret" {
		t.Errorf("expected WebPass secret, got %s", cfg.Server.WebPass)
	}
	if cfg.Server.MetricsAddr != ":9100" {
		t.Errorf("expected MetricsAddr :9100, got %s", cfg.Server.MetricsAddr)
	}
	if cfg.Server.SocketPath != "/tmp/test.sock" {
		t.Errorf("expected SocketPath /tmp/test.sock, got %s", cfg.Server.SocketPath)
	}
	if cfg.Server.StateFile != "/tmp/state.json" {
		t.Errorf("expected StateFile /tmp/state.json, got %s", cfg.Server.StateFile)
	}
	if cfg.Server.LogDir != "/var/log/gosupervisor" {
		t.Errorf("expected LogDir /var/log/gosupervisor, got %s", cfg.Server.LogDir)
	}
	// Verify program still correctly parsed
	if _, ok := cfg.Programs["app"]; !ok {
		t.Error("expected program 'app' to still be parsed")
	}
}

func TestLoadINIWithoutSupervisordSection(t *testing.T) {
	tmpDir := t.TempDir()
	iniPath := filepath.Join(tmpDir, "test_noserver.ini")
	content := `[program:app]
command = sleep 60
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test INI: %v", err)
	}

	cfg, err := LoadConfig(iniPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server != nil {
		t.Error("expected nil Server when no [supervisord] section")
	}
}
