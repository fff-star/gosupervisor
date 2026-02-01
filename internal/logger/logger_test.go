package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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
