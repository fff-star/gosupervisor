package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"html/template"
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
	corsOrigin     string
	rateLimiter    *RateLimiter
	certFile       string
	keyFile        string
	srv            *http.Server

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
	return NewWebServerFull(processManager, logDir, user, pass, apiAuth, "", 0)
}

// NewWebServerFull creates a WebServer with all options including CORS and rate limiting.
func NewWebServerFull(processManager *process.ProcessManager, logDir, user, pass string, apiAuth bool, corsOrigin string, rateLimitRPS int) (*WebServer, error) {
	return NewWebServerFullTLS(processManager, logDir, user, pass, apiAuth, corsOrigin, rateLimitRPS, "", "")
}

// NewWebServerFullTLS creates a WebServer with all options including TLS cert/key.
func NewWebServerFullTLS(processManager *process.ProcessManager, logDir, user, pass string, apiAuth bool, corsOrigin string, rateLimitRPS int, certFile, keyFile string) (*WebServer, error) {
	funcs := template.FuncMap{
		"lower": func(s interface{}) string {
			return strings.ToLower(fmt.Sprintf("%v", s))
		},
		"formatBytes": func(v interface{}) string {
			var b int64
			switch val := v.(type) {
			case uint64:
				b = int64(val)
			case int64:
				b = val
			case int:
				b = int64(val)
			default:
				return fmt.Sprintf("%v", v)
			}
			return formatBytes(b)
		},
	}

	indexTmpl := template.Must(template.New("index").Funcs(funcs).Parse(indexTemplate))
	logsTmpl := template.Must(template.New("logs").Parse(logsTemplate))
	systemInfoTmpl := template.Must(template.New("system").Parse(systemInfoTemplate))
	processDetailTmpl := template.Must(template.New("process").Funcs(funcs).Parse(processDetailTemplate))

	mux := http.NewServeMux()
	var rl *RateLimiter
	if rateLimitRPS > 0 {
		rl = NewRateLimiter(rateLimitRPS, rateLimitRPS*2)
	}
	ws := &WebServer{
		processManager:    processManager,
		logDir:            logDir,
		mux:               mux,
		authUser:          user,
		authPass:          pass,
		apiAuth:           apiAuth,
		corsOrigin:        corsOrigin,
		rateLimiter:       rl,
		certFile:          certFile,
		keyFile:           keyFile,
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
	mux.HandleFunc("/api/v1/system", ws.handleAPIV1System)
	mux.HandleFunc("/api/v1/config", ws.handleAPIV1Config)
	mux.HandleFunc("/api/v1/events", ws.handleAPIV1Events)
	mux.HandleFunc("/api/v1/events/stream", ws.handleAPIV1EventsStream)
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
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

func (ws *WebServer) Start(addr string) error {
	fmt.Printf("Web服务器启动在 %s\n", addr)
	handler := http.Handler(ws.mux)
	if ws.authUser != "" || ws.authPass != "" {
		handler = ws.authMiddleware(ws.mux)
		fmt.Println("Web界面已启用 HTTP Basic Auth 认证")
	}
	if ws.corsOrigin != "" {
		handler = ws.corsMiddleware(handler)
		fmt.Printf("Web界面已启用 CORS (origin=%s)\n", ws.corsOrigin)
	}
	if ws.rateLimiter != nil {
		handler = ws.rateLimiter.Middleware(handler)
		fmt.Println("Web界面已启用 API 速率限制")
	}
	ws.srv = &http.Server{Addr: addr, Handler: handler}
	if ws.certFile != "" && ws.keyFile != "" {
		fmt.Printf("Web服务器已启用 TLS (cert=%s, key=%s)\n", ws.certFile, ws.keyFile)
		return ws.srv.ListenAndServeTLS(ws.certFile, ws.keyFile)
	}
	return ws.srv.ListenAndServe()
}

// Stop cleans up resources held by the WebServer.
func (ws *WebServer) Stop() {
	if ws.rateLimiter != nil {
		ws.rateLimiter.Stop()
	}
	if ws.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ws.srv.Shutdown(ctx)
	}
}

// corsMiddleware adds CORS headers for /api/ routes and handles OPTIONS preflight.
func (ws *WebServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		origin := ws.corsOrigin
		if origin == "*" {
			origin = r.Header.Get("Origin")
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	if snapshots == nil {
		snapshots = []process.Snapshot{}
	}

	processesJSON, _ := json.Marshal(snapshots)

	data := struct {
		Processes     []process.Snapshot
		ProcessesJSON template.JS
		Time          string
	}{
		Processes:     snapshots,
		ProcessesJSON: template.JS(processesJSON),
		Time:          time.Now().Format("2006-01-02 15:04:05"),
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
	stateFilter := strings.ToUpper(r.URL.Query().Get("state"))
	groupFilter := r.URL.Query().Get("group")
	ws.processManager.RangeProcesses(func(name string, p *process.Process) {
		s := p.Snapshot()
		if stateFilter != "" && string(s.State) != stateFilter {
			return
		}
		if groupFilter != "" && s.Config.Group != groupFilter {
			return
		}
		snapshots = append(snapshots, s)
	})

	// Pagination via ?offset= and ?limit=
	var offset, limit int
	if v, err := fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset); v != 1 || err != nil {
		offset = 0
	}
	if v, err := fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit); v != 1 || err != nil {
		limit = 0
	}
	total := len(snapshots)
	if limit > 0 || offset > 0 {
		if offset >= len(snapshots) {
			snapshots = nil
		} else {
			end := offset + limit
			if limit <= 0 || end < offset || end > len(snapshots) {
				end = len(snapshots)
			}
			snapshots = snapshots[offset:end]
		}
	}
	w.Header().Set("X-Total-Count", fmt.Sprintf("%d", total))
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
			fmt.Printf("API: start process failed: %v\n", err)
			jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Failed to start process"})
			return
		}
		jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process started"})
	case action == "stop" && r.Method == http.MethodPost:
		if err := p.Stop(); err != nil {
			fmt.Printf("API: stop process failed: %v\n", err)
			jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Failed to stop process"})
			return
		}
		jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process stopped"})
	case action == "restart" && r.Method == http.MethodPost:
		if err := p.Restart(); err != nil {
			fmt.Printf("API: restart process failed: %v\n", err)
			jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Failed to restart process"})
			return
		}
		jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process restarted"})
	case action == "logs" && r.Method == http.MethodGet:
		ws.handleProcessLogsStream(w, r, name)
	case action == "logs/tail" && r.Method == http.MethodGet:
		ws.handleProcessLogsTail(w, r, name)
	case action == "logs/stderr" && r.Method == http.MethodGet:
		ws.handleProcessLogsTailStream(w, r, name, "stderr")
	case action == "resources" && r.Method == http.MethodGet:
		ws.handleProcessResources(w, r, p)
	case action == "reload" && r.Method == http.MethodPost:
		ws.handleProcessReload(w, r, p)
	case action == "signal" && r.Method == http.MethodPost:
		ws.handleProcessSignal(w, r, p)
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

	// Use the process's configured log path, falling back to default.
	// Support ?stream=stderr for stderr log access.
	s := p.Snapshot()
	stream := r.URL.Query().Get("stream")
	logPath := s.Config.StdoutLogFile
	if stream == "stderr" {
		logPath = s.Config.StderrLogFile
	}
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
		Stream      string
	}{
		ProcessName: processName,
		LogContent:  string(logContent),
		Stream:      stream,
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
				payload := map[string]string{"text": string(buf[:n])}
				jsonData, err := json.Marshal(payload)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", jsonData)
				flusher.Flush()
			}
		}
	}
}

func (ws *WebServer) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	systemInfo := getSystemInfo(ws.processManager)
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
	type detailData struct {
		process.Snapshot
		Uptime string
	}
	data := detailData{Snapshot: snap}
	if snap.State == process.StateRunning && !snap.StartTime.IsZero() {
		d := time.Since(snap.StartTime)
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		s := int(d.Seconds()) % 60
		if h > 0 {
			data.Uptime = fmt.Sprintf("%dh %dm", h, m)
		} else if m > 0 {
			data.Uptime = fmt.Sprintf("%dm %ds", m, s)
		} else {
			data.Uptime = fmt.Sprintf("%ds", s)
		}
	} else {
		data.Uptime = "-"
	}
	if err := ws.processDetailTmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("渲染模板失败: %v", err), http.StatusInternalServerError)
	}
}

// --- new API v1 handlers ---

func (ws *WebServer) handleAPIV1System(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, apiV1Status{Status: "error", Message: "Method Not Allowed"})
		return
	}
	si := getSystemInfo(ws.processManager)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"system": si,
	})
}

func (ws *WebServer) handleAPIV1Config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, apiV1Status{Status: "error", Message: "Method Not Allowed"})
		return
	}
	configs := make([]map[string]interface{}, 0)
	ws.processManager.RangeProcesses(func(name string, p *process.Process) {
		s := p.Snapshot()
		configs = append(configs, map[string]interface{}{
			"name":      s.Config.Name,
			"command":   s.Config.Command,
			"directory": s.Config.Directory,
			"autostart": s.Config.AutoStart,
			"autorestart": s.Config.AutoRestart,
			"startsecs": s.Config.StartSecs,
			"startretries": s.Config.StartRetries,
			"stopsecs": s.Config.StopSecs,
			"stopsignal": s.Config.StopSignal,
			"user": s.Config.User,
			"priority": s.Config.Priority,
			"group": s.Config.Group,
			"dependson": s.Config.DependsOn,
			"healthcheckurl": s.Config.HealthCheckURL,
		})
	})
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"processes": configs,
	})
}

func (ws *WebServer) handleAPIV1Events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, apiV1Status{Status: "error", Message: "Method Not Allowed"})
		return
	}
	var limit int
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit <= 0 {
		limit = 100
	}
	events := process.GlobalEventBuffer.Snapshot(limit)
	if events == nil {
		events = []process.Event{}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"events": events,
	})
}

// handleAPIV1EventsStream streams process events via Server-Sent Events.
func (ws *WebServer) handleAPIV1EventsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := globalSSEBroker.subscribe()
	defer globalSSEBroker.unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			w.Write(data)
			flusher.Flush()
		}
	}
}

func (ws *WebServer) handleProcessLogsTail(w http.ResponseWriter, r *http.Request, name string) {
	var maxLines, maxBytes int64
	maxLines = int64(tailMaxLines)
	maxBytes = int64(tailMaxBytes)
	if v := r.URL.Query().Get("lines"); v != "" {
		fmt.Sscanf(v, "%d", &maxLines)
	}
	if v := r.URL.Query().Get("maxBytes"); v != "" {
		fmt.Sscanf(v, "%d", &maxBytes)
	}

	p := ws.processManager.GetProcess(name)
	logPath := ""
	if p != nil {
		s := p.Snapshot()
		logPath = s.Config.StdoutLogFile
	}
	if logPath == "" {
		logPath = filepath.Join(ws.logDir, fmt.Sprintf("%s.log", name))
	}

	content, err := readTailLines(logPath, int(maxLines), maxBytes)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Internal error"})
		return
	}

	// Get file size
	var fileSize int64
	if fi, err := os.Stat(logPath); err == nil {
		fileSize = fi.Size()
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"name":     name,
		"content":  string(content),
		"fileSize": fileSize,
	})
}

func (ws *WebServer) handleProcessSignal(w http.ResponseWriter, r *http.Request, p *process.Process) {
	var body struct {
		Signal string `json:"signal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, apiV1Status{Status: "error", Message: "Invalid JSON body"})
		return
	}

	sig, ok := process.ParseSignal(body.Signal)
	if !ok {
		jsonResponse(w, http.StatusBadRequest, apiV1Status{Status: "error", Message: "Unknown signal: " + body.Signal})
		return
	}

	if err := p.Signal(sig); err != nil {
		jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Internal error"})
		return
	}
	jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Signal sent"})
}

func (ws *WebServer) handleProcessLogsTailStream(w http.ResponseWriter, r *http.Request, name string, stream string) {
	var maxLines, maxBytes int64
	maxLines = int64(tailMaxLines)
	maxBytes = int64(tailMaxBytes)
	if v := r.URL.Query().Get("lines"); v != "" {
		fmt.Sscanf(v, "%d", &maxLines)
	}
	if v := r.URL.Query().Get("maxBytes"); v != "" {
		fmt.Sscanf(v, "%d", &maxBytes)
	}

	p := ws.processManager.GetProcess(name)
	logPath := ""
	if p != nil {
		s := p.Snapshot()
		if stream == "stderr" {
			logPath = s.Config.StderrLogFile
		} else {
			logPath = s.Config.StdoutLogFile
		}
	}
	if logPath == "" {
		logPath = filepath.Join(ws.logDir, fmt.Sprintf("%s.log", name))
	}

	content, err := readTailLines(logPath, int(maxLines), maxBytes)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Internal error"})
		return
	}

	var fileSize int64
	if fi, err := os.Stat(logPath); err == nil {
		fileSize = fi.Size()
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"name":     name,
		"stream":   stream,
		"content":  string(content),
		"fileSize": fileSize,
	})
}

func (ws *WebServer) handleProcessReload(w http.ResponseWriter, r *http.Request, p *process.Process) {
	if err := p.Signal(syscall.SIGHUP); err != nil {
		jsonResponse(w, http.StatusInternalServerError, apiV1Status{Status: "error", Message: "Internal error"})
		return
	}
	jsonResponse(w, http.StatusOK, apiV1Status{Status: "ok", Message: "Process reload signal sent"})
}

func (ws *WebServer) handleProcessResources(w http.ResponseWriter, r *http.Request, p *process.Process) {
	if p.ResourceHistory == nil {
		jsonResponse(w, http.StatusNotFound, apiV1Status{Status: "error", Message: "Resource history not available"})
		return
	}
	minutes := 5
	if m := r.URL.Query().Get("minutes"); m != "" {
		if parsed, err := fmt.Sscanf(m, "%d", &minutes); err != nil || parsed != 1 {
			jsonResponse(w, http.StatusBadRequest, apiV1Status{Status: "error", Message: "Invalid minutes parameter"})
			return
		}
	}
	var since time.Duration
	if minutes > 0 {
		since = time.Duration(minutes) * time.Minute
	}
	samples := p.ResourceHistory.Snapshot(since)
	if samples == nil {
		samples = []process.ResourceSample{}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"name":    p.Name,
		"samples": samples,
	})
}

// SystemInfo 系统信息结构体
type SystemInfo struct {
	OS                  string
	Arch                string
	Hostname            string
	CPUCount            int
	MemoryTotal         string
	MemoryUsed          string
	DiskTotal           string
	DiskUsed            string
	Uptime              string
	GoVersion           string
	ProcessCount        int
	Version             string
	DaemonPID           int
	DaemonUptime        string
	ManagedProcessCount int
	TotalLogSize        string
}

// getSystemInfo 获取系统信息
func getSystemInfo(pm *process.ProcessManager) *SystemInfo {
	hostname, _ := os.Hostname()
	info := &SystemInfo{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Hostname:     hostname,
		CPUCount:     runtime.NumCPU(),
		GoVersion:    runtime.Version(),
		ProcessCount: countProcesses(),
		Version:      "1.0.0",
		DaemonPID:    os.Getpid(),
	}

	// Supervisor daemon uptime: subtract process starttime from system uptime.
	// /proc/self/stat field 22 (0-indexed rest[19]) = starttime in clock ticks since boot.
	// /proc/uptime first field = system uptime in seconds.
	var daemonUptimeSecs int64
	if statData, err := os.ReadFile("/proc/self/stat"); err == nil {
		content := string(statData)
		idx := strings.LastIndex(content, ")")
		if idx >= 0 && idx+2 < len(content) {
			rest := strings.Fields(content[idx+2:])
			if len(rest) >= 20 {
				var startTicks uint64
				fmt.Sscanf(rest[19], "%d", &startTicks)
				processStartSecs := int64(startTicks) / 100 // USER_HZ=100
				if uptimeData, err := os.ReadFile("/proc/uptime"); err == nil {
					var sysUptime float64
					fmt.Sscanf(string(uptimeData), "%f", &sysUptime)
					daemonUptimeSecs = int64(sysUptime) - processStartSecs
				}
			}
		}
	}
	if daemonUptimeSecs > 0 {
		h := int(daemonUptimeSecs) / 3600
		m := (int(daemonUptimeSecs) % 3600) / 60
		s := int(daemonUptimeSecs) % 60
		if h > 0 {
			info.DaemonUptime = fmt.Sprintf("%dh %dm", h, m)
		} else if m > 0 {
			info.DaemonUptime = fmt.Sprintf("%dm %ds", m, s)
		} else {
			info.DaemonUptime = fmt.Sprintf("%ds", s)
		}
	}
	if info.DaemonUptime == "" {
		info.DaemonUptime = "unknown"
	}

	// Managed process count and total log disk usage
	if pm != nil {
		info.ManagedProcessCount = pm.Len()
		var logSize int64
		pm.RangeProcesses(func(name string, p *process.Process) {
			s := p.Snapshot()
			cfg := s.Config
			for _, path := range []string{cfg.StdoutLogFile, cfg.StderrLogFile} {
				if path == "" {
					continue
				}
				if fi, err := os.Stat(path); err == nil {
					logSize += fi.Size()
				}
			}
		})
		info.TotalLogSize = formatBytes(logSize)
	}

	// 从 /proc/meminfo 读取内存信息
	var memTotal, memUsed uint64
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
					memTotal = uint64(total) * 1024
				}
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					var avail int64
					fmt.Sscanf(fields[1], "%d", &avail)
					memUsed = memTotal - uint64(avail)*1024
				}
			}
		}
	}
	info.MemoryTotal = formatBytes(int64(memTotal))
	info.MemoryUsed = formatBytes(int64(memUsed))

	// 从 /proc/uptime 读取系统运行时间
	if uptime, err := os.Open("/proc/uptime"); err == nil {
		defer uptime.Close()
		var uptimeSecs float64
		fmt.Fscanf(uptime, "%f", &uptimeSecs)
		hours := int(uptimeSecs) / 3600
		mins := (int(uptimeSecs) % 3600) / 60
		info.Uptime = fmt.Sprintf("%dh %dm", hours, mins)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		info.DiskTotal = formatBytes(int64(total))
		info.DiskUsed = formatBytes(int64(total - free))
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

	// If we didn't start at BOF, skip the first partial line.
	if readSize > 0 && readSize < fileSize && len(buf) > 0 {
		if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
			buf = buf[idx+1:]
		}
	}

	// Keep at most maxLines from the end.
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

// formatBytes converts a byte count to a human-readable string.
func formatBytes(bytes int64) string {
	switch {
	case bytes >= 1073741824:
		return fmt.Sprintf("%.1f GB", float64(bytes)/1073741824)
	case bytes >= 1048576:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1048576)
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
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
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>GoSupervisor</title>
<style>
:root {
--bg: #0a0e14;
--bg-card: #12171f;
--bg-row: #141b24;
--border: #1e2a38;
--text: #c8ccd4;
--text-dim: #6c7380;
--accent: #39bae6;
--green: #7fd962;
--red: #f26d78;
--amber: #ffb454;
--purple: #d2a6ff;
--orange: #ff8f40;
--blue: #59c2ff;
--font: -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
--mono: "Fira Code","JetBrains Mono","Cascadia Code",monospace;
}
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:var(--font);line-height:1.5;min-height:100vh}
body::before{content:"";position:fixed;inset:0;background:
radial-gradient(ellipse at 20% 50%,rgba(57,186,230,0.03) 0%,transparent 50%),
radial-gradient(ellipse at 80% 20%,rgba(210,166,255,0.02) 0%,transparent 40%);
pointer-events:none;z-index:0}
.header{position:sticky;top:0;z-index:10;background:var(--bg-card);border-bottom:1px solid var(--border);
-webkit-backdrop-filter:blur(12px);backdrop-filter:blur(12px)}
.header-inner{max-width:1400px;margin:0 auto;padding:12px 24px;display:flex;align-items:center;justify-content:space-between}
.logo{display:flex;align-items:center;gap:10px}
.logo-icon{width:32px;height:32px;background:var(--accent);border-radius:8px;display:flex;align-items:center;justify-content:center;
font-size:16px;font-weight:700;color:#0a0e14}
.logo-text{font-size:18px;font-weight:600;letter-spacing:-0.3px}
.nav{display:flex;gap:4px}
.nav a{color:var(--text-dim);text-decoration:none;padding:6px 14px;border-radius:6px;font-size:14px;
transition:all 0.15s}
.nav a:hover,.nav a.active{color:var(--text);background:rgba(255,255,255,0.05)}
.stats{max-width:1400px;margin:20px auto 0;padding:0 24px;display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px}
.stat-card{background:var(--bg-card);border:1px solid var(--border);border-radius:10px;padding:14px 16px;
transition:border-color 0.2s}
.stat-card:hover{border-color:rgba(255,255,255,0.1)}
.stat-value{font-size:28px;font-weight:700;font-family:var(--mono);line-height:1}
.stat-label{font-size:12px;color:var(--text-dim);margin-top:4px;text-transform:uppercase;letter-spacing:0.5px}
.stat-card.running .stat-value{color:var(--green)}
.stat-card.stopped .stat-value{color:var(--text-dim)}
.stat-card.fatal .stat-value{color:var(--red)}
.toolbar{max-width:1400px;margin:16px auto 0;padding:0 24px;display:flex;gap:12px;align-items:center;flex-wrap:wrap}
.toolbar select,.toolbar input{background:var(--bg-card);border:1px solid var(--border);border-radius:6px;color:var(--text);
padding:6px 12px;font-size:13px;font-family:var(--font);outline:none}
.toolbar select:focus,.toolbar input:focus{border-color:var(--accent)}
.toolbar input{width:140px}
.toolbar input::placeholder{color:var(--text-dim)}
.table-wrap{max-width:1400px;margin:12px auto 40px;padding:0 24px;overflow-x:auto}
table{width:100%;border-collapse:separate;border-spacing:0}
thead{background:var(--bg-card)}
th{background:var(--bg-card);color:var(--text-dim);font-size:11px;font-weight:600;text-transform:uppercase;
letter-spacing:0.8px;padding:10px 14px;text-align:left;border-bottom:2px solid var(--border);
white-space:nowrap}
td{padding:10px 14px;font-size:13px;border-bottom:1px solid var(--border);white-space:nowrap}
tbody tr{background:var(--bg-row);transition:background 0.1s}
tbody tr:hover{background:rgba(57,186,230,0.04)}
.status{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:20px;
font-size:11px;font-weight:600;letter-spacing:0.3px}
.status::before{content:"";width:6px;height:6px;border-radius:50%}
.status-RUNNING{background:rgba(127,217,98,0.1);color:var(--green)}
.status-RUNNING::before{background:var(--green);animation:pulse 2s ease-in-out infinite}
.status-STOPPED{background:rgba(108,115,128,0.1);color:var(--text-dim)}
.status-STOPPED::before{background:var(--text-dim)}
.status-STARTING{background:rgba(255,180,84,0.1);color:var(--amber)}
.status-STARTING::before{background:var(--amber);animation:pulse 0.8s ease-in-out infinite}
.status-STOPPING{background:rgba(255,143,64,0.1);color:var(--orange)}
.status-STOPPING::before{background:var(--orange);animation:pulse 0.8s ease-in-out infinite}
.status-EXITED{background:rgba(108,115,128,0.06);color:var(--text-dim)}
.status-EXITED::before{background:var(--text-dim)}
.status-FATAL{background:rgba(242,109,120,0.12);color:var(--red)}
.status-FATAL::before{background:var(--red)}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:0.4}}
.btn{display:inline-flex;align-items:center;gap:4px;padding:5px 10px;margin-right:6px;border:none;border-radius:5px;
font-size:11px;font-weight:600;cursor:pointer;transition:all 0.12s;font-family:var(--font);
letter-spacing:0.2px}
.btn:hover{transform:translateY(-1px);filter:brightness(1.15)}
.btn:active{transform:translateY(0)}
.btn-start{background:rgba(127,217,98,0.15);color:var(--green)}
.btn-start:hover{background:rgba(127,217,98,0.25)}
.btn-stop{background:rgba(242,109,120,0.15);color:var(--red)}
.btn-stop:hover{background:rgba(242,109,120,0.25)}
.btn-restart{background:rgba(89,194,255,0.15);color:var(--blue)}
.btn-restart:hover{background:rgba(89,194,255,0.25)}
.btn-logs{background:rgba(210,166,255,0.15);color:var(--purple)}
.btn-logs:hover{background:rgba(210,166,255,0.25)}
.btn-detail{background:rgba(255,143,64,0.12);color:var(--orange)}
.btn-detail:hover{background:rgba(255,143,64,0.22)}
.cell-pid{font-family:var(--mono);font-size:12px;color:var(--accent)}
.cell-time{font-size:12px;color:var(--text-dim)}
.cell-ec{font-family:var(--mono);font-size:12px}
.footer{max-width:1400px;margin:0 auto;padding:16px 24px 32px;display:flex;justify-content:space-between;
font-size:11px;color:var(--text-dim);border-top:1px solid var(--border)}
.live-dot{display:inline-block;width:5px;height:5px;background:var(--green);border-radius:50%;
margin-right:4px;animation:pulse 1.5s ease-in-out infinite}
.empty-state{text-align:center;padding:60px 20px;color:var(--text-dim)}
.empty-state p{font-size:14px}
</style>
</head>
<body>
<div class="header">
<div class="header-inner">
<div class="logo">
<div class="logo-icon">GS</div>
<span class="logo-text">GoSupervisor</span>
</div>
<nav class="nav">
<a href="/" class="active">进程</a>
<a href="/system">系统</a>
</nav>
</div>
</div>

<div class="stats" id="stats">
<div class="stat-card"><div class="stat-value" id="stat-total">-</div><div class="stat-label">总数</div></div>
<div class="stat-card running"><div class="stat-value" id="stat-running">-</div><div class="stat-label">运行中</div></div>
<div class="stat-card stopped"><div class="stat-value" id="stat-stopped">-</div><div class="stat-label">已停止</div></div>
<div class="stat-card fatal"><div class="stat-value" id="stat-fatal">-</div><div class="stat-label">Fatal</div></div>
</div>

<div class="toolbar">
<select id="filter-state">
<option value="">全部状态</option>
<option value="RUNNING">RUNNING</option>
<option value="STOPPED">STOPPED</option>
<option value="STARTING">STARTING</option>
<option value="STOPPING">STOPPING</option>
<option value="EXITED">EXITED</option>
<option value="FATAL">FATAL</option>
</select>
<input type="text" id="filter-name" placeholder="搜索进程…">
</div>

<div class="table-wrap">
<table>
<thead><tr>
<th>名称</th><th>状态</th><th>PID</th><th>运行时间</th><th>启动时间</th><th>停止时间</th><th>退出码</th><th>重启</th><th>操作</th>
</tr></thead>
<tbody id="tbody"></tbody>
</table>
<div id="empty" class="empty-state" style="display:none"><p>没有匹配的进程</p></div>
</div>

<div class="footer">
<span><span class="live-dot"></span>实时更新中</span>
<span id="last-updated">{{.Time}}</span>
</div>

<script>
(function(){
var processes = {{.ProcessesJSON}};
var tbody = document.getElementById('tbody');
var empty = document.getElementById('empty');

function esc(s){var d=document.createElement('div');d.textContent=s;return d.innerHTML}
function fmt(ts){if(!ts)return'-';var d=new Date(ts);if(d.getFullYear()<=1)return'-';
return d.getFullYear()+'-'+String(d.getMonth()+1).padStart(2,'0')+'-'+String(d.getDate()).padStart(2,'0')+' '
+String(d.getHours()).padStart(2,'0')+':'+String(d.getMinutes()).padStart(2,'0')+':'+String(d.getSeconds()).padStart(2,'0')}
function uptime(ts,st){if(!ts||st!=='RUNNING')return'-';var d=new Date(ts);
if(d.getFullYear()<=1)return'-';var diff=Math.floor((Date.now()-d.getTime())/1000);
if(diff<0)return'-';var h=Math.floor(diff/3600),m=Math.floor((diff%3600)/60),s=diff%60;
return h>0?h+'h '+m+'m':m>0?m+'m '+s+'s':s+'s'}

function rowHTML(p){
var nm=esc(p.Name),st=(p.State||'').toUpperCase(),ls=st.toLowerCase();
var pid=p.PID>0?'<span class="cell-pid">'+p.PID+'</span>':'-';
var up=uptime(p.StartTime,p.State);
var start=fmt(p.StartTime),stop=fmt(p.StopTime);
var ec=p.ExitCode!==0?p.ExitCode:'-';
var sc='<span class="status status-'+ls+'">'+esc(st)+'</span>';
var btns='';
if(p.State!=='RUNNING')btns+='<form method="post" action="/start" style="display:inline"><input type="hidden" name="name" value="'+nm+'"><button class="btn btn-start">启动</button></form>';
if(p.State==='RUNNING')btns+='<form method="post" action="/stop" style="display:inline"><input type="hidden" name="name" value="'+nm+'"><button class="btn btn-stop">停止</button></form>';
btns+='<form method="post" action="/restart" style="display:inline"><input type="hidden" name="name" value="'+nm+'"><button class="btn btn-restart">重启</button></form>';
btns+='<form method="get" action="/logs" style="display:inline"><input type="hidden" name="name" value="'+nm+'"><button class="btn btn-logs">日志</button></form>';
btns+='<form method="get" action="/process" style="display:inline"><input type="hidden" name="name" value="'+nm+'"><button class="btn btn-detail">详情</button></form>';
return '<tr><td>'+nm+'</td><td>'+sc+'</td><td>'+pid+'</td><td class="cell-time">'+up+'</td><td class="cell-time">'+start+'</td><td class="cell-time">'+stop+'</td><td class="cell-ec">'+ec+'</td><td>'+p.StartRetries+'</td><td>'+btns+'</td></tr>'
}

function updateStats(arr){
var r=0,s=0,f=0;
for(var i=0;i<arr.length;i++){
if(arr[i].State==='RUNNING')r++;else if(arr[i].State==='STOPPED')s++;else if(arr[i].State==='FATAL')f++
}
document.getElementById('stat-total').textContent=arr.length;
document.getElementById('stat-running').textContent=r;
document.getElementById('stat-stopped').textContent=s;
document.getElementById('stat-fatal').textContent=f
}

function render(){
var state=document.getElementById('filter-state').value;
var name=document.getElementById('filter-name').value.toLowerCase();
var h='';var count=0;
for(var i=0;i<processes.length;i++){
var p=processes[i];
if(state&&p.State!==state)continue;
if(name&&p.Name.toLowerCase().indexOf(name)===-1)continue;
h+=rowHTML(p);count++
}
tbody.innerHTML=h;
empty.style.display=count===0?'block':'none';
updateStats(processes)
}

render();
document.getElementById('filter-state').onchange=render;
document.getElementById('filter-name').oninput=render;

// Live updates via SSE
var es=new EventSource('/api/v1/events/stream');
es.onmessage=function(e){
try{
var ev=JSON.parse(e.data);
var found=false;
for(var i=0;i<processes.length;i++){
if(processes[i].Name===ev.name){
processes[i].State=ev.type==='start'?'RUNNING':ev.type==='stop'?'STOPPED':ev.type==='exit'?'EXITED':ev.type==='fatal'?'FATAL':processes[i].State;
if(ev.pid>0)processes[i].PID=ev.pid;
if(ev.exitCode)processes[i].ExitCode=ev.exitCode;
found=true;break
}
}
if(!found){
// New process, reload full list
fetch('/api/processes').then(function(r){return r.json()}).then(function(arr){
processes=arr;render()
}).catch(function(){})
}else{render()}
document.getElementById('last-updated').textContent=fmt(new Date().toISOString())
}catch(_){}
};
es.onerror=function(){setTimeout(function(){es.close();es=new EventSource('/api/v1/events/stream')},3000)};
// Keep the footer clock ticking even without SSE events
setInterval(function(){document.getElementById('last-updated').textContent=fmt(new Date().toISOString())},1000);
})();
</script>
</body>
</html>`

const logsTemplate = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.ProcessName}} — GoSupervisor</title>
<style>
:root{--bg:#0a0e14;--bg-card:#12171f;--border:#1e2a38;--text:#c8ccd4;--text-dim:#6c7380;--accent:#39bae6;--green:#7fd962;--red:#f26d78;--font:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;--mono:"Fira Code","JetBrains Mono","Cascadia Code",monospace}
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:var(--font);line-height:1.5;min-height:100vh}
.header{background:var(--bg-card);border-bottom:1px solid var(--border);-webkit-backdrop-filter:blur(12px);backdrop-filter:blur(12px)}
.header-inner{max-width:1400px;margin:0 auto;padding:12px 24px;display:flex;align-items:center;justify-content:space-between}
.logo{display:flex;align-items:center;gap:10px}
.logo-icon{width:32px;height:32px;background:var(--accent);border-radius:8px;display:flex;align-items:center;justify-content:center;font-size:16px;font-weight:700;color:#0a0e14}
.logo-text{font-size:18px;font-weight:600;letter-spacing:-0.3px}
.back{color:var(--text-dim);text-decoration:none;padding:6px 14px;border-radius:6px;font-size:14px;transition:all 0.15s}
.back:hover{color:var(--text);background:rgba(255,255,255,0.05)}
main{max-width:1400px;margin:0 auto;padding:24px}
.proc-name{display:flex;align-items:center;gap:10px;margin-bottom:20px}
.proc-name h2{font-size:20px;font-weight:600}
.proc-name .tag{font-size:11px;padding:3px 10px;border-radius:20px;background:rgba(57,186,230,0.15);color:var(--accent);font-weight:600;letter-spacing:0.3px}
.terminal{background:#0d1117;border:1px solid var(--border);border-radius:12px;overflow:hidden;box-shadow:0 4px 24px rgba(0,0,0,0.3)}
.terminal-bar{background:var(--bg-card);padding:8px 16px;display:flex;align-items:center;border-bottom:1px solid var(--border)}
.terminal-title{font-size:11px;color:var(--text-dim);font-family:var(--mono)}
.terminal-body{padding:20px;font-family:var(--mono);font-size:13px;line-height:1.7;white-space:pre-wrap;word-break:break-all;
color:#9ed072;max-height:70vh;overflow-y:auto}
.terminal-body::-webkit-scrollbar{width:6px}
.terminal-body::-webkit-scrollbar-track{background:transparent}
.terminal-body::-webkit-scrollbar-thumb{background:var(--border);border-radius:3px}
.empty-log{color:var(--text-dim);text-align:center;padding:40px;font-style:italic}
</style>
</head>
<body>
<div class="header"><div class="header-inner">
<div class="logo"><div class="logo-icon">GS</div><span class="logo-text">GoSupervisor</span></div>
<a href="/" class="back">&larr; 返回</a>
</div></div>
<main>
<div class="proc-name"><h2>{{.ProcessName}}</h2><span class="tag">LOG</span></div>
<div class="terminal">
<div class="terminal-bar"><span class="terminal-title">{{.ProcessName}}.log</span></div>
<div class="terminal-body">{{if .LogContent}}{{.LogContent}}{{else}}<div class="empty-log">暂无日志</div>{{end}}</div>
</div>
</main>
<script>
(function(){var b=document.querySelector('.terminal-body');b.scrollTop=b.scrollHeight})();
var procName="{{.ProcessName}}",count=0;
setInterval(function(){
fetch('/api/v1/processes/'+encodeURIComponent(procName)+'/logs/tail?lines=200')
.then(function(r){return r.json()}).then(function(d){
var b=document.querySelector('.terminal-body');
if(d.content&&d.content!==b.textContent){b.textContent=d.content;b.scrollTop=b.scrollHeight}
}).catch(function(){});
},2000);
</script>
</body>
</html>`

const systemInfoTemplate = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>系统信息 — GoSupervisor</title>
<style>
:root{--bg:#0a0e14;--bg-card:#12171f;--border:#1e2a38;--text:#c8ccd4;--text-dim:#6c7380;--accent:#39bae6;--green:#7fd962;--red:#f26d78;--purple:#d2a6ff;--amber:#ffb454;--font:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;--mono:"Fira Code","JetBrains Mono","Cascadia Code",monospace}
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:var(--font);line-height:1.5;min-height:100vh}
body::before{content:"";position:fixed;inset:0;background:radial-gradient(ellipse at 20% 50%,rgba(57,186,230,0.03) 0%,transparent 50%),radial-gradient(ellipse at 80% 20%,rgba(210,166,255,0.02) 0%,transparent 40%);pointer-events:none;z-index:0}
.header{position:sticky;top:0;z-index:10;background:var(--bg-card);border-bottom:1px solid var(--border);-webkit-backdrop-filter:blur(12px);backdrop-filter:blur(12px)}
.header-inner{max-width:1200px;margin:0 auto;padding:12px 24px;display:flex;align-items:center;justify-content:space-between}
.logo{display:flex;align-items:center;gap:10px}
.logo-icon{width:32px;height:32px;background:var(--accent);border-radius:8px;display:flex;align-items:center;justify-content:center;font-size:16px;font-weight:700;color:#0a0e14}
.logo-text{font-size:18px;font-weight:600;letter-spacing:-0.3px}
.nav{display:flex;gap:4px}
.nav a{color:var(--text-dim);text-decoration:none;padding:6px 14px;border-radius:6px;font-size:14px;transition:all 0.15s}
.nav a:hover,.nav a.active{color:var(--text);background:rgba(255,255,255,0.05)}
main{max-width:1200px;margin:0 auto;padding:24px;position:relative;z-index:1}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:16px}
.card{background:var(--bg-card);border:1px solid var(--border);border-radius:12px;padding:16px 20px;transition:border-color 0.2s}
.card:hover{border-color:rgba(255,255,255,0.1)}
.card-label{font-size:11px;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.8px;margin-bottom:6px;font-weight:600}
.card-value{font-size:17px;font-weight:500;font-family:var(--mono);word-break:break-all}
.card-value.big{font-size:24px;font-weight:700}
.section-title{font-size:13px;color:var(--text-dim);text-transform:uppercase;letter-spacing:1px;margin:32px 0 16px;font-weight:600}
.section-title:first-child{margin-top:0}
.accent{color:var(--accent)}.green{color:var(--green)}.red{color:var(--red)}.purple{color:var(--purple)}.amber{color:var(--amber)}
</style>
</head>
<body>
<div class="header"><div class="header-inner">
<div class="logo"><div class="logo-icon">GS</div><span class="logo-text">GoSupervisor</span></div>
<nav class="nav"><a href="/">进程</a><a href="/system" class="active">系统</a></nav>
</div></div>
<main>
<div class="section-title">系统</div>
<div class="grid">
<div class="card"><div class="card-label">操作系统</div><div class="card-value">{{.OS}} {{.Arch}}</div></div>
<div class="card"><div class="card-label">主机名</div><div class="card-value">{{.Hostname}}</div></div>
<div class="card"><div class="card-label">CPU 核心数</div><div class="card-value big">{{.CPUCount}}</div></div>
<div class="card"><div class="card-label">内存总量</div><div class="card-value" id="mem-total">{{.MemoryTotal}}</div></div>
<div class="card"><div class="card-label">内存已用</div><div class="card-value" id="mem-used">{{.MemoryUsed}}</div></div>
<div class="card"><div class="card-label">磁盘总量</div><div class="card-value" id="disk-total">{{.DiskTotal}}</div></div>
<div class="card"><div class="card-label">磁盘已用</div><div class="card-value" id="disk-used">{{.DiskUsed}}</div></div>
<div class="card"><div class="card-label">系统运行时间</div><div class="card-value" id="sys-uptime">{{.Uptime}}</div></div>
<div class="card"><div class="card-label">Go 版本</div><div class="card-value">{{.GoVersion}}</div></div>
<div class="card"><div class="card-label">系统进程总数</div><div class="card-value big" id="proc-count">{{.ProcessCount}}</div></div>
</div>
<div class="section-title">Supervisor</div>
<div class="grid">
<div class="card"><div class="card-label">版本</div><div class="card-value">{{.Version}}</div></div>
<div class="card"><div class="card-label">PID</div><div class="card-value accent">{{.DaemonPID}}</div></div>
<div class="card"><div class="card-label">运行时间</div><div class="card-value" id="daemon-uptime">{{.DaemonUptime}}</div></div>
<div class="card"><div class="card-label">托管进程数</div><div class="card-value big green" id="managed-count">{{.ManagedProcessCount}}</div></div>
<div class="card"><div class="card-label">日志磁盘用量</div><div class="card-value" id="log-size">{{.TotalLogSize}}</div></div>
</div>
<div style="text-align:center;margin-top:24px;font-size:11px;color:var(--text-dim)">Auto-refresh: <span id="tick" style="color:var(--green)">0s</span> ago</div>
</main>
<script>
(function(){
var lastUpdate=Date.now();
function refresh(){
fetch('/api/v1/system').then(function(r){return r.json()}).then(function(d){
var s=d.system;
document.getElementById('mem-total').textContent=s.MemoryTotal||'-';
document.getElementById('mem-used').textContent=s.MemoryUsed||'-';
document.getElementById('disk-total').textContent=s.DiskTotal||'-';
document.getElementById('disk-used').textContent=s.DiskUsed||'-';
document.getElementById('sys-uptime').textContent=s.Uptime||'-';
document.getElementById('proc-count').textContent=s.ProcessCount||'-';
document.getElementById('daemon-uptime').textContent=s.DaemonUptime||'-';
document.getElementById('managed-count').textContent=s.ManagedProcessCount||'-';
document.getElementById('log-size').textContent=s.TotalLogSize||'-';
lastUpdate=Date.now();
}).catch(function(e){
document.getElementById('tick').textContent='error';
document.getElementById('tick').style.color='red';
});
}
refresh();setInterval(refresh,1000);
setInterval(function(){
document.getElementById('tick').textContent=Math.floor((Date.now()-lastUpdate)/1000)+'s';
},200);
})();
</script>
</body>
</html>`

const processDetailTemplate = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Name}} — GoSupervisor</title>
<style>
:root{--bg:#0a0e14;--bg-card:#12171f;--border:#1e2a38;--text:#c8ccd4;--text-dim:#6c7380;--accent:#39bae6;--green:#7fd962;--red:#f26d78;--amber:#ffb454;--purple:#d2a6ff;--orange:#ff8f40;--blue:#59c2ff;--font:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;--mono:"Fira Code","JetBrains Mono","Cascadia Code",monospace}
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:var(--font);line-height:1.5;min-height:100vh}
body::before{content:"";position:fixed;inset:0;background:radial-gradient(ellipse at 20% 50%,rgba(57,186,230,0.03) 0%,transparent 50%),radial-gradient(ellipse at 80% 20%,rgba(210,166,255,0.02) 0%,transparent 40%);pointer-events:none;z-index:0}
.header{position:sticky;top:0;z-index:10;background:var(--bg-card);border-bottom:1px solid var(--border);-webkit-backdrop-filter:blur(12px);backdrop-filter:blur(12px)}
.header-inner{max-width:1200px;margin:0 auto;padding:12px 24px;display:flex;align-items:center;justify-content:space-between}
.logo{display:flex;align-items:center;gap:10px}
.logo-icon{width:32px;height:32px;background:var(--accent);border-radius:8px;display:flex;align-items:center;justify-content:center;font-size:16px;font-weight:700;color:#0a0e14}
.logo-text{font-size:18px;font-weight:600;letter-spacing:-0.3px}
.back{color:var(--text-dim);text-decoration:none;padding:6px 14px;border-radius:6px;font-size:14px;transition:all 0.15s}
.back:hover{color:var(--text);background:rgba(255,255,255,0.05)}
main{max-width:1200px;margin:0 auto;padding:24px;position:relative;z-index:1}
.hero{display:flex;align-items:center;gap:14px;margin-bottom:24px;flex-wrap:wrap}
.hero h2{font-size:22px;font-weight:600}
.status{display:inline-flex;align-items:center;gap:6px;padding:4px 12px;border-radius:20px;font-size:12px;font-weight:600;letter-spacing:0.3px}
.status::before{content:"";width:7px;height:7px;border-radius:50%}
.status-RUNNING{background:rgba(127,217,98,0.12);color:var(--green)}.status-RUNNING::before{background:var(--green);animation:pulse 2s infinite}
.status-STOPPED{background:rgba(108,115,128,0.1);color:var(--text-dim)}.status-STOPPED::before{background:var(--text-dim)}
.status-STARTING{background:rgba(255,180,84,0.1);color:var(--amber)}.status-STARTING::before{background:var(--amber);animation:pulse 0.8s infinite}
.status-STOPPING{background:rgba(255,143,64,0.1);color:var(--orange)}.status-STOPPING::before{background:var(--orange);animation:pulse 0.8s infinite}
.status-EXITED{background:rgba(108,115,128,0.06);color:var(--text-dim)}.status-EXITED::before{background:var(--text-dim)}
.status-FATAL{background:rgba(242,109,120,0.12);color:var(--red)}.status-FATAL::before{background:var(--red)}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:0.4}}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:12px}
.card{background:var(--bg-card);border:1px solid var(--border);border-radius:10px;padding:14px 18px;transition:border-color 0.2s}
.card:hover{border-color:rgba(255,255,255,0.1)}
.card-label{font-size:10px;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.8px;margin-bottom:4px;font-weight:600}
.card-value{font-size:15px;font-family:var(--mono);word-break:break-all}
.card-value.big{font-size:20px;font-weight:700}
.actions{display:flex;gap:10px;margin-top:24px;flex-wrap:wrap}
.btn{display:inline-flex;align-items:center;gap:4px;padding:8px 18px;border:none;border-radius:8px;font-size:13px;font-weight:600;cursor:pointer;transition:all 0.12s;font-family:var(--font);letter-spacing:0.2px;text-decoration:none}
.btn:hover{transform:translateY(-1px);filter:brightness(1.15)}
.btn:active{transform:translateY(0)}
.btn-start{background:rgba(127,217,98,0.15);color:var(--green)}
.btn-start:hover{background:rgba(127,217,98,0.25)}
.btn-stop{background:rgba(242,109,120,0.15);color:var(--red)}
.btn-stop:hover{background:rgba(242,109,120,0.25)}
.btn-restart{background:rgba(89,194,255,0.15);color:var(--blue)}
.btn-restart:hover{background:rgba(89,194,255,0.25)}
.btn-logs{background:rgba(210,166,255,0.15);color:var(--purple)}
.btn-logs:hover{background:rgba(210,166,255,0.25)}
.accent{color:var(--accent)}.green{color:var(--green)}.red{color:var(--red)}
</style>
</head>
<body>
<div class="header"><div class="header-inner">
<div class="logo"><div class="logo-icon">GS</div><span class="logo-text">GoSupervisor</span></div>
<a href="/" class="back">&larr; 返回</a>
</div></div>
<main>
<div class="hero">
<h2>{{.Name}}</h2>
<span class="status status-{{lower .State}}" id="val-state">{{.State}}</span>
</div>
<div class="grid">
<div class="card"><div class="card-label">PID</div><div class="card-value big accent" id="val-pid">{{if gt .PID 0}}{{.PID}}{{else}}-{{end}}</div></div>
<div class="card"><div class="card-label">命令</div><div class="card-value">{{.Config.Command}}</div></div>
<div class="card"><div class="card-label">工作目录</div><div class="card-value">{{if .Config.Directory}}{{.Config.Directory}}{{else}}-(继承){{end}}</div></div>
<div class="card"><div class="card-label">运行时间</div><div class="card-value" id="val-uptime">{{.Uptime}}</div></div>
<div class="card"><div class="card-label">启动时间</div><div class="card-value" id="val-start">{{if not .StartTime.IsZero}}{{.StartTime.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</div></div>
<div class="card"><div class="card-label">停止时间</div><div class="card-value" id="val-stop">{{if not .StopTime.IsZero}}{{.StopTime.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</div></div>
<div class="card"><div class="card-label">退出码</div><div class="card-value" id="val-ec">{{if ne .ExitCode 0}}{{.ExitCode}}{{else}}-{{end}}</div></div>
<div class="card"><div class="card-label">启动重试</div><div class="card-value big" id="val-retries">{{.StartRetries}}</div></div>
<div class="card"><div class="card-label">重启次数</div><div class="card-value big" id="val-restarts">{{.RestartCount}}</div></div>
<div class="card"><div class="card-label">最后重启</div><div class="card-value" id="val-lastrestart">{{if not .LastRestart.IsZero}}{{.LastRestart.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</div></div>
<div class="card"><div class="card-label">CPU 使用率</div><div class="card-value big" id="val-cpu">{{.CPUUsage}}%</div></div>
<div class="card"><div class="card-label">内存使用</div><div class="card-value" id="val-mem">{{formatBytes .MemoryUsage}}</div></div>
<div class="card"><div class="card-label">健康状态</div><div class="card-value {{if .Healthy}}green{{else}}red{{end}}" id="val-health">{{if .Healthy}}健康{{else}}异常{{end}}</div></div>
<div class="card"><div class="card-label">自动启动</div><div class="card-value">{{if .Config.AutoStart}}是{{else}}否{{end}}</div></div>
<div class="card"><div class="card-label">自动重启</div><div class="card-value">{{if .Config.AutoRestart}}是{{else}}否{{end}}</div></div>
<div class="card"><div class="card-label">启动超时</div><div class="card-value">{{.Config.StartSecs}}s</div></div>
<div class="card"><div class="card-label">停止超时</div><div class="card-value">{{.Config.StopSecs}}s</div></div>
<div class="card"><div class="card-label">停止信号</div><div class="card-value">{{.Config.StopSignal}}</div></div>
<div class="card"><div class="card-label">用户</div><div class="card-value">{{if .Config.User}}{{.Config.User}}{{else}}(继承){{end}}</div></div>
<div class="card"><div class="card-label">优先级</div><div class="card-value">{{.Config.Priority}}</div></div>
<div class="card"><div class="card-label">依赖进程</div><div class="card-value">{{if gt (len .Config.DependsOn) 0}}{{.Config.DependsOn}}{{else}}无{{end}}</div></div>
</div>
<div class="actions">
{{if ne .State "RUNNING"}}<form method="post" action="/start" style="display:inline"><input type="hidden" name="name" value="{{.Name}}"><button class="btn btn-start" type="submit">启动</button></form>{{end}}
{{if eq .State "RUNNING"}}<form method="post" action="/stop" style="display:inline"><input type="hidden" name="name" value="{{.Name}}"><button class="btn btn-stop" type="submit">停止</button></form>{{end}}
<form method="post" action="/restart" style="display:inline"><input type="hidden" name="name" value="{{.Name}}"><button class="btn btn-restart" type="submit">重启</button></form>
<a href="/logs?name={{.Name}}" class="btn btn-logs">查看日志</a>
</div>
</main>
<div style="text-align:center;margin:16px 0 32px;font-size:11px;color:var(--text-dim)">Auto-refresh: <span id="tick" style="color:var(--green)">0s</span></div>
<script>
(function(){
var lastUpdate=Date.now(),pname="{{.Name}}";
function fmt(ts){if(!ts)return'-';var d=new Date(ts);if(d.getFullYear()<=1)return'-';
return d.getFullYear()+'-'+String(d.getMonth()+1).padStart(2,'0')+'-'+String(d.getDate()).padStart(2,'0')+' '
+String(d.getHours()).padStart(2,'0')+':'+String(d.getMinutes()).padStart(2,'0')+':'+String(d.getSeconds()).padStart(2,'0')}
function uptime(ts,st){if(!ts||st!=='RUNNING')return'-';var d=new Date(ts);
if(d.getFullYear()<=1)return'-';var diff=Math.floor((Date.now()-d.getTime())/1000);
if(diff<0)return'-';var h=Math.floor(diff/3600),m=Math.floor((diff%3600)/60),s=diff%60;
return h>0?h+'h '+m+'m':m>0?m+'m '+s+'s':s+'s'}
function refresh(){
fetch('/api/processes').then(function(r){return r.json()}).then(function(procs){
for(var i=0;i<procs.length;i++){if(procs[i].Name===pname){
var p=procs[i];
document.getElementById('val-state').textContent=p.State;
document.getElementById('val-pid').textContent=p.PID>0?p.PID:'-';
document.getElementById('val-uptime').textContent=uptime(p.StartTime,p.State);
document.getElementById('val-start').textContent=fmt(p.StartTime);
document.getElementById('val-stop').textContent=fmt(p.StopTime);
document.getElementById('val-ec').textContent=p.ExitCode!==0?p.ExitCode:'-';
document.getElementById('val-retries').textContent=p.StartRetries;
document.getElementById('val-restarts').textContent=p.RestartCount;
document.getElementById('val-lastrestart').textContent=fmt(p.LastRestart);
document.getElementById('val-cpu').textContent=p.CPUUsage+'%';
document.getElementById('val-mem').textContent=(p.MemoryUsage/1048576).toFixed(1)+' MB';
document.getElementById('val-health').textContent=p.Healthy?'健康':'异常';
document.getElementById('val-health').className='card-value '+(p.Healthy?'green':'red');
var h=document.querySelector('.hero .status');
if(h){h.textContent=p.State;h.className='status status-'+p.State.toUpperCase()}
break
}}
lastUpdate=Date.now()
}).catch(function(){document.getElementById('tick').style.color='red'})
}
refresh();setInterval(refresh,1000);
setInterval(function(){document.getElementById('tick').textContent=Math.floor((Date.now()-lastUpdate)/1000)+'s'},200);
})();
</script>
</body>
</html>`
