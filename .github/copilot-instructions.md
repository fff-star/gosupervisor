# Copilot / AI Agent Instructions for GoSupervisor

Purpose: brief, actionable guidance to help AI coding agents contribute safely and productively.

- **Big picture**:
  - Single binary entry: `cmd/gosupervisor/main.go` builds and wires components.
  - Core components live under `internal/`: `config`, `process`, `logger`, `metrics`, `web`.
  - Data flow: configuration -> `ProcessManager` (in-memory) -> `Monitor` -> exposes state to `web` and `metrics`; `logger` stores process logs on disk (`./logs`).
  - No external DB; state is held in-memory and persisted only via OS processes and log files.

- **Where to look for examples**:
  - CLI / wiring: `cmd/gosupervisor/main.go` (flag usage, lifecycle, signal handling).
  - Config parsing and formats: `internal/config/config.go` (INI default; YAML/JSON supported). See `internal/config/testdata/` for sample configs.
  - Process lifecycle: `internal/process/process.go`, `internal/process/monitor.go` (start/stop/restart, dependency ordering via `DependsOn`).
  - Platform-specific hooks: `internal/process/proc_linux.go` (build-tagged Linux behavior). This repository is Linux-only; non-Linux code has been removed.
  - Logging: `internal/logger/logger.go` (per-process files, rotation, gzip compression). Web UI reads `./logs/<name>.log`.
  - Web UI & templates: `internal/web/web.go` (endpoints `/`, `/start`, `/stop`, `/restart`, `/logs`, `/process`, `/system`; templates embedded in constants).
  - Metrics: `internal/metrics/metrics.go` (Prometheus registry, `/metrics` handler, collector loop).

- **Key project conventions / patterns**:
  - `internal/` packages are intentionally internal; keep APIs minimal and package-scoped.
  - Config: INI sections use `[program:<name>]` with keys like `command`, `directory`, `autostart`, `autorestart`, `dependsOn` (comma-separated). Use `internal/config/load*` helpers.
  - Process dependency ordering: `ProcessManager.StartAll()` uses a topological sort on `DependsOn`; `StopAll()` stops in reverse order.
  - Logging: create a `logger` via `logger.NewDefaultLogger(logDir)` in `main.go`; process `Stdout`/`Stderr` are written into per-process files.
  - Tests and testdata: unit tests reference `internal/*_test.go` and `internal/*/testdata/` directories — use those fixtures when adding tests.

- **Build / test / run workflows** (examples agents should suggest):
  - Build: `go build ./cmd/gosupervisor`
  - Run (local):
    - Basic: `./gosupervisor -c ./internal/config/testdata/test_config.ini -l ./logs`
    - With web UI and metrics: `./gosupervisor -c ./internal/config/testdata/test_config.ini -web -web-addr :8080 -metrics -metrics-addr :9090`
    - Test config only: `./gosupervisor -t -c ./internal/config/testdata/test_config.ini`
  - Tests: `go test ./...` or `go test ./internal/config -v` for package-specific tests.
  - Cross-build: set `GOOS`/`GOARCH` when needed. This repo targets Linux; refer to `internal/process/proc_linux.go` for Linux process-group behavior.

- **Platform notes / gotchas**:
  - `internal/process/process.go` uses `/bin/sh -c <command>` for command execution on Linux. If adding support for other OSes, add OS-specific files with build tags.
  - Linux-specific process group behavior is implemented in `internal/process/proc_linux.go` via `//go:build linux`.
  - `runAsDaemon()` in `main.go` is a stub—do not assume full daemonization is implemented.

- **Integration points / external deps**:
  - Prometheus client: `github.com/prometheus/client_golang` in `internal/metrics`.
  - YAML support: `gopkg.in/yaml.v3` in `internal/config`.

- **When editing code** (explicit, project-specific guidance):
  - If changing process exec or signal handling, add or modify build-tagged files rather than peppering runtime OS checks.
  - When modifying config parsing, update `internal/config/testdata/*` fixtures and the package tests.
  - When changing logging shape or filenames, update `internal/web/web.go` which reads `./logs/<name>.log` for the UI.
  - Use existing state types (`Process`, `ProcessManager`, `Monitor`) rather than introducing global state.

- **Small examples to copy from**:
  - Start all processes honoring dependencies: see `internal/process/process.go` -> `StartAll()` + `topologicalSort()`.
  - Web endpoints and template usage: `internal/web/web.go` (templates embedded as `indexTemplate`, `logsTemplate`).
  - Prometheus metrics registration & update loop: `internal/metrics/metrics.go`.

If any section is unclear or you'd like more detail (for example, line-level references or test commands), tell me which area to expand. I can iterate.
