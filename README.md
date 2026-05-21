# GoSupervisor

GoSupervisor is a process management tool written in Go, inspired by Python's Supervisor. It manages child processes — starting, stopping, restarting, and monitoring them — with a config-driven approach and both CLI and Web UI interfaces.

## Features

- **Process management**: start, stop, restart individual or all managed processes
- **Graceful stop**: configurable signal (SIGTERM/SIGQUIT/SIGINT/…) with timeout, followed by SIGKILL
- **Automatic restart**: auto-restart processes on unexpected exit, with configurable retry limits
- **Process monitoring**: exit detection via built-in `cmd.Wait()` tracking; 1-second ticker triggers auto-restart for exited processes
- **Resource monitoring**: CPU (delta-based percentage) and memory (VmRSS) tracking via `/proc` every 5 seconds
- **Log management**: per-process stdout/stderr writers, per-stream rotation/size/backup settings, custom paths, gzip compression
- **Multi-format config**: INI, YAML, and JSON configuration files (auto-detected by extension); boolean defaults correctly distinguished from explicit `false`
- **Dependency ordering**: topological sort with priority-based tiebreaking for startup/shutdown ordering
- **User switching**: run processes as a different user via `syscall.Credential`
- **Web UI**: browser-based dashboard with process control (POST-only for mutations), log tail viewing, and system info
- **Prometheus metrics**: exposable at `/metrics` for integration with monitoring stacks
- **HTTP/TCP health checks**: configurable endpoint health monitoring with unhealthy threshold and auto-restart
- **Event hooks**: pre-start and post-stop shell scripts for custom lifecycle actions
- **REST API**: `/api/v1/` JSON API for programmatic process and group control
- **Process groups**: bulk start/stop/restart by group name
- **Restart rate limiting**: sliding window to prevent tight restart loops
- **Exit code-based restart policy**: allowlist/blocklist for restart decisions based on exit codes
- **Web UI authentication**: HTTP Basic Auth for dashboard and API protection
- **Config validation**: dependency reference checking at load time
- **Persistent state**: save/restore process state across supervisor restarts
- **Cgroup v2 integration**: per-process memory/CPU limits via Linux cgroups
- **Unix socket CLI**: text-based protocol for interactive process control
- **Stdin support**: pipe file content to managed process stdin
- **Webhook notifications**: POST process state transitions to external URLs
- **Live reload**: SIGHUP signal or `-cmd reload` to reload configuration without restarting the supervisor
- **Incremental reload**: config diff-based reload — only restarts changed/new/removed processes, unchanged processes keep running
- **Daemon mode**: fork with setsid for background operation (`-d` flag)
- **Config includes**: `[include]` section with `files=` glob patterns to split config across multiple files
- **Process templates**: `numprocs` and `process_name` expression (e.g. `worker_%(process_num)02d`) to launch N identical workers
- **Process group signaling**: `killasgroup`/`stopasgroup` options to signal entire process groups, preventing orphan processes
- **Web TLS/SSL**: `-web-cert`/`-web-key` flags and `[inet_http_server]` config section for HTTPS support
- **Interactive REPL**: `gosupervisorctl` (no args) enters an interactive shell with tab completion and command history

## Installation

### Prerequisites

- Go 1.16+ development environment
- Linux operating system

### Build

```bash
git clone https://github.com/user/gosupervisor.git
cd gosupervisor
make build    # builds both gosupervisor and gosupervisorctl
```

### Install to system path (optional)

```bash
sudo cp gosupervisor /usr/local/bin/
```

## Usage

### Command-line flags

| Flag | Default | Description |
|------|---------|-------------|
| `-c` | `gosupervisor.ini` | Config file path |
| `-l` | `./logs` | Log directory path |
| `-cmd` | `start` | Command: `start`, `stop`, `restart`, `status`, `reload`, `update` |
| `-p` | `""` | Process name (for targeting a single process) |
| `-web` | `false` | Enable web UI |
| `-web-addr` | `:8080` | Web UI listen address |
| `-metrics` | `false` | Enable Prometheus metrics |
| `-metrics-addr` | `:9090` | Metrics HTTP listen address |
| `-d` | `false` | Run as daemon (forks with setsid, parent exits) |
| `-t` | `false` | Validate config file and exit |
| `-g` | `""` | Group name (for targeting a group of processes) |
| `-web-user` | `""` | Web UI Basic Auth username |
| `-web-pass` | `""` | Web UI Basic Auth password |
| `-web-api-auth` | `true` | Require API v1 Basic Auth (on by default; set `=false` to expose API without auth) |
| `-socket` | `""` | Unix socket path for CLI control |
| `-state-file` | `""` | Path to persist process state as JSON |
| `-web-cert` | `""` | TLS certificate file path |
| `-web-key` | `""` | TLS private key file path |
| `-version` | `false` | Print version and exit |

### Basic commands

```bash
# Validate configuration
gosupervisor -t -c config.ini

# Start all processes
gosupervisor -cmd start

# Start a single process
gosupervisor -cmd start -p myapp

# Stop all processes
gosupervisor -cmd stop

# Stop a single process
gosupervisor -cmd stop -p myapp

# Restart all processes
gosupervisor -cmd restart

# Restart a single process
gosupervisor -cmd restart -p myapp

# Show process status
gosupervisor -cmd status
gosupervisor -cmd status -p myapp

# Reload configuration (also triggers on SIGHUP)
gosupervisor -cmd reload

# Update a single process config
gosupervisor -cmd update -p myapp

# Start with web UI and Prometheus metrics
gosupervisor -cmd start -web -web-addr :8080 -metrics -metrics-addr :9090
```

## Configuration

GoSupervisor supports INI, YAML, and JSON config formats. The format is auto-detected by file extension (`.ini`, `.yaml`/`.yml`, `.json`).

### INI format

```ini
[program:myapp]
command=python -m http.server 8000
directory=.
autostart=true
autorestart=true
startsecs=2
startretries=3
environment=PATH=$PATH,NODE_ENV=production

[program:worker]
command=node worker.js
directory=./app
autostart=false
autorestart=true
startsecs=3
startretries=5
dependson=myapp
```

### YAML format

```yaml
programs:
  myapp:
    command: python -m http.server 8000
    directory: .
    autostart: true
    autorestart: true
    startsecs: 2
    startretries: 3
    environment:
      NODE_ENV: production

  worker:
    command: node worker.js
    directory: ./app
    autostart: false
    autorestart: true
    startsecs: 3
    startretries: 5
    dependson:
      - myapp
```

### JSON format

```json
{
  "programs": {
    "myapp": {
      "command": "python -m http.server 8000",
      "directory": ".",
      "autostart": true,
      "autorestart": true,
      "startsecs": 2,
      "startretries": 3,
      "environment": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

### Configuration fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `command` | string | — | Command to execute (run via `/bin/sh -c`) |
| `directory` | string | `""` | Working directory |
| `autostart` | bool | `true` | Auto-start when supervisor starts |
| `autorestart` | bool | `true` | Auto-restart on unexpected exit |
| `startsecs` | int | `1` | Seconds the process must stay up to be considered started |
| `startretries` | int | `3` | Max restarts before entering FATAL state |
| `stopsecs` | int | `10` | Seconds to wait for graceful stop before SIGKILL |
| `stopsignal` | string | `SIGTERM` | Signal sent for graceful stop (SIGTERM, SIGQUIT, SIGINT, SIGHUP, SIGUSR1, SIGUSR2, SIGKILL) |
| `user` | string | `""` | Run as this user (Linux only) |
| `environment` | map/string | `{}` | Extra environment variables. INI: comma-separated `KEY=VAL` pairs |
| `redirectstdout` | bool | `true` | Capture stdout to log |
| `redirectstderr` | bool | `true` | Capture stderr to log |
| `stdoutlogfile` | string | — | Custom stdout log path |
| `stderrlogfile` | string | — | Custom stderr log path |
| `stdoutlogmaxbytes` | int64 | `50MB` | Max stdout log size before rotation |
| `stdoutlogbackupcount` | int | `10` | Rotated stdout logs to keep |
| `stderrlogmaxbytes` | int64 | `50MB` | Max stderr log size before rotation |
| `stderrlogbackupcount` | int | `10` | Rotated stderr logs to keep |
| `priority` | int | `999` | Startup priority (lower = earlier) |
| `umask` | int | `022` | File creation mask |
| `dependson` | []string | `[]` | Comma-separated process dependencies |
| `group` | string | `""` | Process group name for bulk operations |
| `healthcheckurl` | string | `""` | HTTP or TCP endpoint for health checks (e.g., `http://localhost:8080/health` or `tcp://localhost:3306`) |
| `healthcheckinterval` | int | `30` | Seconds between health checks |
| `healthchecktimeout` | int | `5` | Seconds before health check attempt times out |
| `healthcheckunhealthythreshold` | int | `3` | Consecutive failures before marking unhealthy |
| `healthcheckrestart` | bool | `false` | Auto-restart process when health check fails |
| `cputhresholdpercent` | float64 | `90.0` | CPU usage percentage threshold for resource-based health |
| `memorythresholdbytes` | int64 | `2GB` | Memory usage bytes threshold for resource-based health |
| `prestartscript` | string | `""` | Shell script to run before process starts |
| `poststopscript` | string | `""` | Shell script to run after process stops |
| `restartmaxcount` | int | `0` | Max restarts within the window (0 = unlimited) |
| `restartwindowsecs` | int | `60` | Sliding window in seconds for rate limiting |
| `restartcodes` | []int | `[]` | Exit codes that trigger restart (empty = all) |
| `norestartcodes` | []int | `[]` | Exit codes that skip restart |
| `cgrouppath` | string | `""` | Cgroup v2 path for resource limits (e.g., `/sys/fs/cgroup/myapp`) |
| `webhookurl` | string | `""` | URL to POST process state transitions |
| `stdinfile` | string | `""` | File path to pipe as process stdin |

## Web UI

The web UI is available when started with `-web`. Default address is `http://localhost:8080`.

### Routes

| Route | Description |
|-------|-------------|
| `GET /` | Dashboard — process list with status, PID, and action buttons |
| `POST /start` | Start a process (body: `name=X`) |
| `POST /stop` | Stop a process (body: `name=X`) |
| `POST /restart` | Restart a process (body: `name=X`) |
| `GET /logs?name=X` | View process log (last 1000 lines / 1 MB tail) |
| `GET /process?name=X` | Process detail (config, resource usage, health) |
| `GET /system` | System info (OS, memory, disk, uptime, Go version) |

### Authentication

Start with `-web-user` and `-web-pass` to enable HTTP Basic Auth on all routes (web UI and API). By default, API routes are also protected. Set `-web-api-auth=false` to allow programmatic API access without credentials while keeping the web UI protected.

## REST API v1

All endpoints return JSON responses with `{"status": "ok", "message": "..."}` or `{"status": "error", "message": "..."}`.

### Processes

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/v1/processes` | List all processes (JSON array of snapshots) |
| `GET` | `/api/v1/processes/{name}` | Get single process detail |
| `POST` | `/api/v1/processes/{name}/start` | Start a process |
| `POST` | `/api/v1/processes/{name}/stop` | Stop a process |
| `POST` | `/api/v1/processes/{name}/restart` | Restart a process |
| `GET` | `/api/v1/processes/{name}/logs` | Stream process log via Server-Sent Events (SSE) |

### Groups

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/api/v1/groups/{group}/start` | Start all processes in a group |
| `POST` | `/api/v1/groups/{group}/stop` | Stop all processes in a group |
| `POST` | `/api/v1/groups/{group}/restart` | Restart all processes in a group |

## Health Checks

Each process can be configured with a health check URL. Two protocols are supported:

- **HTTP**: `healthcheckurl=http://localhost:8080/health` — performs HTTP GET, expects 2xx or 3xx response
- **TCP**: `healthcheckurl=tcp://localhost:3306` — performs TCP dial to verify port is open

When a process exceeds `healthcheckunhealthythreshold` consecutive failures, the process is marked unhealthy (`Healthy: false`). Set `healthcheckrestart=true` to automatically restart unhealthy processes. The `cputhresholdpercent` and `memorythresholdbytes` fields allow tuning of the built-in resource-based health check (CPU and memory monitoring via `/proc`). Health checks begin after the process enters the RUNNING state.

## Event Hooks

Shell scripts executed at process lifecycle events:

- **Pre-start** (`prestartscript`): runs before the process starts. If the script exits non-zero, the start is aborted.
- **Post-stop** (`poststopscript`): runs after the process exits. Best-effort; failures are logged but do not affect the lifecycle.

## Process Groups

Assign processes to groups with the `group` config field, then control them in bulk:

```bash
gosupervisor -cmd start -g web       # start all processes in the "web" group
gosupervisor -cmd stop -g web        # stop all processes in the "web" group
gosupervisor -cmd restart -g workers # restart all processes in the "workers" group
```

Group operations use topological sort based on `dependson` for correct startup/shutdown ordering, and are also available via the REST API and Unix socket CLI.

## Restart Rate Limiting

Prevent tight restart loops with `restartmaxcount` and `restartwindowsecs`. When a process restarts more than `restartmaxcount` times within `restartwindowsecs` seconds (sliding window), further auto-restarts are suppressed and the process enters FATAL state.

## Exit Code-Based Restart Policy

Control which exit codes trigger an auto-restart:

- **`restartcodes`**: only these exit codes cause a restart (empty = all codes). Example: `1,2,3`
- **`norestartcodes`**: these exit codes skip restart even if `restartcodes` would match. Example: `0,143`

## Persistent State

With `-state-file /var/lib/gosupervisor/state.json`, the supervisor saves process metadata (name, group, restart count, last start time) on each state change. On startup, it restores this state so restart counts survive supervisor restarts.

## Cgroup v2 Integration

Set `cgrouppath` to a cgroup v2 directory (e.g., `/sys/fs/cgroup/myapp`). On process start, the PID is written to `cgroup.procs`. Configure `memory.max`, `cpu.max`, etc. externally or via a pre-start script.

## Unix Socket CLI

Start with `-socket /var/run/gosupervisor.sock` to enable a Unix domain socket for interactive control:

```
nc -U /var/run/gosupervisor.sock
> status           # list all processes
> start myapp      # start a process
> stop myapp       # stop a process
> restart myapp    # restart a process
> group-start web  # start a group
> group-stop web   # stop a group
> group-restart web # restart a group
> help             # show available commands
> quit             # close connection
```

## gosupervisorctl

A dedicated CLI client for connecting to the Unix socket:

```bash
# Build
go build -o gosupervisorctl ./cmd/gosupervisorctl

# Usage
gosupervisorctl -socket /tmp/gosupervisor.sock status
gosupervisorctl -socket /tmp/gosupervisor.sock status myapp
gosupervisorctl -socket /tmp/gosupervisor.sock start myapp
gosupervisorctl -socket /tmp/gosupervisor.sock stop myapp
gosupervisorctl -socket /tmp/gosupervisor.sock restart myapp
gosupervisorctl -socket /tmp/gosupervisor.sock group-start web
gosupervisorctl -socket /tmp/gosupervisor.sock group-stop web
gosupervisorctl -socket /tmp/gosupervisor.sock group-restart web
```

## Stdin Support

Pipe file content to a process's stdin on start by setting `stdinfile=/path/to/input.txt`.

## Webhook Notifications

Set `webhookurl` to receive HTTP POST notifications on all process state transitions (RUNNING, STOPPED, EXITED, and FATAL). The JSON payload includes process name, group, state, PID, exit code, and timestamp.

## Prometheus Metrics

Available at `/metrics` when started with `-metrics`. Default address is `http://localhost:9090/metrics`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `gosupervisor_process_count` | Gauge | — | Total managed processes |
| `gosupervisor_process_status` | Gauge | `name` | Process state: 0=stopped, 1=starting, 2=running, 3=stopping, 4=exited, 5=fatal |
| `gosupervisor_process_uptime_seconds` | Gauge | `name` | Process uptime in seconds |
| `gosupervisor_process_restarts_total` | Counter | `name` | Total restart count |
| `gosupervisor_process_cpu_usage_percent` | Gauge | `name` | CPU usage percentage |
| `gosupervisor_process_memory_usage_bytes` | Gauge | `name` | Memory usage in bytes |
| `gosupervisor_healthcheck_status` | Gauge | `name` | Health check status: 1=healthy, 0=unhealthy |
| `gosupervisor_healthcheck_failures_total` | Counter | `name` | Health check failure count |
| `gosupervisor_uptime_seconds` | Gauge | — | Supervisor uptime in seconds |
| `gosupervisor_goroutines` | Gauge | — | Current goroutine count |
| `gosupervisor_memory_bytes` | Gauge | — | Supervisor memory usage in bytes |
| `gosupervisor_config_reloads_total` | Counter | — | Config reload count |

## Logging

- **Process logs**: `{logdir}/{process_name}.log` by default, or custom paths via `stdoutlogfile` / `stderrlogfile`.
- **System log**: `{logdir}/system.log` — automatically rotated at 50 MB (10 backups, gzip-compressed).
- **Log tail**: web UI shows only the last 1000 lines / 1 MB. SSE streaming available at `/api/v1/processes/{name}/logs` for real-time following.

Logs are automatically rotated when they exceed the configured size limit (default 50 MB). Each stream (stdout/stderr) has its own size limit and backup count. Rotated logs are gzip-compressed, and old backups are cleaned up to stay within the configured count (default 10).

When both stdout and stderr target the same file path, a single writer is shared to avoid interleaved writes from separate file handles.

## Project structure

```
gosupervisor/
├── cmd/
│   ├── gosupervisor/       # Main CLI entry point
│   └── gosupervisorctl/    # Socket CLI client
├── internal/
│   ├── config/             # Config parsing (INI/YAML/JSON)
│   ├── logger/             # Log management with rotation
│   ├── metrics/            # Prometheus metrics exposition
│   ├── process/            # Process lifecycle and monitoring
│   ├── socket/             # Unix socket CLI server
│   └── web/                # Web UI and REST API
├── go.mod
├── go.sum
└── README.md
```

## Notes

- Commands are executed via `/bin/sh -c`, supporting shell syntax in the command string.
- Process group isolation (`Setpgid=true`) is enabled on Linux for clean signal propagation.
- **Daemon mode** (`-d`): forks a child with `setsid`, parent exits. For production, running under a process manager like systemd is still recommended.
- **Graceful stop**: `stopsignal` is sent first, then after `stopsecs` (default 10s), SIGKILL is sent if the process is still alive.
- **User switching**: the `user` field enables running processes as a different user via `syscall.Credential`. Only takes effect when running as root.
- **Umask**: set via `syscall.Umask()` serialized across all process starts.
- **CPU metrics**: delta-based percentage computed from `/proc/pid/stat` and `/proc/stat` rather than raw tick counts.
- **Web security**: state-changing endpoints (start/stop/restart) accept POST only, with `Origin`/`Referer` header validation against the `Host` header for CSRF protection. Process names are validated against path traversal (`/`, `..`, `\`). Templates use `html/template` for auto-XSS-escaping. Rate limiting uses `RemoteAddr` only (does not trust `X-Forwarded-For`). API error responses return generic messages to avoid information disclosure.
- **Thread safety**: `ProcessManager.Processes` is protected by `sync.RWMutex`. Use `RangeProcesses()`, `GetProcess()`, `Len()` (readers) and `AddProcess()`, `RemoveProcess()`, `ReplaceProcesses()` (writers) for safe concurrent access.
- **Goroutine lifecycle**: `monitorResources()` exits cleanly when a process is restarted (via per-start context cancellation). `monitor()` signals completion via `monitorDone` channel, ensuring `Start()` waits for old goroutines before creating new ones.
- **Separate HTTP mux**: web and metrics servers each use `http.NewServeMux()` to avoid handler leakage between addresses.
- **Template caching**: all HTML templates are parsed once at startup (`NewWebServer`) rather than per-request.
- **YAML/JSON defaults**: boolean fields (`autostart`, `autorestart`, `redirectstdout`, `redirectstderr`) default to `true` only when absent from the config. An explicit `false` is preserved. All struct fields have explicit `yaml` and `json` tags.
- **Log paths**: when both stdout and stderr target the same file, a single writer is shared to avoid interleaved corruption. Custom paths via `stdoutlogfile`/`stderrlogfile` are fully respected for rotation and tail viewing.
- **Atomic byte counting**: `countingWriter` uses `sync/atomic` for concurrent-safe byte tracking during log rotation.
- **Robust /proc parsing**: VmRSS and MemTotal/MemAvailable use `strings.Fields` instead of tab-dependent `Sscanf`. Disk info uses `syscall.Statfs` for efficient, locale-independent byte output.
- **Retry limit**: `StartRetries` is enforced with `>=` check, preventing off-by-one extra restart.

## License

MIT License.
