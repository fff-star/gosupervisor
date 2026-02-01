package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
