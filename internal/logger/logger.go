package logger

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gosupervisor/internal/config"
)

// Level represents a log severity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Format represents the output format for system logs.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// ParseLevel converts a string to a Level (default: LevelInfo).
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// countingWriter 包装 io.Writer 以追踪写入字节数
type countingWriter struct {
	writerMu     sync.Mutex
	writer       io.Writer
	bytesWritten int64
	onExceed     func()
	maxSize      int64
}

func (cw *countingWriter) Write(p []byte) (n int, err error) {
	cw.writerMu.Lock()
	n, err = cw.writer.Write(p)
	cw.writerMu.Unlock()
	total := atomic.AddInt64(&cw.bytesWritten, int64(n))
	maxSize := atomic.LoadInt64(&cw.maxSize)
	if maxSize > 0 && total >= maxSize && cw.onExceed != nil {
		if atomic.AddInt64(&cw.bytesWritten, -maxSize) >= 0 {
			cw.onExceed()
		}
	}
	return
}

// replaceWriter atomically replaces the underlying io.Writer (used during rotation).
func (cw *countingWriter) replaceWriter(w io.Writer) {
	cw.writerMu.Lock()
	cw.writer = w
	cw.writerMu.Unlock()
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
	level                Level
	format               Format
	mutex                sync.Mutex
	systemLogMu          sync.Mutex
}

func NewLogger(logDir string, maxLogSize int64, maxBackupCount int, compress bool, level Level, format Format) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	if maxLogSize <= 0 {
		maxLogSize = 50 * 1024 * 1024
	}

	if maxBackupCount <= 0 {
		maxBackupCount = 10
	}

	if level < LevelDebug || level > LevelError {
		level = LevelInfo
	}
	if format != FormatText && format != FormatJSON {
		format = FormatText
	}

	return &Logger{
		logDir:               logDir,
		processLogs:          make(map[string]*logStream),
		maxLogSize:           maxLogSize,
		maxBackupCount:       maxBackupCount,
		systemLogMaxBytes:    50 * 1024 * 1024, // 50MB
		systemLogBackupCount: 10,
		compress:             compress,
		level:                level,
		format:               format,
	}, nil
}

func NewDefaultLogger(logDir string) (*Logger, error) {
	return NewLogger(logDir, 50*1024*1024, 10, true, LevelInfo, FormatText)
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
	// Recover from a nil map (e.g. Close() was called while a restart goroutine
	// was still in flight). Re-create the map so the assignment doesn't panic.
	if l.processLogs == nil {
		l.processLogs = make(map[string]*logStream)
	}
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
// Updates the existing countingWriter in-place to preserve callers' io.Writer references.
func (l *Logger) rotateLogByKey(key, filePath string, maxSize int64, backupCount int) error {
	stream, exists := l.processLogs[key]

	// Close the old file through the countingWriter so the write lock serializes.
	if exists {
		if cw, ok := stream.writer.(*countingWriter); ok {
			cw.writerMu.Lock()
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
			cw.writerMu.Unlock()
		}
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

	if stream != nil {
		if cw, ok := stream.writer.(*countingWriter); ok {
			cw.writerMu.Lock()
			cw.writer = f
			atomic.StoreInt64(&cw.maxSize, maxSize)
			cw.writerMu.Unlock()
			stream.path = filePath
			return nil
		}
	}

	// No existing stream — create a new one.
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

	rotated := make(map[*logStream]bool)
	for _, key := range []string{stdoutKey, stderrKey} {
		if stream, exists := l.processLogs[key]; exists {
			if rotated[stream] {
				continue
			}
			rotated[stream] = true
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
		sortKey int64 // parsed from filename timestamp, fallback to modtime
	}
	var fileInfos []fileWithTime
	for _, f := range files {
		key := parseBackupTimestamp(f, basePath)
		if key == 0 {
			// Fall back to modtime if timestamp parsing fails
			if info, err := os.Stat(f); err == nil {
				key = info.ModTime().UnixNano()
			}
		}
		if key > 0 {
			fileInfos = append(fileInfos, fileWithTime{f, key})
		}
	}

	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].sortKey < fileInfos[j].sortKey
	})

	for i := 0; i < len(fileInfos)-backupCount; i++ {
		os.Remove(fileInfos[i].path)
	}
}

// parseBackupTimestamp extracts the UnixNano timestamp from a backup filename
// like "path.log.1716551234567890123" or "path.log.1716551234567890123.gz".
func parseBackupTimestamp(filename, basePath string) int64 {
	suffix := strings.TrimPrefix(filename, basePath+".")
	if suffix == filename {
		return 0
	}
	// Strip optional .gz suffix
	suffix = strings.TrimSuffix(suffix, ".gz")
	ts, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil {
		return 0
	}
	return ts
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
	l.writeSystemLog(message, LevelInfo)
}

func (l *Logger) writeSystemLog(message string, level Level) {
	l.systemLogMu.Lock()
	defer l.systemLogMu.Unlock()

	systemLog := filepath.Join(l.logDir, "system.log")

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
	l.mutex.Lock()
	isJSON := l.format == FormatJSON
	l.mutex.Unlock()
	if isJSON {
		levelStr := levelString(level)
		entry := struct {
			Time    string `json:"time"`
			Level   string `json:"level"`
			Message string `json:"message"`
		}{timestamp, levelStr, message}
		_ = json.NewEncoder(file).Encode(entry)
	} else {
		fmt.Fprintf(file, "[%s] [%s] %s\n", timestamp, levelString(level), message)
	}
}

func levelString(level Level) string {
	switch level {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARNING"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// SetLevel sets the minimum log level. Safe for concurrent use.
func (l *Logger) SetLevel(level Level) {
	l.mutex.Lock()
	l.level = level
	l.mutex.Unlock()
}

// SetFormat sets the system log output format. Safe for concurrent use.
func (l *Logger) SetFormat(format Format) {
	l.mutex.Lock()
	l.format = format
	l.mutex.Unlock()
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	l.mutex.Lock()
	minLevel := l.level
	l.mutex.Unlock()
	if level < minLevel {
		return
	}
	l.writeSystemLog(fmt.Sprintf(format, args...), level)
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

func (l *Logger) Warning(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

func (l *Logger) Close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	closed := make(map[*os.File]bool)
	for key, stream := range l.processLogs {
		if cw, ok := stream.writer.(*countingWriter); ok {
			if f, ok := cw.writer.(*os.File); ok {
				if !closed[f] {
					closed[f] = true
					f.Close()
				}
			}
		}
		delete(l.processLogs, key)
	}
	// Don't nil processLogs — a racing restart goroutine may still
	// call GetProcessLogWriters, which would assign to a nil map.
	return nil
}

func (l *Logger) RotateLogs() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	// Track pointers already rotated to avoid double-processing shared streams.
	rotated := make(map[*logStream]bool)

	for key, stream := range l.processLogs {
		key := key // capture loop variable
		if rotated[stream] {
			continue
		}
		rotated[stream] = true

		if cw, ok := stream.writer.(*countingWriter); ok {
			cw.writerMu.Lock()
			if f, ok := cw.writer.(*os.File); ok {
				f.Close()
			}
			cw.writerMu.Unlock()
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

		// Update countingWriter in-place to preserve callers' io.Writer refs.
		if cw, ok := stream.writer.(*countingWriter); ok {
			cw.replaceWriter(f)
			atomic.StoreInt64(&cw.maxSize, stream.maxSize)
		} else {
			stream.writer = &countingWriter{
				writer:  f,
				maxSize: stream.maxSize,
				onExceed: func() {
					go func() {
						defer func() { _ = recover() }()
						l.rotateIfNeeded(key)
					}()
				},
			}
		}
		stream.path = oldPath
	}

	return nil
}
