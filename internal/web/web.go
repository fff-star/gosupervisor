package web

import (
	"bufio"
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
	templates      *template.Template
}

func NewWebServer(processManager *process.ProcessManager, logDir string) (*WebServer, error) {
	// 创建模板并添加lower函数
	tmpl := template.New("index").Funcs(template.FuncMap{
		"lower": func(s interface{}) string {
			// 将任何类型转换为字符串，然后转换为小写
			return strings.ToLower(fmt.Sprintf("%v", s))
		},
	})

	// 解析HTML模板
	tmpl = template.Must(tmpl.Parse(indexTemplate))

	return &WebServer{
		processManager: processManager,
		logDir:         logDir,
		templates:      tmpl,
	}, nil
}

// validateProcessName rejects names with path separators or parent references.
func validateProcessName(name string) bool {
	return name != "" && !strings.Contains(name, "/") && !strings.Contains(name, "..") && !strings.Contains(name, "\\")
}

func (ws *WebServer) Start(addr string) error {
	// 注册路由
	http.HandleFunc("/", ws.handleIndex)
	http.HandleFunc("/start", ws.handleStart)
	http.HandleFunc("/stop", ws.handleStop)
	http.HandleFunc("/restart", ws.handleRestart)
	http.HandleFunc("/logs", ws.handleLogs)
	http.HandleFunc("/system", ws.handleSystemInfo)
	http.HandleFunc("/process", ws.handleProcessDetail)

	// 启动服务器
	fmt.Printf("Web服务器启动在 %s\n", addr)
	return http.ListenAndServe(addr, nil)
}

func (ws *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	// 获取所有进程快照（线程安全）
	snapshots := make([]process.Snapshot, 0, len(ws.processManager.Processes))
	for _, p := range ws.processManager.Processes {
		snapshots = append(snapshots, p.Snapshot())
	}

	// 渲染模板
	data := struct {
		Processes []process.Snapshot
		Time      string
	}{
		Processes: snapshots,
		Time:      time.Now().Format("2006-01-02 15:04:05"),
	}

	if err := ws.templates.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
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

	logPath := filepath.Join(ws.logDir, fmt.Sprintf("%s.log", processName))
	logContent, err := readTailLines(logPath, tailMaxLines, tailMaxBytes)
	if err != nil {
		logContent = []byte(fmt.Sprintf("无法读取日志文件: %v", err))
	}

	// 渲染日志页面
	tmpl := template.Must(template.New("logs").Parse(logsTemplate))
	data := struct {
		ProcessName string
		LogContent  string
	}{
		ProcessName: processName,
		LogContent:  string(logContent),
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	// 获取系统信息
	systemInfo := getSystemInfo()

	// 渲染系统信息页面
	tmpl := template.Must(template.New("system").Parse(systemInfoTemplate))
	if err := tmpl.Execute(w, systemInfo); err != nil {
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

	// 渲染进程详情页面
	tmpl := template.Must(template.New("process").Funcs(template.FuncMap{
		"lower": func(s interface{}) string {
			// 将任何类型转换为字符串，然后转换为小写
			return strings.ToLower(fmt.Sprintf("%v", s))
		},
	}).Parse(processDetailTemplate))
	snap := p.Snapshot()
	if err := tmpl.Execute(w, snap); err != nil {
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
				var total int64
				fmt.Sscanf(line, "MemTotal:\t%d", &total)
				info.MemoryTotal = uint64(total) * 1024 // 转换为字节
			} else if strings.HasPrefix(line, "MemAvailable:") {
				var avail int64
				fmt.Sscanf(line, "MemAvailable:\t%d", &avail)
				info.MemoryUsed = info.MemoryTotal - uint64(avail)*1024
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

	// 从 df 命令获取磁盘信息（针对根分区）
	cmd := exec.Command("df", "/")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 1 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 3 {
				var total, used int64
				fmt.Sscanf(fields[1], "%d", &total)
				fmt.Sscanf(fields[2], "%d", &used)
				info.DiskTotal = uint64(total) * 1024
				info.DiskUsed = uint64(used) * 1024
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
			for _, entry := range entries {
				// 检查是否是数字目录（进程ID）
				if _, err := fmt.Sscanf(entry, "%d", new(int)); err == nil {
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
	</table>
	<div class="footer">
		<p>最后更新: {{.Time}}</p>
		<p>GoSupervisor - 进程管理工具</p>
	</div>
	<script>
		// 自动刷新页面
		setInterval(function() {
			window.location.reload();
		}, 5000);
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
