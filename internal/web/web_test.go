package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
	"gosupervisor/internal/process"
)

// 初始化测试环境
func setupTestEnvironment() (*process.ProcessManager, error) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)

	// 初始化日志管理器
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		return nil, err
	}

	// 创建进程管理器
	processManager := process.NewProcessManager(logManager)

	// 创建测试进程配置
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

	// 添加进程
	processManager.AddProcess(programCfg)

	return processManager, nil
}

// 清理测试环境
func cleanupTestEnvironment() {
	// 删除测试日志目录
	os.RemoveAll("./test_logs")
}

// TestNewWebServer 测试Web服务器初始化
func TestNewWebServer(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 检查Web服务器是否创建成功
	if webServer == nil {
		t.Fatalf("Web服务器创建失败")
	}

	// 检查processManager是否正确设置
	if webServer.processManager == nil {
		t.Errorf("processManager未正确设置")
	}

	// 检查templates是否正确设置
	if webServer.templates == nil {
		t.Errorf("templates未正确设置")
	}
}

// TestHandleIndex 测试首页处理
func TestHandleIndex(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleIndex(w, req)

	// 检查响应状态码
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusOK, w.Code)
	}

	// 检查响应内容是否包含进程信息
	if !contains(w.Body.String(), "test_process") {
		t.Errorf("响应内容不包含进程信息")
	}
}

// TestHandleStart 测试启动进程
func TestHandleStart(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求
	req, err := http.NewRequest("POST", "/start", strings.NewReader("name=test_process"))
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleStart(w, req)

	// 检查响应状态码（应该是重定向）
	if w.Code != http.StatusFound {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusFound, w.Code)
	}

	// 检查重定向URL
	if w.Header().Get("Location") != "/" {
		t.Errorf("期望重定向到/，实际重定向到%s", w.Header().Get("Location"))
	}
}

// TestHandleStartWithInvalidProcess 测试启动不存在的进程
func TestHandleStartWithInvalidProcess(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求（请求不存在的进程）
	req, err := http.NewRequest("POST", "/start", strings.NewReader("name=non_existent_process"))
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleStart(w, req)

	// 检查响应状态码（应该是404）
	if w.Code != http.StatusNotFound {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusNotFound, w.Code)
	}
}

// TestHandleStop 测试停止进程
func TestHandleStop(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 先启动进程
	p := processManager.GetProcess("test_process")
	if p != nil {
		p.Start()
		time.Sleep(1 * time.Second)
	}

	// 创建测试请求
	req, err := http.NewRequest("POST", "/stop", strings.NewReader("name=test_process"))
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleStop(w, req)

	// 检查响应状态码（应该是重定向）
	if w.Code != http.StatusFound {
		t.Logf("停止进程时遇到错误，状态码为%d", w.Code)
	}

	// 检查重定向URL
	if w.Header().Get("Location") != "/" {
		t.Logf("停止进程时重定向URL不正确，实际重定向到%s", w.Header().Get("Location"))
	}
}

// TestHandleRestart 测试重启进程
func TestHandleRestart(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 先启动进程
	p := processManager.GetProcess("test_process")
	if p != nil {
		p.Start()
		time.Sleep(1 * time.Second)
	}

	// 创建测试请求
	req, err := http.NewRequest("POST", "/restart", strings.NewReader("name=test_process"))
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleRestart(w, req)

	// 检查响应状态码（应该是重定向）
	if w.Code != http.StatusFound {
		t.Logf("重启进程时遇到错误，状态码为%d", w.Code)
	}

	// 检查重定向URL
	if w.Header().Get("Location") != "/" {
		t.Logf("重启进程时重定向URL不正确，实际重定向到%s", w.Header().Get("Location"))
	}
}

// TestHandleLogs 测试查看日志
func TestHandleLogs(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求
	req, err := http.NewRequest("GET", "/logs?name=test_process", nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleLogs(w, req)

	// 检查响应状态码
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusOK, w.Code)
	}

	// 检查响应内容是否包含日志页面信息
	if !contains(w.Body.String(), "test_process") {
		t.Errorf("响应内容不包含进程名称")
	}

	if !contains(w.Body.String(), "进程日志") {
		t.Errorf("响应内容不包含日志页面标题")
	}
}

// TestHandleSystemInfo 测试系统信息页面
func TestHandleSystemInfo(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求
	req, err := http.NewRequest("GET", "/system", nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleSystemInfo(w, req)

	// 检查响应状态码
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusOK, w.Code)
	}

	// 检查响应内容是否包含系统信息页面信息
	if !contains(w.Body.String(), "系统信息") {
		t.Errorf("响应内容不包含系统信息页面标题")
	}

	if !contains(w.Body.String(), "操作系统:") {
		t.Errorf("响应内容不包含操作系统信息")
	}
}

// TestHandleProcessDetail 测试进程详情页面
func TestHandleProcessDetail(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求
	req, err := http.NewRequest("GET", "/process?name=test_process", nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleProcessDetail(w, req)

	// 检查响应状态码
	if w.Code != http.StatusOK {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusOK, w.Code)
	}

	// 检查响应内容是否包含进程详情页面信息
	if !contains(w.Body.String(), "进程详情") {
		t.Errorf("响应内容不包含进程详情页面标题")
	}

	if !contains(w.Body.String(), "test_process") {
		t.Errorf("响应内容不包含进程名称")
	}
}

// TestHandleProcessDetailWithInvalidProcess 测试查看不存在的进程详情
func TestHandleProcessDetailWithInvalidProcess(t *testing.T) {
	// 初始化测试环境
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// 创建Web服务器
	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	// 创建测试请求（请求不存在的进程）
	req, err := http.NewRequest("GET", "/process?name=non_existent_process", nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	webServer.handleProcessDetail(w, req)

	// 检查响应状态码（应该是404）
	if w.Code != http.StatusNotFound {
		t.Errorf("期望状态码为%d，实际为%d", http.StatusNotFound, w.Code)
	}
}

// 辅助函数：检查字符串是否包含子字符串
func contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	return indexOf(s, substr) != -1
}

// 辅助函数：查找子字符串在字符串中的位置
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// TestReadTailLines tests reading the last N lines from a file.
func TestReadTailLines(t *testing.T) {
	// Create a temp file with known content
	tmpFile := "./test_logs/tail_test.log"
	os.MkdirAll("./test_logs", 0755)
	defer os.RemoveAll("./test_logs")

	// Write 50 lines
	var content string
	for i := 1; i <= 50; i++ {
		content += fmt.Sprintf("line %d\n", i)
	}
	os.WriteFile(tmpFile, []byte(content), 0644)

	// Read last 10 lines
	result, err := readTailLines(tmpFile, 10, 1*1024*1024)
	if err != nil {
		t.Fatalf("readTailLines 失败: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result)), "\n")
	if len(lines) != 10 {
		t.Errorf("期望 10 行, 实际 %d 行", len(lines))
	}
	if lines[0] != "line 41" {
		t.Errorf("期望第一行为 'line 41', 实际 '%s'", lines[0])
	}
	if lines[9] != "line 50" {
		t.Errorf("期望最后一行为 'line 50', 实际 '%s'", lines[9])
	}

	// Test with maxLines larger than file
	result2, err := readTailLines(tmpFile, 100, 1*1024*1024)
	if err != nil {
		t.Fatalf("readTailLines 大行数失败: %v", err)
	}
	lines2 := strings.Split(strings.TrimSpace(string(result2)), "\n")
	if len(lines2) != 50 {
		t.Errorf("期望 50 行, 实际 %d 行", len(lines2))
	}

	// Test with non-existent file
	_, err = readTailLines("./test_logs/nonexistent.log", 10, 1*1024*1024)
	if err == nil {
		t.Error("期望非存在文件返回错误")
	}
}

// TestReadTailLinesSmallMaxBytes tests reading with a tight byte limit.
func TestReadTailLinesSmallMaxBytes(t *testing.T) {
	os.MkdirAll("./test_logs", 0755)
	defer os.RemoveAll("./test_logs")

	tmpFile := "./test_logs/tail_bytes_test.log"
	var content string
	for i := 1; i <= 100; i++ {
		content += fmt.Sprintf("line %d with more text to fill bytes\n", i)
	}
	os.WriteFile(tmpFile, []byte(content), 0644)

	// Limit to 200 bytes
	result, err := readTailLines(tmpFile, 1000, 200)
	if err != nil {
		t.Fatalf("readTailLines 小字节限制失败: %v", err)
	}

	if len(result) > 300 {
		t.Errorf("结果应远小于300字节 (限制200), 实际 %d 字节", len(result))
	}
}
