package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gosupervisor/internal/config"
)

// countingWriter 包装 io.Writer 以追踪写入字节数
type countingWriter struct {
	writer       io.Writer
	bytesWritten int64
	onExceed     func()
	maxSize      int64
}

func (cw *countingWriter) Write(p []byte) (n int, err error) {
	n, err = cw.writer.Write(p)
	cw.bytesWritten += int64(n)
	if cw.maxSize > 0 && cw.bytesWritten >= cw.maxSize && cw.onExceed != nil {
		cw.onExceed()
		cw.bytesWritten = 0
	}
	return
}

type Logger struct {
	logDir         string
	processLogs    map[string]io.Writer
	maxLogSize     int64
	maxBackupCount int
	compress       bool
	mutex          sync.Mutex
	logFileSizes   map[string]int64
}

func NewLogger(logDir string, maxLogSize int64, maxBackupCount int, compress bool) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	if maxLogSize <= 0 {
		maxLogSize = 50 * 1024 * 1024
	}

	if maxBackupCount <= 0 {
		maxBackupCount = 10
	}

	return &Logger{
		logDir:         logDir,
		processLogs:    make(map[string]io.Writer),
		maxLogSize:     maxLogSize,
		maxBackupCount: maxBackupCount,
		compress:       compress,
		logFileSizes:   make(map[string]int64),
	}, nil
}

func NewDefaultLogger(logDir string) (*Logger, error) {
	return NewLogger(logDir, 50*1024*1024, 10, true)
}

// GetProcessLogWriters returns stdout and stderr writers for a process.
// Uses per-process settings from cfg when available, falls back to global defaults.
func (l *Logger) GetProcessLogWriters(name string, cfg *config.ProgramConfig) (io.Writer, io.Writer, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	stdoutKey := name + "/stdout"
	stderrKey := name + "/stderr"

	stdoutPath := cfg.StdoutLogFile
	if stdoutPath == "" {
		stdoutPath = filepath.Join(l.logDir, fmt.Sprintf("%s.log", name))
	}
	stdoutMaxSize := cfg.StdoutLogMaxBytes
	if stdoutMaxSize <= 0 {
		stdoutMaxSize = l.maxLogSize
	}
	stdoutBackup := cfg.StdoutLogBackupCount
	if stdoutBackup <= 0 {
		stdoutBackup = l.maxBackupCount
	}

	stderrPath := cfg.StderrLogFile
	if stderrPath == "" {
		stderrPath = filepath.Join(l.logDir, fmt.Sprintf("%s.log", name))
	}
	stderrMaxSize := cfg.StderrLogMaxBytes
	if stderrMaxSize <= 0 {
		stderrMaxSize = l.maxLogSize
	}
	stderrBackup := cfg.StderrLogBackupCount
	if stderrBackup <= 0 {
		stderrBackup = l.maxBackupCount
	}

	stdoutWriter, err := l.getOrCreateWriter(stdoutKey, stdoutPath, stdoutMaxSize, stdoutBackup)
	if err != nil {
		return nil, nil, err
	}
	stderrWriter, err := l.getOrCreateWriter(stderrKey, stderrPath, stderrMaxSize, stderrBackup)
	if err != nil {
		return nil, nil, err
	}

	return stdoutWriter, stderrWriter, nil
}

// getOrCreateWriter returns an existing writer or creates a new one with rotation check.
func (l *Logger) getOrCreateWriter(key, filePath string, maxSize int64, backupCount int) (io.Writer, error) {
	if writer, exists := l.processLogs[key]; exists {
		if info, err := os.Stat(filePath); err == nil {
			if info.Size() >= maxSize {
				if err := l.rotateLogByKey(key, filePath, maxSize, backupCount); err != nil {
					return nil, err
				}
				return l.processLogs[key], nil
			}
		}
		return writer, nil
	}

	if info, err := os.Stat(filePath); err == nil {
		if info.Size() >= maxSize {
			if err := l.rotateLogByKey(key, filePath, maxSize, backupCount); err != nil {
				return nil, err
			}
		}
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %v", err)
	}

	cw := &countingWriter{
		writer:  f,
		maxSize: maxSize,
		onExceed: func() {
			go l.rotateIfNeeded(key, filePath, maxSize, backupCount)
		},
	}
	l.processLogs[key] = cw
	l.logFileSizes[key] = 0
	return cw, nil
}

func (l *Logger) rotateIfNeeded(key, filePath string, maxSize int64, backupCount int) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if info, err := os.Stat(filePath); err == nil && info.Size() >= maxSize {
		l.rotateLogByKey(key, filePath, maxSize, backupCount)
	}
}

// rotateLogByKey rotates a single log file identified by its stream key.
func (l *Logger) rotateLogByKey(key, filePath string, maxSize int64, backupCount int) error {
	if writer, exists := l.processLogs[key]; exists {
		if cw, ok := writer.(*countingWriter); ok {
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
		}
		delete(l.processLogs, key)
	}

	newPath := filePath + fmt.Sprintf(".%d", time.Now().UnixNano())
	if err := os.Rename(filePath, newPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("重命名日志文件失败: %v", err)
	}

	if l.compress {
		if err := l.compressLog(newPath); err != nil {
			fmt.Printf("压缩日志文件失败: %v\n", err)
		}
	}

	l.cleanupBackups(filePath, backupCount)

	newFile, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("创建新日志文件失败: %v", err)
	}
	l.processLogs[key] = newFile
	l.logFileSizes[key] = 0
	return nil
}

// rotateLog rotates logs for a process (both stdout and stderr streams).
func (l *Logger) rotateLog(processName string) error {
	stdoutKey := processName + "/stdout"
	stderrKey := processName + "/stderr"
	defaultPath := filepath.Join(l.logDir, fmt.Sprintf("%s.log", processName))

	for _, key := range []string{stdoutKey, stderrKey} {
		if _, exists := l.processLogs[key]; exists {
			if err := l.rotateLogByKey(key, defaultPath, l.maxLogSize, l.maxBackupCount); err != nil {
				return err
			}
		}
	}

	// If no active writers, rotate the file directly if it exists on disk
	if _, hasStdout := l.processLogs[stdoutKey]; !hasStdout {
		if _, hasStderr := l.processLogs[stderrKey]; !hasStderr {
			if _, err := os.Stat(defaultPath); err == nil {
				newPath := defaultPath + fmt.Sprintf(".%d", time.Now().UnixNano())
				os.Rename(defaultPath, newPath)
				if l.compress {
					l.compressLog(newPath)
				}
				l.cleanupBackups(defaultPath, l.maxBackupCount)
				os.WriteFile(defaultPath, nil, 0644)
			}
		}
	}
	return nil
}

func (l *Logger) compressLog(logFile string) error {
	srcFile, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %v", err)
	}
	defer srcFile.Close()

	gzFile := logFile + ".gz"
	dstFile, err := os.Create(gzFile)
	if err != nil {
		return fmt.Errorf("创建压缩文件失败: %v", err)
	}
	defer dstFile.Close()

	gzWriter := gzip.NewWriter(dstFile)
	defer gzWriter.Close()

	if _, err := io.Copy(gzWriter, srcFile); err != nil {
		return fmt.Errorf("压缩日志文件失败: %v", err)
	}

	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("关闭gzip写入器失败: %v", err)
	}

	if err := os.Remove(logFile); err != nil {
		return fmt.Errorf("删除源日志文件失败: %v", err)
	}

	return nil
}

// cleanupBackups removes old backup files keeping only the last backupCount.
func (l *Logger) cleanupBackups(basePath string, backupCount int) {
	pattern := basePath + ".*"
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) <= backupCount {
		return
	}

	type fileWithTime struct {
		path    string
		modTime time.Time
	}
	var fileInfos []fileWithTime
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileWithTime{f, info.ModTime()})
	}

	// Sort oldest first
	for i := 0; i < len(fileInfos); i++ {
		for j := i + 1; j < len(fileInfos); j++ {
			if fileInfos[i].modTime.After(fileInfos[j].modTime) {
				fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
			}
		}
	}

	for i := 0; i < len(fileInfos)-backupCount; i++ {
		os.Remove(fileInfos[i].path)
	}
}

func (l *Logger) cleanupOldLogs(processName string) error {
	pattern := filepath.Join(l.logDir, fmt.Sprintf("%s.log.*", processName))
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) <= l.maxBackupCount {
		return err
	}

	type fileWithTime struct {
		path    string
		modTime time.Time
	}
	var fileInfos []fileWithTime
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileWithTime{f, info.ModTime()})
	}

	for i := 0; i < len(fileInfos); i++ {
		for j := i + 1; j < len(fileInfos); j++ {
			if fileInfos[i].modTime.After(fileInfos[j].modTime) {
				fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
			}
		}
	}

	for i := 0; i < len(fileInfos)-l.maxBackupCount; i++ {
		os.Remove(fileInfos[i].path)
	}

	return nil
}

func (l *Logger) CloseProcessLog(processName string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, suffix := range []string{"/stdout", "/stderr"} {
		key := processName + suffix
		if writer, exists := l.processLogs[key]; exists {
			if cw, ok := writer.(*countingWriter); ok {
				if f, ok := cw.writer.(*os.File); ok {
					f.Close()
				}
			}
			delete(l.processLogs, key)
		}
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

	for key, writer := range l.processLogs {
		if cw, ok := writer.(*countingWriter); ok {
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
		}
		delete(l.processLogs, key)
	}
	l.processLogs = make(map[string]io.Writer)
	return nil
}

func (l *Logger) RotateLogs() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for key, writer := range l.processLogs {
		if cw, ok := writer.(*countingWriter); ok {
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
		}

		// Extract process name from key "name/stdout" or "name/stderr"
		name := key
		if idx := len(name) - 7; idx > 0 && name[idx:] == "/stdout" {
			name = name[:idx]
		} else if idx := len(name) - 7; idx > 0 && name[idx:] == "/stderr" {
			name = name[:idx]
		}

		oldLog := filepath.Join(l.logDir, fmt.Sprintf("%s.log", name))
		newLog := filepath.Join(l.logDir, fmt.Sprintf("%s.log.%d", name, time.Now().UnixNano()))
		if err := os.Rename(oldLog, newLog); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("重命名日志文件失败: %v", err)
		}

		newFile, err := os.OpenFile(oldLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("创建新日志文件失败: %v", err)
		}
		l.processLogs[key] = newFile
	}

	return nil
}

func (l *Logger) Info(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.LogSystem(fmt.Sprintf("[INFO] %s", message))
}

func (l *Logger) Warning(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.LogSystem(fmt.Sprintf("[WARNING] %s", message))
}

func (l *Logger) Error(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.LogSystem(fmt.Sprintf("[ERROR] %s", message))
}
