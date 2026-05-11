package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"gosupervisor/internal/process"
)

type WebServer struct {
	processManager *process.ProcessManager
	logDir         string
	mux            *http.ServeMux
	authUser       string
	authPass       string
	apiAuth        bool

	indexTmpl         *template.Template
	logsTmpl          *template.Template
	systemInfoTmpl    *template.Template
	processDetailTmpl *template.Template
}

func NewWebServer(processManager *process.ProcessManager, logDir string) (*WebServer, error) {
	return NewWebServerWithAuth(processManager, logDir, "", "", false)
}

// NewWebServerWithAuth creates a WebServer with optional HTTP Basic Auth.
func NewWebServerWithAuth(processManager *process.ProcessManager, logDir, user, pass string, apiAuth bool) (*WebServer, error) {
	funcs := template.FuncMap{
		"lower": func(s interface{}) string {
			return strings.ToLower(fmt.Sprintf("%v", s))
		},
	}

	indexTmpl := template.Must(template.New("index").Funcs(funcs).Parse(indexTemplate))
	logsTmpl := template.Must(template.New("logs").Parse(logsTemplate))
	systemInfoTmpl := template.Must(template.New("system").Parse(systemInfoTemplate))
	processDetailTmpl := template.Must(template.New("process").Funcs(funcs).Parse(processDetailTemplate))

	mux := http.NewServeMux()
	ws := &WebServer{
		processManager:    processManager,
		logDir:            logDir,
		mux:               mux,
		authUser:          user,
		authPass:          pass,
		apiAuth:           apiAuth,
		indexTmpl:         indexTmpl,
		logsTmpl:          logsTmpl,
		systemInfoTmpl:    systemInfoTmpl,
		processDetailTmpl: processDetailTmpl,
	}

	mux.HandleFunc("/", ws.handleIndex)
	mux.HandleFunc("/api/processes", ws.handleAPIProcesses)
	mux.HandleFunc("/api/v1/processes", ws.handleAPIV1Processes)
	mux.HandleFunc("/api/v1/processes/", ws.handleAPIV1ProcessAction)
	mux.HandleFunc("/api/v1/groups/", ws.handleAPIV1GroupAction)
	mux.HandleFunc("/start", ws.handleStart)
	mux.HandleFunc("/stop", ws.handleStop)
	mux.HandleFunc("/restart", ws.handleRestart)
	mux.HandleFunc("/logs", ws.handleLogs)
	mux.HandleFunc("/system", ws.handleSystemInfo)
	mux.HandleFunc("/process", ws.handleProcessDetail)

	return ws, nil
}

// basicAuth wraps a handler with HTTP Basic Auth if credentials are configured.
func (ws *WebServer) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ws.authUser == "" && ws.authPass == "" {
			next(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != ws.authUser || pass != ws.authPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoSupervisor"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// jsonResponse writes a JSON response.
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// apiV1ProcessesList is the JSON response for GET /api/v1/processes
type apiV1ProcessesList struct {
	Status    string             `json:"status"`
	Processes []process.Snapshot `json:"processes"`
}

// apiV1Status is a generic JSON status response.
type apiV1Status struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// validateProcessName rejects names with path separators or parent references.
func validateProcessName(name string) bool {
	return name != "" && !strings.Contains(name, "/") && !strings.Contains(name, "..") && !strings.Contains(name, "\\")
}

// isSameOrigin checks that the request's Origin or Referer matches the Host.
// This provides basic CSRF protection for state-changing endpoints.
func isSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return false
	}
	if r.Host == "" {
		return false
	}
	return strings.Contains(origin, r.Host)
}

func (ws *WebServer) Start(addr string) error {
	fmt.Printf("Web服务器启动在 %s\n", addr)
	handler := http.Handler(ws.mux)
	if ws.authUser != "" || ws.authPass != "" {
		handler = ws.authMiddleware(ws.mux)
		fmt.Println("Web界面已启用 HTTP Basic Auth 认证")
	}
	return http.ListenAndServe(addr, handler)
}

func (ws *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth disabled
		if ws.authUser == "" && ws.authPass == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Allow /api/v1/ without auth unless apiAuth is enabled
		if !ws.apiAuth && strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != ws.authUser || pass != ws.authPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoSupervisor"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (ws *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	snapshots := make([]process.Snapshot, 0)
	ws.processManager.RangeProcesses(func(name string, p *process.Process) {
		snapshots = append(snapshots, p.Snapshot())
	})

	data := struct {
		Processes []process.Snapshot
		Time      string
	}{
		Processes: snapshots,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
	}

	if err := ws.indexTmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleAPIProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	snapshots := make([]process.Snapshot, 0)
	ws.processManager.RangeProcesses(func(name string, p *process.Process) {
		snapshots = append(snapshots, p.Snapshot())
	})
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshots); err != nil {
		http.Error(w, fmt.Sprintf("编码JSON失败: %v", err), http.StatusInternalServerError)
	}
}

// --- API v1 handlers ---

func (ws *WebServer) handleAPIV1Processes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, apiV1Status{Status: "error", Message: "Method Not Allowed"})
		return
	}
	snapshots := make([]process.Snapshot, 0)
	ws.processManager.RangeProcesses(func(name string, p *process.Process) {
		snapshots = append(snapshots, p.Snapshot())
	})
	jsonResponse(w, http.StatusOK, apiV1ProcessesList{Status: "ok", Processes: snapshots})
}

func (ws *WebServer) handleAPIV1ProcessAction(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/processes/{name}[/{action}]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/processes/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	if name == "" || !validateProcessName(name) {
		jsonResponse(w, http.StatusBadRequest, apiV1Status{Status: "error", Message: "Bad Request"})
		return
	}

	// POST requests require CSRF protection
	if r.Method == http.MethodPost && !isSameOrigin(r) {
		jsonResponse(w, http.StatusForbidden, apiV1Status{Status: "error", Message: "Forbidden"})
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	p := ws.processManager.GetProcess(name)
	if p == nil {
		jsonResponse(w, http.StatusNotFound, apiV1Status{Status: "error", Message: "Process not found"})
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		snap := p.Snapshot()
		jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "ok", "process": snap})
	case action == "start" && r.Method == http.MethodPost:
		if err := p.Start(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process started"})
	case action == "stop" && r.Method == http.MethodPost:
		if err := p.Stop(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process stopped"})
	case action == "restart" && r.Method == http.MethodPost:
		if err := p.Restart(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process restarted"})
	case action == "logs" && r.Method == http.MethodGet:
		ws.handleProcessLogsStream(w, r, name)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, apiV1Status{Status: "error", Message: "Method Not Allowed"})
	}
}

func (ws *WebServer) handleAPIV1GroupAction(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/groups/{group}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/groups/")
	parts := strings.SplitN(path, "/", 2)
	group := parts[0]
	if group == "" || !validateProcessName(group) {
		jsonResponse(w, http.StatusBadRequest, apiV1Status{Status: "error", Message: "Bad Request"})
		return
	}

	// POST requests require CSRF protection
	if r.Method == http.MethodPost && !isSameOrigin(r) {
		jsonResponse(w, http.StatusForbidden, apiV1Status{Status: "error", Message: "Forbidden"})
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	var result []string
	var errMsg string
	switch {
	case action == "start" && r.Method == http.MethodPost:
		result = ws.processManager.StartGroup(group)
	case action == "stop" && r.Method == http.MethodPost:
		result = ws.processManager.StopGroup(group)
	case action == "restart" && r.Method == http.MethodPost:
		result = ws.processManager.RestartGroup(group)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, apiV1Status{Status: "error", Message: "Method Not Allowed"})
		return
	}

	if errMsg != "" {
		jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: errMsg})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "ok", "processes": result})
}

func (ws *WebServer) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isSameOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	processName := r.FormValue("name")
	if processName == "" || !validateProcessName(processName) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	p := ws.processManager.GetProcess(processName)
	if p == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := p.Start(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (ws *WebServer) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isSameOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	processName := r.FormValue("name")
	if processName == "" || !validateProcessName(processName) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	p := ws.processManager.GetProcess(processName)
	if p == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := p.Stop(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (ws *WebServer) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isSameOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	processName := r.FormValue("name")
	if processName == "" || !validateProcessName(processName) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	p := ws.processManager.GetProcess(processName)
	if p == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := p.Restart(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (ws *WebServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	processName := r.URL.Query().Get("name")
	if processName == "" || !validateProcessName(processName) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	p := ws.processManager.GetProcess(processName)
	if p == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Use the process's configured stdout log path, falling back to default.
	s := p.Snapshot()
	logPath := s.Config.StdoutLogFile
	if logPath == "" {
		logPath = filepath.Join(ws.logDir, fmt.Sprintf("%s.log", processName))
	}

	logContent, err := readTailLines(logPath, tailMaxLines, tailMaxBytes)
	if err != nil {
		logContent = []byte(fmt.Sprintf("无法读取日志文件: %v", err))
	}

	data := struct {
		ProcessName string
		LogContent  string
	}{
		ProcessName: processName,
		LogContent:  string(logContent),
	}

	if err := ws.logsTmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

// handleProcessLogsStream streams a process log via Server-Sent Events.
func (ws *WebServer) handleProcessLogsStream(w http.ResponseWriter, r *http.Request, name string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	logFile := filepath.Join(ws.logDir, name+".log")
	f, err := os.Open(logFile)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\": \"无法打开日志文件\"}\n\n")
		flusher.Flush()
		return
	}
	defer f.Close()

	// Seek to end for tail -f behavior
	f.Seek(0, io.SeekEnd)

	ctx := r.Context()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			buf := make([]byte, 65536)
			n, err := f.Read(buf)
			if err != nil && err != io.EOF {
				return
			}
			if n > 0 {
				// JSON-encode each chunk for clean SSE
				escaped := strings.ReplaceAll(string(buf[:n]), "\n", "\\n")
				escaped = strings.ReplaceAll(escaped, "\r", "")
				fmt.Fprintf(w, "data: {\"text\": \"%s\"}\n\n", escaped)
				flusher.Flush()
			}
		}
	}
}

func (ws *WebServer) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	systemInfo := getSystemInfo()
	if err := ws.systemInfoTmpl.Execute(w, systemInfo); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleProcessDetail(w http.ResponseWriter, r *http.Request) {
	processName := r.URL.Query().Get("name")
	if processName == "" || !validateProcessName(processName) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	p := ws.processManager.GetProcess(processName)
	if p == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	snap := p.Snapshot()
	if err := ws.processDetailTmpl.Execute(w, snap); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

// SystemInfo 系统信息结构体
type SystemInfo struct {
	OS           string
	Arch         string
	Hostname     string
	CPUCount     int
	MemoryTotal  uint64
	MemoryUsed   uint64
	DiskTotal    uint64
	DiskUsed     uint64
	Uptime       string
	GoVersion    string
	ProcessCount int
}

// getSystemInfo 获取系统信息
func getSystemInfo() *SystemInfo {
	hostname, _ := os.Hostname()
	info := &SystemInfo{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Hostname:     hostname,
		CPUCount:     runtime.NumCPU(),
		GoVersion:    runtime.Version(),
		ProcessCount: countProcesses(),
	}

	// 从 /proc/meminfo 读取内存信息
	if meminfo, err := os.Open("/proc/meminfo"); err == nil {
		defer meminfo.Close()
		scanner := bufio.NewScanner(meminfo)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					var total int64
					fmt.Sscanf(fields[1], "%d", &total)
					info.MemoryTotal = uint64(total) * 1024
				}
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					var avail int64
					fmt.Sscanf(fields[1], "%d", &avail)
					info.MemoryUsed = info.MemoryTotal - uint64(avail)*1024
				}
			}
		}
	}

	// 从 /proc/uptime 读取系统运行时间
	if uptime, err := os.Open("/proc/uptime"); err == nil {
		defer uptime.Close()
		var uptimeSecs float64
		fmt.Fscanf(uptime, "%f", &uptimeSecs)
		hours := int(uptimeSecs) / 3600
		mins := (int(uptimeSecs) % 3600) / 60
		info.Uptime = fmt.Sprintf("%dh %dm", hours, mins)
	}

	// 从 df -B1 命令获取磁盘信息（POSIX 兼容，字节输出）
	cmd := exec.Command("df", "-B1", "/")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 1 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 3 {
				var total, used int64
				fmt.Sscanf(fields[1], "%d", &total)
				fmt.Sscanf(fields[2], "%d", &used)
				info.DiskTotal = uint64(total)
				info.DiskUsed = uint64(used)
			}
		}
	}

	return info
}

const (
	tailMaxLines = 1000
	tailMaxBytes = 1 * 1024 * 1024 // 1MB
)

// readTailLines reads the last maxLines lines from a file,
// bounded by maxBytes from the end of the file.
func readTailLines(path string, maxLines int, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := fi.Size()
	readSize := maxBytes
	if readSize > fileSize {
		readSize = fileSize
	}

	_, err = f.Seek(-readSize, io.SeekEnd)
	if err != nil {
		// File too small or seek failed; read from start
		f.Seek(0, io.SeekStart)
		readSize = fileSize
	}

	buf := make([]byte, readSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	buf = buf[:n]

	// Find the start position: skip the first partial line (if we didn't start at BOF)
	// and keep at most maxLines
	lines := 0
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			lines++
			if lines > maxLines {
				return buf[i+1:], nil
			}
		}
	}

	return buf, nil
}

// countProcesses 统计系统中运行的进程数
func countProcesses() int {
	if dir, err := os.Open("/proc"); err == nil {
		defer dir.Close()
		if entries, err := dir.Readdirnames(-1); err == nil {
			count := 0
			var n int
			for _, entry := range entries {
				if _, err := fmt.Sscanf(entry, "%d", &n); err == nil {
					count++
				}
			}
			return count
		}
	}
	return 0
}

const indexTemplate = `<!DOCTYPE html>
<html>
<head>
	<title>GoSupervisor</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			margin: 20px;
		}
		h1 {
			color: #333;
		}
		table {
			width: 100%;
			border-collapse: collapse;
			margin-top: 20px;
		}
		th, td {
			padding: 10px;
			text-align: left;
			border-bottom: 1px solid #ddd;
		}
		th {
			background-color: #f2f2f2;
		}
		tr:hover {
			background-color: #f5f5f5;
		}
		.status-running {
			color: green;
		}
		.status-stopped {
			color: red;
		}
		.status-starting {
			color: orange;
		}
		.status-stopping {
			color: orange;
		}
		.status-exited {
			color: gray;
		}
		.status-fatal {
			color: darkred;
		}
		button {
			padding: 5px 10px;
			margin-right: 5px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
		}
		.btn-start {
			background-color: green;
			color: white;
		}
		.btn-stop {
			background-color: red;
			color: white;
		}
		.btn-restart {
			background-color: blue;
			color: white;
		}
		.btn-logs {
			background-color: purple;
			color: white;
		}
		.btn-detail {
			background-color: orange;
			color: white;
		}
		.footer {
			margin-top: 20px;
			font-size: 12px;
			color: #666;
		}
	</style>
</head>
<body>
	<h1>GoSupervisor 进程管理</h1>
	<table>
		<tr>
			<th>进程名称</th>
			<th>状态</th>
			<th>PID</th>
			<th>启动时间</th>
			<th>停止时间</th>
			<th>退出码</th>
			<th>启动重试次数</th>
			<th>操作</th>
		</tr>
		<tbody id="process-table-body">
		{{range .Processes}}
		<tr>
			<td>{{.Name}}</td>
			<td class="status-{{lower .State}}">{{.State}}</td>
			<td>{{if gt .PID 0}}{{.PID}}{{else}}-{{end}}</td>
			<td>{{if not .StartTime.IsZero}}{{.StartTime.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</td>
			<td>{{if not .StopTime.IsZero}}{{.StopTime.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</td>
			<td>{{if ne .ExitCode 0}}{{.ExitCode}}{{else}}-{{end}}</td>
			<td>{{.StartRetries}}</td>
			<td>
				{{if ne .State "RUNNING"}}
				<form method="post" action="/start" style="display:inline">
					<input type="hidden" name="name" value="{{.Name}}">
					<button class="btn-start" type="submit">启动</button>
				</form>
				{{end}}
				{{if eq .State "RUNNING"}}
				<form method="post" action="/stop" style="display:inline">
					<input type="hidden" name="name" value="{{.Name}}">
					<button class="btn-stop" type="submit">停止</button>
				</form>
				{{end}}
				<form method="post" action="/restart" style="display:inline">
					<input type="hidden" name="name" value="{{.Name}}">
					<button class="btn-restart" type="submit">重启</button>
				</form>
				<form method="get" action="/logs" style="display:inline">
					<input type="hidden" name="name" value="{{.Name}}">
					<button class="btn-logs" type="submit">查看日志</button>
				</form>
				<form method="get" action="/process" style="display:inline">
					<input type="hidden" name="name" value="{{.Name}}">
					<button class="btn-detail" type="submit">详情</button>
				</form>
			</td>
		</tr>
		{{end}}
		</tbody>
	</table>
	<div class="footer">
		<p>最后更新: <span id="last-updated">{{.Time}}</span></p>
		<p>GoSupervisor - 进程管理工具</p>
	</div>
		<script>
		function esc(s) {
			return s.replace(/&/g,'&amp;').replace(/</g,'&lt;')
			        .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
		}
		function fmtTime(ts) {
			if (!ts) return '-';
			var d = new Date(ts);
			if (d.getFullYear() <= 1) return '-';
			return d.getFullYear() + '-' +
				String(d.getMonth()+1).padStart(2,'0') + '-' +
				String(d.getDate()).padStart(2,'0') + ' ' +
				String(d.getHours()).padStart(2,'0') + ':' +
				String(d.getMinutes()).padStart(2,'0') + ':' +
				String(d.getSeconds()).padStart(2,'0');
		}
		function updateTable() {
			fetch('/api/processes')
				.then(function(r) { return r.json(); })
				.then(function(procs) {
					var h = '';
					for (var i = 0; i < procs.length; i++) {
						var p = procs[i];
						var st  = (p.State || '').toLowerCase();
						var pid = p.PID > 0 ? String(p.PID) : '-';
						var start = fmtTime(p.StartTime);
						var stop  = fmtTime(p.StopTime);
						var ec    = p.ExitCode !== 0 ? p.ExitCode : '-';

						h += '<tr>' +
							'<td>' + esc(p.Name) + '</td>' +
							'<td class="status-' + st + '">' + esc(p.State) + '</td>' +
							'<td>' + pid + '</td>' +
							'<td>' + start + '</td>' +
							'<td>' + stop + '</td>' +
							'<td>' + ec + '</td>' +
							'<td>' + p.StartRetries + '</td>' +
							'<td>';

						var nm = esc(p.Name);
						if (p.State !== 'RUNNING') {
							h += '<form method="post" action="/start" style="display:inline">' +
								'<input type="hidden" name="name" value="' + nm + '">' +
								'<button class="btn-start" type="submit">启动</button></form>';
						}
						if (p.State === 'RUNNING') {
							h += '<form method="post" action="/stop" style="display:inline">' +
								'<input type="hidden" name="name" value="' + nm + '">' +
								'<button class="btn-stop" type="submit">停止</button></form>';
						}
						h += '<form method="post" action="/restart" style="display:inline">' +
							'<input type="hidden" name="name" value="' + nm + '">' +
							'<button class="btn-restart" type="submit">重启</button></form>' +
							'<form method="get" action="/logs" style="display:inline">' +
							'<input type="hidden" name="name" value="' + nm + '">' +
							'<button class="btn-logs" type="submit">查看日志</button></form>' +
							'<form method="get" action="/process" style="display:inline">' +
							'<input type="hidden" name="name" value="' + nm + '">' +
							'<button class="btn-detail" type="submit">详情</button></form>';

						h += '</td></tr>';
					}
					document.getElementById('process-table-body').innerHTML = h;
					document.getElementById('last-updated').textContent =
						fmtTime(new Date().toISOString());
				})
				.catch(function(e) {
					console.error('process update failed:', e);
				});
		}
		updateTable();
		setInterval(updateTable, 2000);
	</script>
</body>
</html>`

const logsTemplate = `<!DOCTYPE html>
<html>
<head>
	<title>GoSupervisor - 进程日志</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			margin: 20px;
		}
		h1 {
			color: #333;
		}
		.log-container {
			background-color: #f5f5f5;
			padding: 20px;
			border-radius: 5px;
			margin-top: 20px;
			max-height: 600px;
			overflow-y: auto;
			font-family: monospace;
			white-space: pre-wrap;
		}
		.back-button {
			margin-top: 20px;
			padding: 10px 20px;
			background-color: #333;
			color: white;
			border: none;
			border-radius: 3px;
			cursor: pointer;
		}
		.back-button:hover {
			background-color: #555;
		}
		.footer {
			margin-top: 20px;
			font-size: 12px;
			color: #666;
		}
	</style>
</head>
<body>
	<h1>GoSupervisor - 进程日志</h1>
	<h2>进程: {{.ProcessName}}</h2>
	<div class="log-container">
		{{.LogContent}}
	</div>
	<button class="back-button" onclick="window.location.href='/'">返回首页</button>
	<div class="footer">
		<p>GoSupervisor - 进程管理工具</p>
	</div>
	<script>
		// 自动滚动到日志底部
		window.onload = function() {
			var logContainer = document.querySelector('.log-container');
			logContainer.scrollTop = logContainer.scrollHeight;
		};
	</script>
</body>
</html>`

const systemInfoTemplate = `<!DOCTYPE html>
<html>
<head>
	<title>GoSupervisor - 系统信息</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			margin: 20px;
		}
		h1 {
			color: #333;
		}
		.info-container {
			background-color: #f5f5f5;
			padding: 20px;
			border-radius: 5px;
			margin-top: 20px;
		}
		.info-item {
			margin-bottom: 10px;
			padding: 10px;
			background-color: white;
			border-radius: 3px;
			box-shadow: 0 1px 3px rgba(0,0,0,0.1);
		}
		.info-label {
			font-weight: bold;
			color: #555;
			margin-right: 10px;
		}
		.back-button {
			margin-top: 20px;
			padding: 10px 20px;
			background-color: #333;
			color: white;
			border: none;
			border-radius: 3px;
			cursor: pointer;
		}
		.back-button:hover {
			background-color: #555;
		}
		.footer {
			margin-top: 20px;
			font-size: 12px;
			color: #666;
		}
		.navbar {
			background-color: #333;
			color: white;
			padding: 10px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.navbar a {
			color: white;
			text-decoration: none;
			margin-right: 20px;
		}
		.navbar a:hover {
			text-decoration: underline;
		}
	</style>
</head>
<body>
	<div class="navbar">
		<a href="/">进程管理</a>
		<a href="/system">系统信息</a>
	</div>
	<h1>GoSupervisor - 系统信息</h1>
	<div class="info-container">
		<div class="info-item">
			<span class="info-label">操作系统:</span>
			<span>{{.OS}} {{.Arch}}</span>
		</div>
		<div class="info-item">
			<span class="info-label">主机名:</span>
			<span>{{.Hostname}}</span>
		</div>
		<div class="info-item">
			<span class="info-label">CPU核心数:</span>
			<span>{{.CPUCount}}</span>
		</div>
		<div class="info-item">
			<span class="info-label">内存总量:</span>
			<span>{{.MemoryTotal}} bytes</span>
		</div>
		<div class="info-item">
			<span class="info-label">内存使用:</span>
			<span>{{.MemoryUsed}} bytes</span>
		</div>
		<div class="info-item">
			<span class="info-label">磁盘总量:</span>
			<span>{{.DiskTotal}} bytes</span>
		</div>
		<div class="info-item">
			<span class="info-label">磁盘使用:</span>
			<span>{{.DiskUsed}} bytes</span>
		</div>
		<div class="info-item">
			<span class="info-label">系统运行时间:</span>
			<span>{{.Uptime}}</span>
		</div>
		<div class="info-item">
			<span class="info-label">Go版本:</span>
			<span>{{.GoVersion}}</span>
		</div>
		<div class="info-item">
			<span class="info-label">进程数量:</span>
			<span>{{.ProcessCount}}</span>
		</div>
	</div>
	<button class="back-button" onclick="window.location.href='/'">返回首页</button>
	<div class="footer">
		<p>GoSupervisor - 进程管理工具</p>
	</div>
</body>
</html>`

const processDetailTemplate = `<!DOCTYPE html>
<html>
<head>
	<title>GoSupervisor - 进程详情</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			margin: 20px;
		}
		h1 {
			color: #333;
		}
		.detail-container {
			background-color: #f5f5f5;
			padding: 20px;
			border-radius: 5px;
			margin-top: 20px;
		}
		.detail-item {
			margin-bottom: 10px;
			padding: 10px;
			background-color: white;
			border-radius: 3px;
			box-shadow: 0 1px 3px rgba(0,0,0,0.1);
		}
		.detail-label {
			font-weight: bold;
			color: #555;
			margin-right: 10px;
		}
		.status-running {
			color: green;
		}
		.status-stopped {
			color: red;
		}
		.status-starting {
			color: orange;
		}
		.status-stopping {
			color: orange;
		}
		.status-exited {
			color: gray;
		}
		.status-fatal {
			color: darkred;
		}
		.btn-group {
			margin-top: 20px;
		}
		.btn {
			padding: 10px 20px;
			margin-right: 10px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
		}
		.btn-start {
			background-color: green;
			color: white;
		}
		.btn-stop {
			background-color: red;
			color: white;
		}
		.btn-restart {
			background-color: blue;
			color: white;
		}
		.btn-logs {
			background-color: purple;
			color: white;
		}
		.back-button {
			margin-top: 20px;
			padding: 10px 20px;
			background-color: #333;
			color: white;
			border: none;
			border-radius: 3px;
			cursor: pointer;
		}
		.back-button:hover {
			background-color: #555;
		}
		.footer {
			margin-top: 20px;
			font-size: 12px;
			color: #666;
		}
		.navbar {
			background-color: #333;
			color: white;
			padding: 10px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.navbar a {
			color: white;
			text-decoration: none;
			margin-right: 20px;
		}
		.navbar a:hover {
			text-decoration: underline;
		}
	</style>
</head>
<body>
	<div class="navbar">
		<a href="/">进程管理</a>
		<a href="/system">系统信息</a>
	</div>
	<h1>GoSupervisor - 进程详情</h1>
	<h2>进程: {{.Name}}</h2>
	<div class="detail-container">
		<div class="detail-item">
			<span class="detail-label">状态:</span>
			<span class="status-{{lower .State}}">{{.State}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">PID:</span>
			<span>{{if gt .PID 0}}{{.PID}}{{else}}-{{end}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">命令:</span>
			<span>{{.Config.Command}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">工作目录:</span>
			<span>{{.Config.Directory}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">启动时间:</span>
			<span>{{if not .StartTime.IsZero}}{{.StartTime.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">停止时间:</span>
			<span>{{if not .StopTime.IsZero}}{{.StopTime.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">退出码:</span>
			<span>{{if ne .ExitCode 0}}{{.ExitCode}}{{else}}-{{end}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">启动重试次数:</span>
			<span>{{.StartRetries}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">重启次数:</span>
			<span>{{.RestartCount}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">最后重启时间:</span>
			<span>{{if not .LastRestart.IsZero}}{{.LastRestart.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">CPU使用率:</span>
			<span>{{.CPUUsage}}%</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">内存使用:</span>
			<span>{{.MemoryUsage}} bytes</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">健康状态:</span>
			<span>{{if .Healthy}}健康{{else}}异常{{end}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">自动启动:</span>
			<span>{{.Config.AutoStart}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">自动重启:</span>
			<span>{{.Config.AutoRestart}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">启动超时:</span>
			<span>{{.Config.StartSecs}}秒</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">停止超时:</span>
			<span>{{.Config.StopSecs}}秒</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">停止信号:</span>
			<span>{{.Config.StopSignal}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">用户:</span>
			<span>{{.Config.User}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">优先级:</span>
			<span>{{.Config.Priority}}</span>
		</div>
		<div class="detail-item">
			<span class="detail-label">依赖进程:</span>
			<span>{{if gt (len .Config.DependsOn) 0}}{{.Config.DependsOn}}{{else}}无{{end}}</span>
		</div>
	</div>
	<div class="btn-group">
		{{if ne .State "RUNNING"}}
		<form method="post" action="/start" style="display:inline">
			<input type="hidden" name="name" value="{{.Name}}">
			<button class="btn btn-start" type="submit">启动</button>
		</form>
		{{end}}
		{{if eq .State "RUNNING"}}
		<form method="post" action="/stop" style="display:inline">
			<input type="hidden" name="name" value="{{.Name}}">
			<button class="btn btn-stop" type="submit">停止</button>
		</form>
		{{end}}
		<form method="post" action="/restart" style="display:inline">
			<input type="hidden" name="name" value="{{.Name}}">
			<button class="btn btn-restart" type="submit">重启</button>
		</form>
		<form method="get" action="/logs" style="display:inline">
			<input type="hidden" name="name" value="{{.Name}}">
			<button class="btn btn-logs" type="submit">查看日志</button>
		</form>
	</div>
	<button class="back-button" onclick="window.location.href='/'">返回首页</button>
	<div class="footer">
		<p>GoSupervisor - 进程管理工具</p>
	</div>
</body>
</html>`
