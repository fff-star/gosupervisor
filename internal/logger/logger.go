package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	logDir             string
	processLogs        map[string]*os.File
	maxLogSize         int64
	maxBackupCount     int
	compress           bool
	mutex              sync.Mutex
	logFileSizes       map[string]int64
}

func NewLogger(logDir string, maxLogSize int64, maxBackupCount int, compress bool) (*Logger, error) {
	// 确保日志目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	// 设置默认值
	if maxLogSize <= 0 {
		maxLogSize = 50 * 1024 * 1024 // 默认50MB
	}

	if maxBackupCount <= 0 {
		maxBackupCount = 10 // 默认保留10个备份
	}

	return &Logger{
		logDir:             logDir,
		processLogs:        make(map[string]*os.File),
		maxLogSize:         maxLogSize,
		maxBackupCount:     maxBackupCount,
		compress:           compress,
		logFileSizes:       make(map[string]int64),
	}, nil
}

// NewDefaultLogger 创建默认配置的日志管理器
func NewDefaultLogger(logDir string) (*Logger, error) {
	return NewLogger(logDir, 50*1024*1024, 10, true)
}

func (l *Logger) GetProcessLogWriter(processName string) (io.Writer, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	// 检查是否已经有打开的日志文件
	if file, exists := l.processLogs[processName]; exists {
		// 检查日志文件大小
		if l.logFileSizes[processName] >= l.maxLogSize {
			// 进行日志轮转
			if err := l.rotateLog(processName); err != nil {
				return nil, fmt.Errorf("日志轮转失败: %v", err)
			}
			return l.processLogs[processName], nil
		}
		return file, nil
	}

	// 检查日志文件大小
	logFile := filepath.Join(l.logDir, fmt.Sprintf("%s.log", processName))
	fileInfo, err := os.Stat(logFile)
	if err == nil {
		// 文件存在，检查大小
		if fileInfo.Size() >= l.maxLogSize {
			// 进行日志轮转
			if err := l.rotateLog(processName); err != nil {
				return nil, fmt.Errorf("日志轮转失败: %v", err)
			}
		}
	}

	// 创建新的日志文件
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %v", err)
	}

	l.processLogs[processName] = file
	l.logFileSizes[processName] = 0
	return file, nil
}

// rotateLog 执行单个日志文件的轮转
func (l *Logger) rotateLog(processName string) error {
	// 关闭现有的日志文件
	if file, exists := l.processLogs[processName]; exists {
		if err := file.Close(); err != nil {
			return fmt.Errorf("关闭日志文件失败: %v", err)
		}
		delete(l.processLogs, processName)
	}

	// 重命名旧日志文件
	oldLog := filepath.Join(l.logDir, fmt.Sprintf("%s.log", processName))
	newLog := filepath.Join(l.logDir, fmt.Sprintf("%s.log.%s", processName, time.Now().Format("20060102150405")))
	if err := os.Rename(oldLog, newLog); err != nil {
		return fmt.Errorf("重命名日志文件失败: %v", err)
	}

	// 如果启用了压缩，压缩日志文件
	if l.compress {
		if err := l.compressLog(newLog); err != nil {
			fmt.Printf("压缩日志文件失败: %v\n", err)
			// 压缩失败不影响主流程
		}
	}

	// 清理旧的日志文件，只保留指定数量的备份
	if err := l.cleanupOldLogs(processName); err != nil {
		fmt.Printf("清理旧日志文件失败: %v\n", err)
		// 清理失败不影响主流程
	}

	// 创建新的日志文件
	newFile, err := os.OpenFile(oldLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("创建新日志文件失败: %v", err)
	}
	l.processLogs[processName] = newFile
	l.logFileSizes[processName] = 0

	return nil
}

// compressLog 压缩日志文件
func (l *Logger) compressLog(logFile string) error {
	// 打开源文件
	srcFile, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %v", err)
	}
	defer srcFile.Close()

	// 创建压缩文件
	gzFile := logFile + ".gz"
	dstFile, err := os.Create(gzFile)
	if err != nil {
		return fmt.Errorf("创建压缩文件失败: %v", err)
	}
	defer dstFile.Close()

	// 创建gzip写入器
	gzWriter := gzip.NewWriter(dstFile)
	defer gzWriter.Close()

	// 复制内容
	if _, err := io.Copy(gzWriter, srcFile); err != nil {
		return fmt.Errorf("压缩日志文件失败: %v", err)
	}

	// 关闭gzip写入器
	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("关闭gzip写入器失败: %v", err)
	}

	// 删除源文件
	if err := os.Remove(logFile); err != nil {
		return fmt.Errorf("删除源日志文件失败: %v", err)
	}

	return nil
}

// cleanupOldLogs 清理旧的日志文件，只保留指定数量的备份
func (l *Logger) cleanupOldLogs(processName string) error {
	// 获取所有日志文件
	files, err := filepath.Glob(filepath.Join(l.logDir, fmt.Sprintf("%s.log.*", processName)))
	if err != nil {
		return fmt.Errorf("查找日志文件失败: %v", err)
	}

	// 如果文件数量小于等于保留数量，不需要清理
	if len(files) <= l.maxBackupCount {
		return nil
	}

	// 按修改时间排序
	fileInfos := make([]os.FileInfo, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, info)
	}

	// 按修改时间排序（从旧到新）
	for i := 0; i < len(fileInfos); i++ {
		for j := i + 1; j < len(fileInfos); j++ {
			if fileInfos[i].ModTime().After(fileInfos[j].ModTime()) {
				fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
			}
		}
	}

	// 删除多余的文件
	for i := 0; i < len(fileInfos)-l.maxBackupCount; i++ {
		file := filepath.Join(l.logDir, fileInfos[i].Name())
		if err := os.Remove(file); err != nil {
			fmt.Printf("删除旧日志文件失败: %v\n", err)
			// 删除失败不影响主流程
		}
	}

	return nil
}

func (l *Logger) CloseProcessLog(processName string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if file, exists := l.processLogs[processName]; exists {
		if err := file.Close(); err != nil {
			return fmt.Errorf("关闭日志文件失败: %v", err)
		}
		delete(l.processLogs, processName)
	}
	return nil
}

func (l *Logger) LogSystem(message string) {
	systemLog := filepath.Join(l.logDir, "system.log")
	file, err := os.OpenFile(systemLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("打开系统日志文件失败: %v\n", err)
		return
	}
	defer file.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(file, "[%s] %s\n", timestamp, message)
}

func (l *Logger) Close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for name, file := range l.processLogs {
		if err := file.Close(); err != nil {
			return fmt.Errorf("关闭进程 %s 的日志文件失败: %v", name, err)
		}
	}
	l.processLogs = make(map[string]*os.File)
	return nil
}

// RotateLogs 日志轮转
func (l *Logger) RotateLogs() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for name, file := range l.processLogs {
		if err := file.Close(); err != nil {
			return fmt.Errorf("关闭进程 %s 的日志文件失败: %v", name, err)
		}

		// 重命名旧日志文件
		oldLog := filepath.Join(l.logDir, fmt.Sprintf("%s.log", name))
		newLog := filepath.Join(l.logDir, fmt.Sprintf("%s.log.%s", name, time.Now().Format("20060102150405")))
		if err := os.Rename(oldLog, newLog); err != nil {
			return fmt.Errorf("重命名日志文件失败: %v", err)
		}

		// 创建新的日志文件
		newFile, err := os.OpenFile(oldLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("创建新日志文件失败: %v", err)
		}
		l.processLogs[name] = newFile
	}

	return nil
}

// Info 记录信息级别日志
func (l *Logger) Info(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.LogSystem(fmt.Sprintf("[INFO] %s", message))
}

// Warning 记录警告级别日志
func (l *Logger) Warning(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.LogSystem(fmt.Sprintf("[WARNING] %s", message))
}

// Error 记录错误级别日志
func (l *Logger) Error(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.LogSystem(fmt.Sprintf("[ERROR] %s", message))
}
