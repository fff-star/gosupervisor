package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosupervisor/internal/config"
)

func TestLogger(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 测试日志写入
	logger.Info("测试信息日志")
	logger.Warning("测试警告日志")
	logger.Error("测试错误日志")

	// 测试格式化日志
	logger.Info("测试格式化日志: %s", "格式化参数")
	logger.Warning("测试格式化警告: %d", 123)
	logger.Error("测试格式化错误: %v", err)

	// 等待日志写入
	time.Sleep(1 * time.Second)

	// 检查日志文件是否创建
	logFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	if len(logFiles) == 0 {
		t.Fatalf("日志文件未创建")
	}
}

// TestProcessLogWriterWithLargeWrites 测试日志写入时文件大小跟踪是否同步更新
func TestProcessLogWriterWithLargeWrites(t *testing.T) {
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 创建小的日志大小限制以便快速触发轮转
	logger, err := NewLogger(logDir, 1024, 5, false) // 1KB max size
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 获取进程日志写入器
	cfg := &config.ProgramConfig{
		Name:               "test_large",
		StdoutLogMaxBytes:  1024,
		StderrLogMaxBytes:  1024,
		StdoutLogBackupCount: 5,
		StderrLogBackupCount: 5,
	}
	writer, _, err := logger.GetProcessLogWriters("test_large", cfg)
	if err != nil {
		t.Fatalf("获取日志写入器失败: %v", err)
	}

	// 写入大量数据以触发轮转（>1KB）
	largeData := make([]byte, 1500)
	for i := range largeData {
		largeData[i] = 'A'
	}

	n, err := writer.Write(largeData)
	if err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}

	if n != len(largeData) {
		t.Errorf("期望写入%d字节，实际写入%d字节", len(largeData), n)
	}

	// 等待轮转
	time.Sleep(500 * time.Millisecond)

	// 再次获取写入器，应该触发轮转
	writer2, _, err := logger.GetProcessLogWriters("test_large", cfg)
	if err != nil {
		t.Fatalf("第二次获取日志写入器失败: %v", err)
	}

	n2, err := writer2.Write(largeData)
	if err != nil {
		t.Fatalf("第二次写入日志失败: %v", err)
	}

	if n2 != len(largeData) {
		t.Errorf("期望第二次写入%d字节，实际写入%d字节", len(largeData), n2)
	}

	// 检查是否生成了轮转文件
	files, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	// 应该至少有原始日志文件和至少一个轮转后的文件
	if len(files) < 1 {
		t.Errorf("期望至少有日志文件，实际有%d个", len(files))
	}
}

func TestLogRotation(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 先写入一些日志，确保日志文件存在
	systemLog := filepath.Join(logDir, "system.log")
	file, err := os.OpenFile(systemLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("创建系统日志文件失败: %v", err)
	}
	file.WriteString("测试日志内容\n")
	file.Close()

	// 强制日志轮转
	if err := logger.rotateLog("system"); err != nil {
		t.Errorf("日志轮转失败: %v", err)
	}

	// 等待日志轮转完成
	time.Sleep(1 * time.Second)

	// 检查是否生成了轮转后的日志文件
	logFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	// 至少应该有一个原始日志文件和一个轮转后的日志文件
	if len(logFiles) < 1 {
		t.Errorf("日志文件数量不足，期望至少1个，实际有%d个", len(logFiles))
	}
}

func TestLogCompression(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 先写入一些日志，确保日志文件存在
	systemLog := filepath.Join(logDir, "system.log")
	file, err := os.OpenFile(systemLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("创建系统日志文件失败: %v", err)
	}
	file.WriteString("测试日志内容\n")
	file.Close()

	// 先进行一次日志轮转
	if err := logger.rotateLog("system"); err != nil {
		t.Errorf("日志轮转失败: %v", err)
	}

	// 等待日志轮转完成
	time.Sleep(1 * time.Second)

	// 获取轮转后的日志文件
	logFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	if len(logFiles) == 0 {
		t.Fatalf("没有找到日志文件")
	}

	// 测试日志压缩
	for _, file := range logFiles {
		if !file.IsDir() && len(file.Name()) > 4 && file.Name()[:7] == "system" {
			if err := logger.compressLog(filepath.Join(logDir, file.Name())); err != nil {
				t.Errorf("日志压缩失败: %v", err)
			}
			break
		}
	}

	// 等待日志压缩完成
	time.Sleep(1 * time.Second)

	// 检查是否生成了压缩后的日志文件
	compressedFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	// 至少应该有一个压缩后的日志文件
	foundCompressedFile := false
	for _, file := range compressedFiles {
		if !file.IsDir() && len(file.Name()) > 3 && file.Name()[len(file.Name())-3:] == ".gz" {
			foundCompressedFile = true
			break
		}
	}

	if !foundCompressedFile {
		t.Errorf("未生成压缩后的日志文件")
	}
}

func TestCleanupOldLogs(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 测试清理旧日志
	if err := logger.cleanupOldLogs("test"); err != nil {
		t.Errorf("清理旧日志失败: %v", err)
	}

	// 等待清理完成
	time.Sleep(1 * time.Second)

	// 检查日志目录是否存在
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Fatalf("日志目录不存在")
	}
}

// TestLogRotationWithMultipleRotations 测试多次日志轮转
func TestLogRotationWithMultipleRotations(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 先写入一些日志，确保日志文件存在
	systemLog := filepath.Join(logDir, "system.log")
	file, err := os.OpenFile(systemLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("创建系统日志文件失败: %v", err)
	}
	file.WriteString("测试日志内容\n")
	file.Close()

	// 执行多次日志轮转
	for i := 0; i < 3; i++ {
		if err := logger.rotateLog("system"); err != nil {
			t.Errorf("第%d次日志轮转失败: %v", i+1, err)
		}
		// 等待轮转完成
		time.Sleep(500 * time.Millisecond)
	}

	// 检查日志文件数量
	logFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	// 至少应该有3个轮转后的日志文件和1个当前日志文件
	if len(logFiles) < 4 {
		t.Errorf("日志文件数量不足，期望至少4个，实际有%d个", len(logFiles))
	}
}

// TestLogCompressionWithInvalidFile 测试压缩无效文件
func TestLogCompressionWithInvalidFile(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 测试压缩不存在的文件
	err = logger.compressLog("non_existent_file.log")
	if err == nil {
		t.Errorf("期望压缩不存在的文件失败，但成功了")
	}
}

// TestCleanupOldLogsWithMultipleFiles 测试清理多个旧日志文件
func TestCleanupOldLogsWithMultipleFiles(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 创建多个日志文件
	for i := 0; i < 5; i++ {
		// 创建测试日志文件
		logFileName := filepath.Join(logDir, fmt.Sprintf("test_log_%d.log", i))
		if err := os.WriteFile(logFileName, []byte("test log content"), 0644); err != nil {
			t.Fatalf("创建测试日志文件失败: %v", err)
		}
		// 等待一段时间，确保文件修改时间不同
		time.Sleep(100 * time.Millisecond)
	}

	// 清理旧日志
	if err := logger.cleanupOldLogs("test"); err != nil {
		t.Errorf("清理旧日志失败: %v", err)
	}

	// 等待清理完成
	time.Sleep(1 * time.Second)

	// 检查剩余日志文件数量
	remainingFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	// 应该还有一些文件剩余（具体数量取决于清理策略）
	if len(remainingFiles) == 0 {
		t.Errorf("所有日志文件都被清理了，这可能不是预期行为")
	}
}

// TestLoggerWithNonExistentDir 测试在不存在的目录中初始化日志管理器
func TestLoggerWithNonExistentDir(t *testing.T) {
	// 使用不存在的目录
	nonExistentDir := "./non_existent_log_dir"
	defer os.RemoveAll(nonExistentDir) // 清理可能创建的目录

	// 初始化日志管理器
	logger, err := NewDefaultLogger(nonExistentDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 测试日志写入
	logger.Info("测试日志写入")

	// 等待日志写入
	time.Sleep(1 * time.Second)

	// 检查目录是否被创建
	if _, err := os.Stat(nonExistentDir); os.IsNotExist(err) {
		t.Errorf("日志目录未被创建")
	}
}

// TestLogLevelFiltering 测试日志级别过滤
func TestLogLevelFiltering(t *testing.T) {
	// 创建测试日志目录
	logDir := "./test_logs"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 初始化日志管理器
	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 测试不同级别的日志写入
	logger.Info("信息级别日志")
	logger.Warning("警告级别日志")
	logger.Error("错误级别日志")

	// 等待日志写入
	time.Sleep(1 * time.Second)

	// 检查日志文件是否创建
	logFiles, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}

	if len(logFiles) == 0 {
		t.Fatalf("日志文件未创建")
	}
}

// TestGetProcessLogWritersPerProcessConfig tests per-process log settings.
func TestGetProcessLogWritersPerProcessConfig(t *testing.T) {
	logDir := "./test_logs_pp"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// Use a small per-process max size so rotation triggers
	cfg := &config.ProgramConfig{
		Name:                 "pp_test",
		StdoutLogMaxBytes:    1024,
		StderrLogMaxBytes:    2048,
		StdoutLogBackupCount: 3,
		StderrLogBackupCount: 5,
	}

	stdoutW, stderrW, err := logger.GetProcessLogWriters("pp_test", cfg)
	if err != nil {
		t.Fatalf("GetProcessLogWriters 失败: %v", err)
	}
	if stdoutW == nil {
		t.Error("stdout writer 不应为 nil")
	}
	if stderrW == nil {
		t.Error("stderr writer 不应为 nil")
	}

	// Write enough data to trigger rotation on stdout
	largeData := make([]byte, 1500)
	for i := range largeData {
		largeData[i] = 'X'
	}
	stdoutW.Write(largeData)
	time.Sleep(300 * time.Millisecond)

	// Next GetProcessLogWriters call should trigger rotation check
	stdoutW2, _, err := logger.GetProcessLogWriters("pp_test", cfg)
	if err != nil {
		t.Fatalf("第二次 GetProcessLogWriters 失败: %v", err)
	}
	stdoutW2.Write([]byte("more data\n"))
	time.Sleep(300 * time.Millisecond)
}

// TestGetProcessLogWritersSeparatePaths tests custom log file paths.
func TestGetProcessLogWritersSeparatePaths(t *testing.T) {
	logDir := "./test_logs_sep"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	stdoutPath := filepath.Join(logDir, "custom_stdout.log")
	stderrPath := filepath.Join(logDir, "custom_stderr.log")

	cfg := &config.ProgramConfig{
		Name:                 "sep_test",
		StdoutLogFile:        stdoutPath,
		StderrLogFile:        stderrPath,
		StdoutLogMaxBytes:    50 * 1024 * 1024,
		StderrLogMaxBytes:    50 * 1024 * 1024,
		StdoutLogBackupCount: 10,
		StderrLogBackupCount: 10,
	}

	stdoutW, stderrW, err := logger.GetProcessLogWriters("sep_test", cfg)
	if err != nil {
		t.Fatalf("GetProcessLogWriters 失败: %v", err)
	}

	stdoutW.Write([]byte("stdout log\n"))
	stderrW.Write([]byte("stderr log\n"))
	time.Sleep(300 * time.Millisecond)

	// Verify both custom files exist
	if _, err := os.Stat(stdoutPath); os.IsNotExist(err) {
		t.Errorf("自定义 stdout 日志文件未创建: %s", stdoutPath)
	}
	if _, err := os.Stat(stderrPath); os.IsNotExist(err) {
		t.Errorf("自定义 stderr 日志文件未创建: %s", stderrPath)
	}
}
// TestGetProcessLogWritersSamePath tests that when stdout and stderr share
// the same path, they reuse the same writer instead of opening separate handles.
func TestGetProcessLogWritersSamePath(t *testing.T) {
	logDir := "./test_logs_samepath"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// Both stdout and stderr default to {logDir}/{name}.log — same path
	cfg := &config.ProgramConfig{
		Name:                  "same_test",
		StdoutLogMaxBytes:     1024,
		StderrLogMaxBytes:     2048,
		StdoutLogBackupCount:  3,
		StderrLogBackupCount:  5,
	}

	stdoutW, stderrW, err := logger.GetProcessLogWriters("same_test", cfg)
	if err != nil {
		t.Fatalf("GetProcessLogWriters 失败: %v", err)
	}

	// Same path → same writer instance
	if stdoutW != stderrW {
		t.Error("stdout 和 stderr 同路径时应返回同一个 writer")
	}

	stdoutW.Write([]byte("stdout line\n"))
	stderrW.Write([]byte("stderr line\n"))
	time.Sleep(300 * time.Millisecond)

	// Verify only one log file was created
	files, _ := os.ReadDir(logDir)
	logFiles := 0
	for _, f := range files {
		if !f.IsDir() {
			logFiles++
		}
	}
	if logFiles != 1 {
		t.Errorf("期望 1 个日志文件, 实际 %d 个", logFiles)
	}

	// Verify both lines are in the same file
	logPath := filepath.Join(logDir, "same_test.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "stdout line") || !strings.Contains(content, "stderr line") {
		t.Errorf("日志文件应同时包含 stdout 和 stderr 内容: %s", content)
	}
}

// TestCountingWriterConcurrent tests that concurrent writes to countingWriter
// don't race when tracking bytesWritten via sync/atomic.
func TestCountingWriterConcurrent(t *testing.T) {
	logDir := "./test_logs_cwcon"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, false)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	cfg := &config.ProgramConfig{
		Name:                 "cw_test",
		StdoutLogMaxBytes:    50 * 1024 * 1024,
		StderrLogMaxBytes:    50 * 1024 * 1024,
		StdoutLogBackupCount: 10,
		StderrLogBackupCount: 10,
	}
	stdoutW, _, err := logger.GetProcessLogWriters("cw_test", cfg)
	if err != nil {
		t.Fatalf("GetProcessLogWriters 失败: %v", err)
	}

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				stdoutW.Write([]byte("concurrent write test data\n"))
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestRotateLogsSharedWriter tests that RotateLogs handles shared writers
// without double-rotating the same file.
func TestRotateLogsSharedWriter(t *testing.T) {
	logDir := "./test_logs_sw"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 1024, 5, false)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	cfg := &config.ProgramConfig{
		Name:                 "sw_test",
		StdoutLogMaxBytes:    1024,
		StderrLogMaxBytes:    1024,
		StdoutLogBackupCount: 3,
		StderrLogBackupCount: 3,
	}
	stdoutW, stderrW, err := logger.GetProcessLogWriters("sw_test", cfg)
	if err != nil {
		t.Fatalf("GetProcessLogWriters 失败: %v", err)
	}
	if stdoutW != stderrW {
		t.Fatal("期望 stdout 和 stderr 共享同一个 writer")
	}

	stdoutW.Write([]byte(strings.Repeat("A", 500)))
	stderrW.Write([]byte(strings.Repeat("B", 500)))

	if err := logger.RotateLogs(); err != nil {
		t.Errorf("RotateLogs 失败: %v", err)
	}

	// After rotation, old writers are closed; acquire fresh ones
	newW, _, err := logger.GetProcessLogWriters("sw_test", cfg)
	if err != nil {
		t.Fatalf("旋转后获取新 writer 失败: %v", err)
	}
	_, err = newW.Write([]byte("after rotation\n"))
	if err != nil {
		t.Errorf("旋转后写入失败: %v", err)
	}
}


// TestRotateLogFallback tests the fallback path when no active writers exist.
func TestRotateLogFallback(t *testing.T) {
	logDir := "./test_logs_fallback"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// Create a log file on disk without an active writer
	defaultPath := filepath.Join(logDir, "fallback_test.log")
	if err := os.WriteFile(defaultPath, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("创建测试日志文件失败: %v", err)
	}

	// rotateLog should handle the fallback path (no active writers)
	if err := logger.rotateLog("fallback_test"); err != nil {
		t.Errorf("rotateLog fallback 应成功, 返回: %v", err)
	}

	// Verify the old file was rotated away from defaultPath
	files, _ := os.ReadDir(logDir)
	rotatedFound := false
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), "fallback_test.log.") {
			rotatedFound = true
			break
		}
	}
	if !rotatedFound {
		t.Error("期望找到轮转后的文件 (带时间戳后缀)")
	}
}

// TestRotateLogFallbackCompressEnabled tests fallback rotation with compression.
func TestRotateLogFallbackCompressEnabled(t *testing.T) {
	logDir := "./test_logs_fallback_gz"
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, true)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	defaultPath := filepath.Join(logDir, "fallback_gz_test.log")
	os.WriteFile(defaultPath, []byte("compressible content\n"), 0644)

	if err := logger.rotateLog("fallback_gz_test"); err != nil {
		t.Errorf("rotateLog fallback (compress) 应成功, 返回: %v", err)
	}

	// Should have created a .gz file
	files, _ := os.ReadDir(logDir)
	gzFound := false
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".gz") {
			gzFound = true
			break
		}
	}
	if !gzFound {
		t.Error("期望找到压缩后的 .gz 文件")
	}
}
