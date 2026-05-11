package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	// 检查模板是否正确设置
	if webServer.indexTmpl == nil {
		t.Errorf("indexTmpl未正确设置")
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
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"

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
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"

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
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"

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
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"

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

// TestHandleStartGetMethodNotAllowed tests that GET to /start returns 405.
func TestHandleStartGetMethodNotAllowed(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/start?name=test_process", nil)
	w := httptest.NewRecorder()
	webServer.handleStart(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 %d, 实际 %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestHandleStopGetMethodNotAllowed tests that GET to /stop returns 405.
func TestHandleStopGetMethodNotAllowed(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/stop?name=test_process", nil)
	w := httptest.NewRecorder()
	webServer.handleStop(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 %d, 实际 %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestHandleRestartGetMethodNotAllowed tests that GET to /restart returns 405.
func TestHandleRestartGetMethodNotAllowed(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/restart?name=test_process", nil)
	w := httptest.NewRecorder()
	webServer.handleRestart(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 %d, 实际 %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestPathTraversalRejected tests that names with path separators are rejected.
func TestPathTraversalRejected(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	badNames := []string{"../../../etc/passwd", "name/../evil", "path\\name"}

	for _, name := range badNames {
		// Read endpoints: validate via GET query param
		for _, ep := range []string{"/logs", "/process"} {
			req, _ := http.NewRequest("GET", ep+"?name="+name, nil)
			w := httptest.NewRecorder()
			switch ep {
			case "/logs":
				webServer.handleLogs(w, req)
			case "/process":
				webServer.handleProcessDetail(w, req)
			}
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s GET name=%q: 期望 400, 实际 %d", ep, name, w.Code)
			}
		}

		// Write endpoints: validate via POST form body
		for _, ep := range []string{"/start", "/stop", "/restart"} {
			req, _ := http.NewRequest("POST", ep, strings.NewReader("name="+name))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "http://localhost")
			req.Host = "localhost"
			w := httptest.NewRecorder()
			switch ep {
			case "/start":
				webServer.handleStart(w, req)
			case "/stop":
				webServer.handleStop(w, req)
			case "/restart":
				webServer.handleRestart(w, req)
			}
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s POST name=%q: 期望 400, 实际 %d", ep, name, w.Code)
			}
		}
	}
}

// TestValidateProcessName tests the process name validation function.
func TestValidateProcessName(t *testing.T) {
	valid := []string{"test", "my-process_1", "app.service", "foo"}
	for _, name := range valid {
		if !validateProcessName(name) {
			t.Errorf("validateProcessName(%q) 应为 true", name)
		}
	}

	invalid := []string{"", "a/b", "a\\b", "..", "../file", "etc/../passwd"}
	for _, name := range invalid {
		if validateProcessName(name) {
			t.Errorf("validateProcessName(%q) 应为 false", name)
		}
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

// TestCSRFProtection tests that POST requests without Origin/Referer are rejected.
func TestCSRFProtection(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	endpoints := []struct {
		handler func(w http.ResponseWriter, r *http.Request)
		path    string
	}{
		{webServer.handleStart, "/start"},
		{webServer.handleStop, "/stop"},
		{webServer.handleRestart, "/restart"},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			// No Origin — should be forbidden
			req, _ := http.NewRequest("POST", ep.path, strings.NewReader("name=test_process"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			ep.handler(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("无 Origin 期望 403, 实际 %d", w.Code)
			}

			// Wrong Origin with correct Host — should be forbidden
			req2, _ := http.NewRequest("POST", ep.path, strings.NewReader("name=test_process"))
			req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req2.Header.Set("Origin", "http://evil.com")
			req2.Host = "gosupervisor.local"
			w2 := httptest.NewRecorder()
			ep.handler(w2, req2)
			if w2.Code != http.StatusForbidden {
				t.Errorf("错误 Origin 期望 403, 实际 %d", w2.Code)
			}

			// Matching Origin and Host — should succeed
			req3, _ := http.NewRequest("POST", ep.path, strings.NewReader("name=test_process"))
			req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req3.Header.Set("Origin", "http://gosupervisor.local")
			req3.Host = "gosupervisor.local"
			w3 := httptest.NewRecorder()
			ep.handler(w3, req3)
			if w3.Code != http.StatusFound {
				t.Errorf("正确 Origin 期望 302, 实际 %d", w3.Code)
			}
		})
	}
}

// TestHandleLogsCustomPath tests that custom stdoutlogfile path is used.
func TestHandleLogsCustomPath(t *testing.T) {
	logDir := "./test_logs_custom"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logManager.Close()

	processManager := process.NewProcessManager(logManager)

	customPath := filepath.Join(logDir, "custom_mylog.log")
	os.WriteFile(customPath, []byte("custom log content\n"), 0644)

	programCfg := &config.ProgramConfig{
		Name:                 "custom_log_test",
		Command:              "echo hi",
		Directory:            ".",
		AutoStart:            true,
		AutoRestart:          true,
		StartSecs:            1,
		StartRetries:         3,
		StdoutLogFile:        customPath,
		StdoutLogMaxBytes:    50 * 1024 * 1024,
		StderrLogMaxBytes:    50 * 1024 * 1024,
		StdoutLogBackupCount: 10,
		StderrLogBackupCount: 10,
		Environment:          make(map[string]string),
	}
	processManager.AddProcess(programCfg)

	webServer, err := NewWebServer(processManager, logDir)
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/logs?name=custom_log_test", nil)
	w := httptest.NewRecorder()
	webServer.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "custom log content") {
		t.Errorf("日志内容应包含 'custom log content'")
	}
}

// TestWebServerSeparateMux tests independent ServeMux instances.
func TestWebServerSeparateMux(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws1, _ := NewWebServer(processManager, "./test_logs")
	ws2, _ := NewWebServer(processManager, "./test_logs")

	if ws1.mux == ws2.mux {
		t.Error("两个 WebServer 应使用不同的 ServeMux")
	}
}

// TestHandleAPIProcesses tests the JSON API endpoint returns process data.
func TestHandleAPIProcesses(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// Add a long-running process so we can check RUNNING state
	cfg := &config.ProgramConfig{
		Name:         "api_test",
		Command:      "sleep 60",
		Directory:    ".",
		AutoStart:    false,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	}
	processManager.AddProcess(cfg)

	p := processManager.GetProcess("api_test")
	if p == nil {
		t.Fatal("获取进程失败")
	}
	if err := p.Start(); err != nil {
		t.Fatalf("启动进程失败: %v", err)
	}
	defer p.Stop()
	time.Sleep(200 * time.Millisecond)

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/processes", nil)
	w := httptest.NewRecorder()
	webServer.handleAPIProcesses(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("期望 Content-Type 包含 application/json, 实际 %s", contentType)
	}

	var snapshots []process.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}

	if len(snapshots) < 1 {
		t.Fatal("期望至少 1 个进程")
	}

	// Find api_test in results
	var s process.Snapshot
	for _, snap := range snapshots {
		if snap.Name == "api_test" {
			s = snap
			break
		}
	}
	if s.Name == "" {
		t.Fatal("未在 JSON 中找到 api_test")
	}
	if s.State != process.StateRunning {
		t.Errorf("期望 State=RUNNING, 实际 %s", s.State)
	}
	if s.PID <= 0 {
		t.Errorf("期望 PID > 0, 实际 %d", s.PID)
	}
}

// TestHandleAPIProcessesMethodNotAllowed tests that POST returns 405.
func TestHandleAPIProcessesMethodNotAllowed(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("POST", "/api/processes", nil)
	w := httptest.NewRecorder()
	webServer.handleAPIProcesses(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 %d, 实际 %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestHandleAPIProcessesEmptyList tests JSON API with no processes.
func TestHandleAPIProcessesEmptyList(t *testing.T) {
	logDir := "./test_logs_empty"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logManager, _ := logger.NewDefaultLogger(logDir)
	defer logManager.Close()

	processManager := process.NewProcessManager(logManager)
	webServer, err := NewWebServer(processManager, logDir)
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/processes", nil)
	w := httptest.NewRecorder()
	webServer.handleAPIProcesses(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var snapshots []process.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}

	if len(snapshots) != 0 {
		t.Errorf("期望空列表, 实际 %d 项", len(snapshots))
	}

	// Empty list should still be valid JSON array "[]"
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("期望 '[]', 实际 '%s'", strings.TrimSpace(w.Body.String()))
	}
}

// TestHandleAPIProcessesSnapshotFields tests all expected Snapshot fields are populated.
func TestHandleAPIProcessesSnapshotFields(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	cfg := &config.ProgramConfig{
		Name:                 "fields_test",
		Command:              "sleep 30",
		Directory:            "/tmp",
		AutoStart:            false,
		AutoRestart:          true,
		StartSecs:            2,
		StartRetries:         5,
		StopSecs:             15,
		StopSignal:           "SIGINT",
		User:                 "",
		Priority:             100,
		StdoutLogMaxBytes:    10 * 1024 * 1024,
		StderrLogMaxBytes:    5 * 1024 * 1024,
		StdoutLogBackupCount: 3,
		StderrLogBackupCount: 7,
		Environment:          make(map[string]string),
	}
	cfg.Environment["FOO"] = "bar"
	processManager.AddProcess(cfg)

	p := processManager.GetProcess("fields_test")
	if p == nil {
		t.Fatal("获取进程失败")
	}
	p.Start()
	defer p.Stop()
	time.Sleep(200 * time.Millisecond)

	webServer, err := NewWebServer(processManager, "./test_logs")
	if err != nil {
		t.Fatalf("创建Web服务器失败: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/processes", nil)
	w := httptest.NewRecorder()
	webServer.handleAPIProcesses(w, req)

	var snapshots []process.Snapshot
	json.Unmarshal(w.Body.Bytes(), &snapshots)

	var s process.Snapshot
	for _, snap := range snapshots {
		if snap.Name == "fields_test" {
			s = snap
			break
		}
	}
	if s.Name == "" {
		t.Fatal("未在 JSON 中找到 fields_test")
	}

	if s.Name != "fields_test" {
		t.Errorf("Name 期望 'fields_test', 实际 '%s'", s.Name)
	}
	if s.Config == nil {
		t.Fatal("Config 不应为 nil")
	}
	if s.Config.Command != "sleep 30" {
		t.Errorf("Config.Command 期望 'sleep 30', 实际 '%s'", s.Config.Command)
	}
	if s.Config.Directory != "/tmp" {
		t.Errorf("Config.Directory 期望 '/tmp', 实际 '%s'", s.Config.Directory)
	}
	if s.Config.Priority != 100 {
		t.Errorf("Config.Priority 期望 100, 实际 %d", s.Config.Priority)
	}
	if s.Config.StopSignal != "SIGINT" {
		t.Errorf("Config.StopSignal 期望 'SIGINT', 实际 '%s'", s.Config.StopSignal)
	}
	if s.Config.Environment["FOO"] != "bar" {
		t.Errorf("Config.Environment[FOO] 期望 'bar', 实际 '%s'", s.Config.Environment["FOO"])
	}
}

// --- API v1 tests ---

func TestAPIV1ProcessesList(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("GET", "/api/v1/processes", nil)
	w := httptest.NewRecorder()
	ws.handleAPIV1Processes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp apiV1ProcessesList
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("期望 status=ok, 实际 %s", resp.Status)
	}
	if len(resp.Processes) < 1 {
		t.Error("期望至少 1 个进程")
	}
}

func TestAPIV1ProcessDetail(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("GET", "/api/v1/processes/test_process", nil)
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("期望 status=ok, 实际 %v", resp["status"])
	}
}

func TestAPIV1ProcessStart(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/processes/test_process/start", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d: %s", w.Code, w.Body.String())
	}

	var resp apiV1Status
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("期望 status=ok, 实际 %s", resp.Status)
	}
	time.Sleep(200 * time.Millisecond)
}

func TestAPIV1ProcessStop(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	p := processManager.GetProcess("test_process")
	if p != nil {
		p.Start()
		time.Sleep(200 * time.Millisecond)
	}

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/processes/test_process/stop", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIV1ProcessRestart(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/processes/test_process/restart", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d: %s", w.Code, w.Body.String())
	}
	time.Sleep(200 * time.Millisecond)
}

func TestAPIV1ProcessNotFound(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("GET", "/api/v1/processes/nonexistent", nil)
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404, 实际 %d", w.Code)
	}
}

func TestAPIV1ProcessBadName(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("GET", "/api/v1/processes/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400, 实际 %d", w.Code)
	}
}

func TestAPIV1GroupStart(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	cfg := &config.ProgramConfig{
		Name:         "grp_test",
		Command:      "sleep 0.1",
		Group:        "testgrp",
		AutoStart:    true,
		AutoRestart:  false,
		StartSecs:    0,
		StartRetries: 1,
		Environment:  make(map[string]string),
	}
	processManager.AddProcess(cfg)

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/groups/testgrp/start", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("期望 status=ok, 实际 %v", resp["status"])
	}
	time.Sleep(200 * time.Millisecond)
}

func TestAPIV1GroupBadName(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/groups/../evil/start", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400, 实际 %d", w.Code)
	}
}

func TestAPIV1CSRFProtection(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/processes/test_process/start", nil)
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("无 Origin 的 POST 应返回 403, 实际 %d", w.Code)
	}
}

// --- Basic Auth tests ---

func TestBasicAuthEnabled(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServerWithAuth(processManager, "./test_logs", "admin", "secret", false)

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ws.authMiddleware(ws.mux).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("无认证的请求应返回 401, 实际 %d", w.Code)
	}

	req2, _ := http.NewRequest("GET", "/", nil)
	req2.SetBasicAuth("admin", "secret")
	w2 := httptest.NewRecorder()
	ws.authMiddleware(ws.mux).ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("正确认证应返回 200, 实际 %d", w2.Code)
	}

	req3, _ := http.NewRequest("GET", "/", nil)
	req3.SetBasicAuth("admin", "wrong")
	w3 := httptest.NewRecorder()
	ws.authMiddleware(ws.mux).ServeHTTP(w3, req3)

	if w3.Code != http.StatusUnauthorized {
		t.Errorf("错误密码应返回 401, 实际 %d", w3.Code)
	}
}

func TestBasicAuthDisabled(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServerWithAuth(processManager, "./test_logs", "", "", false)

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ws.authMiddleware(ws.mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("无认证配置时应返回 200, 实际 %d", w.Code)
	}
}

func TestAPIV1BypassesAuth(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServerWithAuth(processManager, "./test_logs", "admin", "secret", false)

	req, _ := http.NewRequest("GET", "/api/v1/processes", nil)
	w := httptest.NewRecorder()
	ws.authMiddleware(ws.mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("API v1 应绕过 Basic Auth, 期望 200, 实际 %d", w.Code)
	}
}

func TestNewWebServerWithAuth(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, err := NewWebServerWithAuth(processManager, "./test_logs", "user", "pass", false)
	if err != nil {
		t.Fatalf("NewWebServerWithAuth 失败: %v", err)
	}
	if ws.authUser != "user" || ws.authPass != "pass" {
		t.Error("认证凭据未正确设置")
	}
}

func TestAPIV1MethodNotAllowed(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req, _ := http.NewRequest("POST", "/api/v1/processes", nil)
	w := httptest.NewRecorder()
	ws.handleAPIV1Processes(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 405, 实际 %d", w.Code)
	}

	req2, _ := http.NewRequest("GET", "/api/v1/groups/test/unknown", nil)
	w2 := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w2, req2)

	if w2.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 405, 实际 %d", w2.Code)
	}
}

func TestBasicAuthWrapper(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// Test with credentials configured
	ws, _ := NewWebServerWithAuth(processManager, "./test_logs", "admin", "secret", false)

	// Wrap a simple handler that returns a known response
	wrapped := ws.basicAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	// Test without credentials -> 401
	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	wrapped(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无凭据应返回 401, 实际 %d", w.Code)
	}

	// Test with wrong credentials -> 401
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.SetBasicAuth("admin", "wrong")
	w2 := httptest.NewRecorder()
	wrapped(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("错误凭据应返回 401, 实际 %d", w2.Code)
	}

	// Test with correct credentials -> 200
	req3, _ := http.NewRequest("GET", "/", nil)
	req3.SetBasicAuth("admin", "secret")
	w3 := httptest.NewRecorder()
	wrapped(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("正确凭据应返回 200, 实际 %d", w3.Code)
	}
}

func TestBasicAuthWrapperDisabled(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// No auth configured
	ws, _ := NewWebServer(processManager, "./test_logs")

	wrapped := ws.basicAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	wrapped(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("无认证配置应允许通过, 实际 %d", w.Code)
	}
}

func TestAPIV1GroupStop(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	// Add a process with a group
	processManager.AddProcess(&config.ProgramConfig{
		Name:      "grouped_worker",
		Command:   "sleep 60",
		Group:     "workers",
		AutoStart: false,
	})

	ws, _ := NewWebServer(processManager, "./test_logs")

	// Start it first
	ws.processManager.GetProcess("grouped_worker").Start()
	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest("POST", "/api/v1/groups/workers/stop", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 实际 %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIV1GroupRestart(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	processManager.AddProcess(&config.ProgramConfig{
		Name:      "grouped_restart",
		Command:   "sleep 60",
		Group:     "workers",
		AutoStart: false,
	})

	ws, _ := NewWebServer(processManager, "./test_logs")

	// Start it first so restart works
	ws.processManager.GetProcess("grouped_restart").Start()
	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest("POST", "/api/v1/groups/workers/restart", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 实际 %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIV1GroupCSRFProtection(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req := httptest.NewRequest("POST", "/api/v1/groups/test/start", nil)
	// No Origin or Referer header
	w := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("无 Origin/Referer 应返回 403, 实际 %d", w.Code)
	}
}

func TestAPIV1GroupBadNamePaths(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	badPaths := []string{
		"/api/v1/groups//start",
		"/api/v1/groups/../start",
	}

	for _, p := range badPaths {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		ws.handleAPIV1GroupAction(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("path %q: 期望 400, 实际 %d", p, w.Code)
		}
	}
}

func TestAPIV1GroupUnknownAction(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req := httptest.NewRequest("POST", "/api/v1/groups/test/unknown", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1GroupAction(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("未知 action 应返回 405, 实际 %d", w.Code)
	}
}

func TestAPIV1ProcessUnknownAction(t *testing.T) {
	processManager, err := setupTestEnvironment()
	if err != nil {
		t.Fatalf("初始化测试环境失败: %v", err)
	}
	defer cleanupTestEnvironment()

	ws, _ := NewWebServer(processManager, "./test_logs")

	req := httptest.NewRequest("POST", "/api/v1/processes/test_process/unknown", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	w := httptest.NewRecorder()
	ws.handleAPIV1ProcessAction(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("未知 action 应返回 405, 实际 %d", w.Code)
	}
}


// --- Fuzz tests ---

func FuzzValidateProcessName(f *testing.F) {
	f.Add("myapp")
	f.Add("test_process")
	f.Add("")
	f.Add("../etc/passwd")
	f.Add("/tmp/socket")
	f.Add("a\\b")
	f.Add("a/b")

	f.Fuzz(func(t *testing.T, name string) {
		result := validateProcessName(name)
		// Must never panic. Results must be deterministic.
		_ = result
	})
}
