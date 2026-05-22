package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
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
	total := atomic.AddInt64(&cw.bytesWritten, int64(n))
	if cw.maxSize > 0 && total >= cw.maxSize && cw.onExceed != nil {
		cw.onExceed()
		atomic.AddInt64(&cw.bytesWritten, -cw.maxSize)
	}
	return
}

// logStream holds per-stream metadata (stdout or stderr for a process).
type logStream struct {
	writer      io.Writer
	path        string
	maxSize     int64
	backupCount int
}

type Logger struct {
	logDir               string
	processLogs          map[string]*logStream
	maxLogSize           int64
	maxBackupCount       int
	systemLogMaxBytes    int64
	systemLogBackupCount int
	compress             bool
	mutex                sync.Mutex
	systemLogMu          sync.Mutex
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
		logDir:               logDir,
		processLogs:          make(map[string]*logStream),
		maxLogSize:           maxLogSize,
		maxBackupCount:       maxBackupCount,
		systemLogMaxBytes:    50 * 1024 * 1024, // 50MB
		systemLogBackupCount: 10,
		compress:             compress,
	}, nil
}

func NewDefaultLogger(logDir string) (*Logger, error) {
	return NewLogger(logDir, 50*1024*1024, 10, true)
}

// GetProcessLogWriters returns stdout and stderr writers for a process.
// Uses per-process settings from cfg when available, falls back to global defaults.
// When stdout and stderr paths are the same, they share a single writer to avoid
// interleaved writes from two file handles to the same file.
func (l *Logger) GetProcessLogWriters(name string, cfg *config.ProgramConfig) (io.Writer, io.Writer, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	stdoutKey := name + "/stdout"
	stderrKey := name + "/stderr"

	// Resolve stdout path and settings
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

	// Resolve stderr path and settings
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

	// If both streams go to the same path, share the same writer to avoid corruption
	if stdoutPath == stderrPath {
		w, err := l.getOrCreateWriter(stdoutKey, stdoutPath, stdoutMaxSize, stdoutBackup)
		if err != nil {
			return nil, nil, err
		}
		// Link stderr key to same stream
		l.processLogs[stderrKey] = l.processLogs[stdoutKey]
		return w, w, nil
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
	if stream, exists := l.processLogs[key]; exists {
		if info, err := os.Stat(filePath); err == nil {
			if info.Size() >= maxSize {
				if err := l.rotateLogByKey(key, stream.path, stream.maxSize, stream.backupCount); err != nil {
					return nil, err
				}
				return l.processLogs[key].writer, nil
			}
		}
		return stream.writer, nil
	}

	if info, err := os.Stat(filePath); err == nil {
		if info.Size() >= maxSize {
			if err := l.rotateFileOutsideLock(filePath, backupCount); err != nil {
				return nil, err
			}
		}
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %v", err)
	}

	ls := &logStream{
		writer:      f,
		path:        filePath,
		maxSize:     maxSize,
		backupCount: backupCount,
	}

	cw := &countingWriter{
		writer:  f,
		maxSize: maxSize,
		onExceed: func() {
			go func() {
				defer func() { _ = recover() }()
				l.rotateIfNeeded(key)
			}()
		},
	}
	ls.writer = cw
	l.processLogs[key] = ls
	return cw, nil
}

func (l *Logger) rotateIfNeeded(key string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	stream, exists := l.processLogs[key]
	if !exists {
		return
	}
	if info, err := os.Stat(stream.path); err == nil && info.Size() >= stream.maxSize {
		l.rotateLogByKey(key, stream.path, stream.maxSize, stream.backupCount)
	}
}

// rotateFileOutsideLock rotates a file that has no active writer stream yet.
func (l *Logger) rotateFileOutsideLock(filePath string, backupCount int) error {
	newPath := filePath + fmt.Sprintf(".%d", time.Now().UnixNano())
	if err := os.Rename(filePath, newPath); err != nil {
		if os.IsNotExist(err) {
			return nil // already rotated by another goroutine
		}
		return fmt.Errorf("重命名日志文件失败: %v", err)
	}
	if l.compress {
		if err := l.compressLog(newPath); err != nil {
			fmt.Printf("压缩日志文件失败: %v\n", err)
		}
	}
	l.cleanupBackups(filePath, backupCount)
	return nil
}

// rotateLogByKey rotates a single log file identified by its stream key.
func (l *Logger) rotateLogByKey(key, filePath string, maxSize int64, backupCount int) error {
	if stream, exists := l.processLogs[key]; exists {
		if cw, ok := stream.writer.(*countingWriter); ok {
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

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("创建新日志文件失败: %v", err)
	}

	cw := &countingWriter{
		writer:  f,
		maxSize: maxSize,
		onExceed: func() {
			go func() {
				defer func() { _ = recover() }()
				l.rotateIfNeeded(key)
			}()
		},
	}

	l.processLogs[key] = &logStream{
		writer:      cw,
		path:        filePath,
		maxSize:     maxSize,
		backupCount: backupCount,
	}
	return nil
}

// rotateLog rotates logs for a process (both stdout and stderr streams).
func (l *Logger) rotateLog(processName string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	stdoutKey := processName + "/stdout"
	stderrKey := processName + "/stderr"

	for _, key := range []string{stdoutKey, stderrKey} {
		if stream, exists := l.processLogs[key]; exists {
			if err := l.rotateLogByKey(key, stream.path, stream.maxSize, stream.backupCount); err != nil {
				return err
			}
		}
	}

	// If no active writers, rotate default file directly if it exists on disk
	defaultPath := filepath.Join(l.logDir, fmt.Sprintf("%s.log", processName))
	if _, hasStdout := l.processLogs[stdoutKey]; !hasStdout {
		if _, hasStderr := l.processLogs[stderrKey]; !hasStderr {
			if _, err := os.Stat(defaultPath); err == nil {
				newPath := defaultPath + fmt.Sprintf(".%d", time.Now().UnixNano())
				if err := os.Rename(defaultPath, newPath); err != nil {
					return fmt.Errorf("重命名日志文件失败: %v", err)
				}
				if l.compress {
					if err := l.compressLog(newPath); err != nil {
						fmt.Printf("压缩日志文件失败: %v\n", err)
					}
				}
				l.cleanupBackups(defaultPath, l.maxBackupCount)
				if err := os.WriteFile(defaultPath, nil, 0644); err != nil {
					return fmt.Errorf("创建新日志文件失败: %v", err)
				}
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
	pattern := basePath + ".[0-9]*"
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

	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].modTime.Before(fileInfos[j].modTime)
	})

	for i := 0; i < len(fileInfos)-backupCount; i++ {
		os.Remove(fileInfos[i].path)
	}
}

func (l *Logger) cleanupOldLogs(processName string) error {
	basePath := filepath.Join(l.logDir, fmt.Sprintf("%s.log", processName))
	l.cleanupBackups(basePath, l.maxBackupCount)
	return nil
}

func (l *Logger) CloseProcessLog(processName string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	stdoutKey := processName + "/stdout"
	stderrKey := processName + "/stderr"

	closed := make(map[*os.File]bool)
	for _, key := range []string{stdoutKey, stderrKey} {
		if stream, exists := l.processLogs[key]; exists {
			if cw, ok := stream.writer.(*countingWriter); ok {
				if f, ok := cw.writer.(*os.File); ok {
					if !closed[f] {
						f.Close()
						closed[f] = true
					}
				}
			}
			delete(l.processLogs, key)
		}
	}
	return nil
}

func (l *Logger) LogSystem(message string) {
	l.systemLogMu.Lock()
	defer l.systemLogMu.Unlock()

	systemLog := filepath.Join(l.logDir, "system.log")

	// Rotate if file exceeds max size
	if info, err := os.Stat(systemLog); err == nil && info.Size() >= l.systemLogMaxBytes {
		if err := l.rotateFileOutsideLock(systemLog, l.systemLogBackupCount); err != nil {
			fmt.Printf("旋转系统日志文件失败: %v\n", err)
		}
	}

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

	for key, stream := range l.processLogs {
		if cw, ok := stream.writer.(*countingWriter); ok {
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
		}
		delete(l.processLogs, key)
	}
	l.processLogs = make(map[string]*logStream)
	return nil
}

func (l *Logger) RotateLogs() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	// Track pointers already rotated to avoid double-processing shared streams.
	rotated := make(map[*logStream]bool)

	for key, stream := range l.processLogs {
		if rotated[stream] {
			continue
		}
		rotated[stream] = true

		if cw, ok := stream.writer.(*countingWriter); ok {
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
		}

		oldPath := stream.path
		newPath := oldPath + fmt.Sprintf(".%d", time.Now().UnixNano())
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("重命名日志文件失败: %v", err)
		}

		if l.compress {
			l.compressLog(newPath)
		}
		l.cleanupBackups(oldPath, stream.backupCount)

		f, err := os.OpenFile(oldPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("创建新日志文件失败: %v", err)
		}

		cw := &countingWriter{
			writer:  f,
			maxSize: stream.maxSize,
			onExceed: func() {
				go l.rotateIfNeeded(key)
			},
		}
		stream.writer = cw
		stream.path = oldPath
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
