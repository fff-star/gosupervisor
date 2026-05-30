package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"gosupervisor/internal/config"
)

func TestLogger(t *testing.T) {
	// 创建测试日志目录
	logDir := t.TempDir()
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

	// Verify log content contains the written messages
	for _, f := range logFiles {
		if strings.HasSuffix(f.Name(), ".log") || f.Name() == "system.log" {
			data, err := os.ReadFile(filepath.Join(logDir, f.Name()))
			if err != nil {
				t.Errorf("读取日志文件 %s 失败: %v", f.Name(), err)
				continue
			}
			content := string(data)
			if !strings.Contains(content, "测试信息日志") {
				t.Errorf("日志文件 %s 未包含测试信息日志", f.Name())
			}
			if !strings.Contains(content, "测试警告日志") {
				t.Errorf("日志文件 %s 未包含测试警告日志", f.Name())
			}
		}
	}
}

// TestProcessLogWriterWithLargeWrites 测试日志写入时文件大小跟踪是否同步更新
func TestProcessLogWriterWithLargeWrites(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	// 创建小的日志大小限制以便快速触发轮转
	logger, err := NewLogger(logDir, 1024, 5, false, LevelInfo, FormatText) // 1KB max size
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// 获取进程日志写入器
	cfg := &config.ProgramConfig{
		Name:                 "test_large",
		StdoutLogMaxBytes:    1024,
		StderrLogMaxBytes:    1024,
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
	logDir := t.TempDir()
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

	// Verify that rotation produced additional log files (original + rotated).
	if len(logFiles) < 2 {
		t.Errorf("日志轮转后文件数量不足，期望至少2个，实际有%d个", len(logFiles))
	}
	// Check for existence of a rotated file (with timestamp suffix).
	hasRotated := false
	for _, f := range logFiles {
		if strings.Contains(f.Name(), "system.log.") {
			hasRotated = true
			break
		}
	}
	if !hasRotated {
		t.Error("未找到轮转后的日志文件 (system.log.xxx)")
	}
}

func TestLogCompression(t *testing.T) {
	// 创建测试日志目录
	logDir := t.TempDir()
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
	logDir := t.TempDir()

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// Create backup files exceeding the default maxBackupCount (10)
	basePath := filepath.Join(logDir, "test.log")
	for i := 0; i < 15; i++ {
		backup := fmt.Sprintf("%s.%d", basePath, i)
		if err := os.WriteFile(backup, []byte("backup"), 0644); err != nil {
			t.Fatalf("创建备份文件失败: %v", err)
		}
	}

	if err := logger.cleanupOldLogs("test"); err != nil {
		t.Errorf("清理旧日志失败: %v", err)
	}

	// Verify old backups were trimmed to maxBackupCount
	files, err := filepath.Glob(basePath + ".*")
	if err != nil {
		t.Fatalf("glob 失败: %v", err)
	}
	if len(files) > logger.maxBackupCount {
		t.Errorf("备份文件未正确清理: got %d, want <= %d", len(files), logger.maxBackupCount)
	}
}

// TestLogRotationWithMultipleRotations 测试多次日志轮转
func TestLogRotationWithMultipleRotations(t *testing.T) {
	// 创建测试日志目录
	logDir := t.TempDir()
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
	logDir := t.TempDir()
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
	logDir := t.TempDir()

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// Create backup files matching the cleanup pattern (basePath.*)
	basePath := filepath.Join(logDir, "test.log")
	for i := 0; i < 15; i++ {
		backup := fmt.Sprintf("%s.%d", basePath, i)
		if err := os.WriteFile(backup, []byte("backup content"), 0644); err != nil {
			t.Fatalf("创建测试日志文件失败: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cleanup should reduce files to maxBackupCount (10)
	if err := logger.cleanupOldLogs("test"); err != nil {
		t.Errorf("清理旧日志失败: %v", err)
	}

	files, err := filepath.Glob(basePath + ".*")
	if err != nil {
		t.Fatalf("glob 失败: %v", err)
	}
	if len(files) > logger.maxBackupCount {
		t.Errorf("备份文件未正确清理: got %d, want <= %d", len(files), logger.maxBackupCount)
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

// TestLogLevelsWriteToSystemLog tests that all log levels write to system.log.
func TestLogLevelsWriteToSystemLog(t *testing.T) {
	// 创建测试日志目录
	logDir := t.TempDir()
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
	logDir := t.TempDir()
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
	logDir := t.TempDir()
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
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	// Both stdout and stderr default to {logDir}/{name}.log — same path
	cfg := &config.ProgramConfig{
		Name:                 "same_test",
		StdoutLogMaxBytes:    1024,
		StderrLogMaxBytes:    2048,
		StdoutLogBackupCount: 3,
		StderrLogBackupCount: 5,
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
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, false, LevelInfo, FormatText)
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
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 1024, 5, false, LevelInfo, FormatText)
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
	logDir := t.TempDir()
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
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, true, LevelInfo, FormatText)
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

// TestCleanupBackupsKeepsNewest verifies cleanupBackups removes old files
// keeping only the most recent backupCount files.
func TestCleanupBackupsKeepsNewest(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	baseFile := filepath.Join(logDir, "test.log")
	old := time.Now().Add(-3 * time.Hour)
	mid := time.Now().Add(-2 * time.Hour)

	os.WriteFile(baseFile+".100", []byte("old"), 0644)
	_ = os.Chtimes(baseFile+".100", old, old)
	os.WriteFile(baseFile+".200", []byte("mid"), 0644)
	_ = os.Chtimes(baseFile+".200", mid, mid)
	os.WriteFile(baseFile+".300", []byte("new"), 0644)

	logger.cleanupBackups(baseFile, 2)

	files, _ := filepath.Glob(baseFile + ".*")
	if len(files) != 2 {
		t.Errorf("expected 2 files after cleanup, got %d: %v", len(files), files)
	}
}

// TestCleanupBackupsNoOpWhenUnderLimit verifies cleanupBackups is a no-op
// when the number of backup files does not exceed backupCount.
func TestCleanupBackupsNoOpWhenUnderLimit(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, _ := NewDefaultLogger(logDir)
	defer logger.Close()

	baseFile := filepath.Join(logDir, "keep.log")
	os.WriteFile(baseFile+".1", []byte("a"), 0644)
	os.WriteFile(baseFile+".2", []byte("b"), 0644)

	logger.cleanupBackups(baseFile, 5)

	files, _ := filepath.Glob(baseFile + ".*")
	if len(files) != 2 {
		t.Errorf("expected 2 files (under limit), got %d", len(files))
	}
}

// TestRotateFileOutsideLock validates that rotateFileOutsideLock renames a
// file, compresses it if enabled, and cleans up old backups.
func TestRotateFileOutsideLock(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 10*1024*1024, 5, true, LevelInfo, FormatText)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	logFile := filepath.Join(logDir, "rotate_test.log")
	os.WriteFile(logFile, []byte("test content for rotation\n"), 0644)

	err = logger.rotateFileOutsideLock(logFile, 2)
	if err != nil {
		t.Fatalf("rotateFileOutsideLock failed: %v", err)
	}

	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("expected original file to be renamed (gone)")
	}

	gzFiles, _ := filepath.Glob(logFile + ".*.gz")
	if len(gzFiles) == 0 {
		t.Error("expected compressed .gz backup file after rotation")
	}
}

// TestRotateFileOutsideLockNoCompress validates rotation without compression.
func TestRotateFileOutsideLockNoCompress(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 10*1024*1024, 5, false, LevelInfo, FormatText)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	logFile := filepath.Join(logDir, "rotate_nc.log")
	os.WriteFile(logFile, []byte("no compression test\n"), 0644)

	err = logger.rotateFileOutsideLock(logFile, 2)
	if err != nil {
		t.Fatalf("rotateFileOutsideLock failed: %v", err)
	}

	backups, _ := filepath.Glob(logFile + ".*")
	nonGz := 0
	for _, b := range backups {
		if !strings.HasSuffix(b, ".gz") {
			nonGz++
		}
	}
	if nonGz == 0 {
		t.Error("expected non-compressed backup file")
	}
}

// TestRotateFileOutsideLockFileNotExist validates behavior when the file
// does not exist (should return nil, no error).
func TestRotateFileOutsideLockFileNotExist(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, _ := NewDefaultLogger(logDir)
	defer logger.Close()

	nonexistent := filepath.Join(logDir, "does_not_exist.log")
	err := logger.rotateFileOutsideLock(nonexistent, 2)
	if err != nil {
		t.Errorf("expected nil error for non-existent file, got: %v", err)
	}
}

// TestCloseProcessLog verifies CloseProcessLog removes the log stream.
func TestCloseProcessLog(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, _ := NewDefaultLogger(logDir)
	defer logger.Close()

	logFile := filepath.Join(logDir, "close_test.log")
	key := "close_test/stdout"
	_, err := logger.getOrCreateWriter(key, logFile, 10*1024*1024, 5)
	if err != nil {
		t.Fatalf("getOrCreateWriter failed: %v", err)
	}

	_ = logger.CloseProcessLog("close_test")

	if _, exists := logger.processLogs[key]; exists {
		t.Error("expected log stream to be removed after CloseProcessLog")
	}
}

// TestLogSystemRotation verifies LogSystem triggers rotation when
// system.log exceeds max size.
func TestLogSystemRotation(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)
	defer os.RemoveAll(logDir)

	logger, err := NewLogger(logDir, 10*1024*1024, 5, false, LevelInfo, FormatText)
	if err != nil {
		t.Fatalf("初始化日志管理器失败: %v", err)
	}
	defer logger.Close()

	logger.systemLogMaxBytes = 10
	logger.systemLogBackupCount = 2

	systemLog := filepath.Join(logDir, "system.log")
	logger.LogSystem("first message")
	logger.LogSystem("second message long enough to push over limit")

	backups, _ := filepath.Glob(systemLog + ".*")
	if len(backups) == 0 {
		t.Error("expected backup file after system log rotation")
	}
}

// readSystemLog reads the system.log file from the given directory.
func readSystemLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "system.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read system.log: %v", err)
	}
	return string(data)
}

// TestParseLevel_AllValues tests ParseLevel with all valid and edge-case inputs.
func TestParseLevel_AllValues(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"unknown", LevelInfo},
		{"", LevelInfo},
	}
	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// TestSetLevel_FiltersOutput verifies that setting the log level to WARN
// filters out DEBUG and INFO messages from system.log.
func TestSetLevel_FiltersOutput(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, false, LevelWarn, FormatText)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Debug("debug message should be filtered")
	logger.Info("info message should be filtered")
	logger.Warning("warning message should appear")
	logger.Error("error message should appear")

	content := readSystemLog(t, logDir)
	if strings.Contains(content, "debug message should be filtered") {
		t.Error("DEBUG message was written to system.log despite WARN level")
	}
	if strings.Contains(content, "info message should be filtered") {
		t.Error("INFO message was written to system.log despite WARN level")
	}
	if !strings.Contains(content, "warning message should appear") {
		t.Error("WARNING message was NOT written to system.log at WARN level")
	}
	if !strings.Contains(content, "error message should appear") {
		t.Error("ERROR message was NOT written to system.log at WARN level")
	}
}

// TestSetFormat_JSON verifies that JSON formatted logs contain the expected fields.
func TestSetFormat_JSON(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, false, LevelInfo, FormatJSON)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Info("test json message")

	content := readSystemLog(t, logDir)
	if !strings.Contains(content, `"level":"INFO"`) {
		t.Errorf("system.log does not contain expected level field: %s", content)
	}
	if !strings.Contains(content, `"message":"test json message"`) {
		t.Errorf("system.log does not contain expected message field: %s", content)
	}
}

// TestDebug_OutputsWhenEnabled verifies that Debug messages appear in
// system.log when the log level is set to DEBUG.
func TestDebug_OutputsWhenEnabled(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logger, err := NewLogger(logDir, 50*1024*1024, 10, false, LevelDebug, FormatText)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Debug("debug message at debug level")

	content := readSystemLog(t, logDir)
	if !strings.Contains(content, "debug message at debug level") {
		t.Errorf("DEBUG message not found in system.log at DEBUG level, content: %s", content)
	}
}

// TestSetLevel verifies that SetLevel changes the minimum log level and
// that the new level actually filters subsequent log output.
func TestSetLevel(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("NewDefaultLogger failed: %v", err)
	}
	defer logger.Close()

	// Default is INFO — Debug should be filtered
	logger.Debug("before setlevel")
	content := readSystemLog(t, logDir)
	if strings.Contains(content, "before setlevel") {
		t.Error("Debug message should be filtered at default INFO level")
	}

	logger.SetLevel(LevelDebug)
	logger.Debug("after setlevel")

	content = readSystemLog(t, logDir)
	if !strings.Contains(content, "after setlevel") {
		t.Error("Debug message should appear after SetLevel(LevelDebug)")
	}
}

// TestSetFormat verifies that SetFormat changes the system log output format
// and subsequent log messages use the new format.
func TestSetFormat(t *testing.T) {
	logDir := t.TempDir()
	os.MkdirAll(logDir, 0755)

	logger, err := NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("NewDefaultLogger failed: %v", err)
	}
	defer logger.Close()

	// Default is text format
	logger.Info("text format message")
	content := readSystemLog(t, logDir)
	if !strings.Contains(content, "[INFO]") {
		t.Error("text format should contain [INFO] prefix")
	}

	// Switch to JSON format and verify output format changes
	logger.SetFormat(FormatJSON)
	logger.Info("json format message")

	content = readSystemLog(t, logDir)
	if !strings.Contains(content, `"level":"INFO"`) {
		t.Error("JSON format should contain level field")
	}
	if !strings.Contains(content, `"message":"json format message"`) {
		t.Error("JSON format should contain message field")
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
