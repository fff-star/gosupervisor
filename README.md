# GoSupervisor

GoSupervisor is a process management tool written in Go, inspired by Python's Supervisor. It manages child processes — starting, stopping, restarting, and monitoring them — with a config-driven approach and both CLI and Web UI interfaces.

## Features

- **Process management**: start, stop, restart individual or all managed processes
- **Automatic restart**: auto-restart processes on unexpected exit, with configurable retry limits
- **Process monitoring**: liveness checks every second via null-signal probing
- **Resource monitoring**: CPU and memory usage tracking via `/proc` every 5 seconds
- **Log management**: per-process log files with automatic rotation, size limits, and gzip compression
- **Multi-format config**: INI, YAML, and JSON configuration files (auto-detected by extension)
- **Dependency ordering**: topological sort for startup/shutdown ordering via `dependson`
- **Web UI**: browser-based dashboard with process control, log viewing, and system info
- **Prometheus metrics**: exposable at `/metrics` for integration with monitoring stacks
- **Live reload**: SIGHUP signal or `-cmd reload` to reload configuration without restarting the supervisor

## Installation

### Prerequisites

- Go 1.16+ development environment
- Linux operating system

### Build

```bash
git clone https://github.com/user/gosupervisor.git
cd gosupervisor
go build -o gosupervisor ./cmd/gosupervisor
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
| `-d` | `false` | Run as daemon (stub) |
| `-t` | `false` | Validate config file and exit |
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
| `stopsignal` | string | `SIGTERM` | Signal sent on graceful stop |
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

## Web UI

The web UI is available when started with `-web`. Default address is `http://localhost:8080`.

### Routes

| Route | Description |
|-------|-------------|
| `GET /` | Dashboard — process list with status, PID, and action buttons |
| `GET /start?name=X` | Start a process |
| `GET /stop?name=X` | Stop a process |
| `GET /restart?name=X` | Restart a process |
| `GET /logs?name=X` | View process log |
| `GET /process?name=X` | Process detail (config, resource usage, health) |
| `GET /system` | System info (OS, memory, disk, uptime, Go version) |

## Prometheus Metrics

Available at `/metrics` when started with `-metrics`. Default address is `http://localhost:9090/metrics`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `gosupervisor_process_count` | Gauge | — | Total managed processes |
| `gosupervisor_process_status` | Gauge | `name` | Process state: 0=stopped, 1=starting, 2=running, 3=stopping, 4=exited, 5=fatal |
| `gosupervisor_process_uptime_seconds` | Gauge | `name` | Process uptime in seconds |
| `gosupervisor_process_restarts_total` | Gauge | `name` | Total restart count |
| `gosupervisor_process_cpu_usage_percent` | Gauge | `name` | CPU usage percentage |
| `gosupervisor_process_memory_usage_bytes` | Gauge | `name` | Memory usage in bytes |

## Logging

- **Process logs**: `logs/<process_name>.log`
- **System log**: `logs/system.log`

Logs are automatically rotated when they exceed the configured size limit (default 50 MB). Rotated logs are gzip-compressed, and old backups are cleaned up to stay within the configured backup count (default 10).

## Project structure

```
gosupervisor/
├── cmd/gosupervisor/       # CLI entry point
├── internal/
│   ├── config/             # Config parsing (INI/YAML/JSON)
│   ├── logger/             # Log management with rotation
│   ├── metrics/            # Prometheus metrics exposition
│   ├── process/            # Process lifecycle and monitoring
│   └── web/                # Web UI
├── go.mod
├── go.sum
└── README.md
```

## Notes

- Commands are executed via `/bin/sh -c`, supporting shell syntax in the command string.
- Process group isolation (`Setpgid=true`) is enabled on Linux for clean signal propagation.
- The `-d` (daemon) flag is a stub and not fully implemented. Run under a process manager like systemd for production daemonization.
- The `user` field only takes effect when running as root on Linux.

## License

MIT License.
