package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRoundTrip_INI writes an INI file with all known fields, loads it,
// and verifies every field is preserved.
func TestRoundTrip_INI(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.ini")

	ini := `[supervisord]
webaddr=:8888
webuser=admin
webpass=secret
metricsaddr=:9090
socketpath=/tmp/test.sock
statefile=/tmp/state.json
logdir=/tmp/logs
corsorigin=https://example.com
ratelimitrps=100
webcert=/tmp/cert.pem
webkey=/tmp/key.pem

[program:full_test]
command=sleep 60
directory=/tmp
autostart=true
autorestart=true
startsecs=5
startretries=5
stopsecs=15
stopsignal=SIGTERM
user=root
environment=KEY1=val1,KEY2=val2
redirectstdout=true
redirectstderr=true
stdoutlogfile=/tmp/out.log
stderrlogfile=/tmp/err.log
stdoutlogmaxbytes=104857600
stdoutlogbackupcount=20
stderrlogmaxbytes=52428800
stderrlogbackupcount=5
priority=100
umask=18
dependson=dep1,dep2
group=web
healthcheckurl=http://localhost:8080/health
healthcheckinterval=15
healthchecktimeout=3
healthcheckunhealthythreshold=5
healthcheckrestart=true
cputhresholdpercent=80.5
memorythresholdbytes=1073741824
prestartscript=/tmp/pre.sh
poststopscript=/tmp/post.sh
restartmaxcount=10
restartwindowsecs=120
restartcodes=0,2
norestartcodes=1,3
cgrouppath=/sys/fs/cgroup/test
webhookurl=http://localhost:8080/webhook
webhookretries=5
webhooktimeout=10
stdinfile=/tmp/stdin.txt
numprocs=2
process_name=%(program_name)s_%(process_num)02d
killasgroup=true
stopasgroup=true
`

	if err := os.WriteFile(configPath, []byte(ini), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// ---- Server config ----
	if cfg.Server == nil {
		t.Fatal("expected Server config")
	}
	if cfg.Server.WebAddr != ":8888" {
		t.Errorf("WebAddr: expected :8888, got %s", cfg.Server.WebAddr)
	}
	if cfg.Server.WebUser != "admin" {
		t.Errorf("WebUser: expected admin, got %s", cfg.Server.WebUser)
	}
	if cfg.Server.WebPass != "secret" {
		t.Errorf("WebPass: expected secret, got %s", cfg.Server.WebPass)
	}
	if cfg.Server.MetricsAddr != ":9090" {
		t.Errorf("MetricsAddr: expected :9090, got %s", cfg.Server.MetricsAddr)
	}
	if cfg.Server.SocketPath != "/tmp/test.sock" {
		t.Errorf("SocketPath: expected /tmp/test.sock, got %s", cfg.Server.SocketPath)
	}
	if cfg.Server.StateFile != "/tmp/state.json" {
		t.Errorf("StateFile: expected /tmp/state.json, got %s", cfg.Server.StateFile)
	}
	if cfg.Server.LogDir != "/tmp/logs" {
		t.Errorf("LogDir: expected /tmp/logs, got %s", cfg.Server.LogDir)
	}
	if cfg.Server.CORSOrigin != "https://example.com" {
		t.Errorf("CORSOrigin: expected https://example.com, got %s", cfg.Server.CORSOrigin)
	}
	if cfg.Server.RateLimitRPS != 100 {
		t.Errorf("RateLimitRPS: expected 100, got %d", cfg.Server.RateLimitRPS)
	}
	if cfg.Server.WebCert != "/tmp/cert.pem" {
		t.Errorf("WebCert: expected /tmp/cert.pem, got %s", cfg.Server.WebCert)
	}
	if cfg.Server.WebKey != "/tmp/key.pem" {
		t.Errorf("WebKey: expected /tmp/key.pem, got %s", cfg.Server.WebKey)
	}

	// ---- Program config ----
	prog := cfg.Programs["full_test"]
	if prog == nil {
		t.Fatal("expected full_test program")
	}

	if prog.Command != "sleep 60" {
		t.Errorf("Command: expected sleep 60, got %s", prog.Command)
	}
	if prog.Directory != "/tmp" {
		t.Errorf("Directory: expected /tmp, got %s", prog.Directory)
	}
	if prog.AutoStart != true {
		t.Errorf("AutoStart: expected true")
	}
	if prog.AutoRestart != true {
		t.Errorf("AutoRestart: expected true")
	}
	if prog.StartSecs != 5 {
		t.Errorf("StartSecs: expected 5, got %d", prog.StartSecs)
	}
	if prog.StartRetries != 5 {
		t.Errorf("StartRetries: expected 5, got %d", prog.StartRetries)
	}
	if prog.StopSecs != 15 {
		t.Errorf("StopSecs: expected 15, got %d", prog.StopSecs)
	}
	if prog.StopSignal != "SIGTERM" {
		t.Errorf("StopSignal: expected SIGTERM, got %s", prog.StopSignal)
	}
	if prog.User != "root" {
		t.Errorf("User: expected root, got %s", prog.User)
	}
	if len(prog.Environment) != 2 {
		t.Errorf("Environment: expected 2 entries, got %d", len(prog.Environment))
	}
	if prog.Environment["KEY1"] != "val1" {
		t.Errorf("Environment[KEY1]: expected val1, got %s", prog.Environment["KEY1"])
	}
	if prog.Environment["KEY2"] != "val2" {
		t.Errorf("Environment[KEY2]: expected val2, got %s", prog.Environment["KEY2"])
	}
	if prog.RedirectStdout != true {
		t.Errorf("RedirectStdout: expected true")
	}
	if prog.RedirectStderr != true {
		t.Errorf("RedirectStderr: expected true")
	}
	if prog.StdoutLogFile != "/tmp/out.log" {
		t.Errorf("StdoutLogFile: expected /tmp/out.log, got %s", prog.StdoutLogFile)
	}
	if prog.StderrLogFile != "/tmp/err.log" {
		t.Errorf("StderrLogFile: expected /tmp/err.log, got %s", prog.StderrLogFile)
	}
	if prog.StdoutLogMaxBytes != 104857600 {
		t.Errorf("StdoutLogMaxBytes: expected 104857600, got %d", prog.StdoutLogMaxBytes)
	}
	if prog.StdoutLogBackupCount != 20 {
		t.Errorf("StdoutLogBackupCount: expected 20, got %d", prog.StdoutLogBackupCount)
	}
	if prog.StderrLogMaxBytes != 52428800 {
		t.Errorf("StderrLogMaxBytes: expected 52428800, got %d", prog.StderrLogMaxBytes)
	}
	if prog.StderrLogBackupCount != 5 {
		t.Errorf("StderrLogBackupCount: expected 5, got %d", prog.StderrLogBackupCount)
	}
	if prog.Priority != 100 {
		t.Errorf("Priority: expected 100, got %d", prog.Priority)
	}
	if prog.Umask != 18 {
		t.Errorf("Umask: expected 18, got %d", prog.Umask)
	}
	if len(prog.DependsOn) != 2 {
		t.Errorf("DependsOn: expected 2 entries, got %d", len(prog.DependsOn))
	}
	if len(prog.DependsOn) >= 1 && prog.DependsOn[0] != "dep1" {
		t.Errorf("DependsOn[0]: expected dep1, got %s", prog.DependsOn[0])
	}
	if len(prog.DependsOn) >= 2 && prog.DependsOn[1] != "dep2" {
		t.Errorf("DependsOn[1]: expected dep2, got %s", prog.DependsOn[1])
	}
	if prog.Group != "web" {
		t.Errorf("Group: expected web, got %s", prog.Group)
	}
	if prog.HealthCheckURL != "http://localhost:8080/health" {
		t.Errorf("HealthCheckURL: expected http://localhost:8080/health, got %s", prog.HealthCheckURL)
	}
	if prog.HealthCheckInterval != 15 {
		t.Errorf("HealthCheckInterval: expected 15, got %d", prog.HealthCheckInterval)
	}
	if prog.HealthCheckTimeout != 3 {
		t.Errorf("HealthCheckTimeout: expected 3, got %d", prog.HealthCheckTimeout)
	}
	if prog.HealthCheckUnhealthyThreshold != 5 {
		t.Errorf("HealthCheckUnhealthyThreshold: expected 5, got %d", prog.HealthCheckUnhealthyThreshold)
	}
	if prog.HealthCheckRestart != true {
		t.Errorf("HealthCheckRestart: expected true")
	}
	if prog.CPUThresholdPercent != 80.5 {
		t.Errorf("CPUThresholdPercent: expected 80.5, got %f", prog.CPUThresholdPercent)
	}
	if prog.MemoryThresholdBytes != 1073741824 {
		t.Errorf("MemoryThresholdBytes: expected 1073741824, got %d", prog.MemoryThresholdBytes)
	}
	if prog.PreStartScript != "/tmp/pre.sh" {
		t.Errorf("PreStartScript: expected /tmp/pre.sh, got %s", prog.PreStartScript)
	}
	if prog.PostStopScript != "/tmp/post.sh" {
		t.Errorf("PostStopScript: expected /tmp/post.sh, got %s", prog.PostStopScript)
	}
	if prog.RestartMaxCount != 10 {
		t.Errorf("RestartMaxCount: expected 10, got %d", prog.RestartMaxCount)
	}
	if prog.RestartWindowSecs != 120 {
		t.Errorf("RestartWindowSecs: expected 120, got %d", prog.RestartWindowSecs)
	}
	if len(prog.RestartCodes) != 2 {
		t.Errorf("RestartCodes: expected 2 entries, got %d", len(prog.RestartCodes))
	}
	if len(prog.RestartCodes) >= 1 && prog.RestartCodes[0] != 0 {
		t.Errorf("RestartCodes[0]: expected 0, got %d", prog.RestartCodes[0])
	}
	if len(prog.RestartCodes) >= 2 && prog.RestartCodes[1] != 2 {
		t.Errorf("RestartCodes[1]: expected 2, got %d", prog.RestartCodes[1])
	}
	if len(prog.NoRestartCodes) != 2 {
		t.Errorf("NoRestartCodes: expected 2 entries, got %d", len(prog.NoRestartCodes))
	}
	if len(prog.NoRestartCodes) >= 1 && prog.NoRestartCodes[0] != 1 {
		t.Errorf("NoRestartCodes[0]: expected 1, got %d", prog.NoRestartCodes[0])
	}
	if len(prog.NoRestartCodes) >= 2 && prog.NoRestartCodes[1] != 3 {
		t.Errorf("NoRestartCodes[1]: expected 3, got %d", prog.NoRestartCodes[1])
	}
	if prog.CgroupPath != "/sys/fs/cgroup/test" {
		t.Errorf("CgroupPath: expected /sys/fs/cgroup/test, got %s", prog.CgroupPath)
	}
	if prog.WebhookURL != "http://localhost:8080/webhook" {
		t.Errorf("WebhookURL: expected http://localhost:8080/webhook, got %s", prog.WebhookURL)
	}
	if prog.WebhookRetries != 5 {
		t.Errorf("WebhookRetries: expected 5, got %d", prog.WebhookRetries)
	}
	if prog.WebhookTimeout != 10 {
		t.Errorf("WebhookTimeout: expected 10, got %d", prog.WebhookTimeout)
	}
	if prog.StdinFile != "/tmp/stdin.txt" {
		t.Errorf("StdinFile: expected /tmp/stdin.txt, got %s", prog.StdinFile)
	}
	if prog.NumProcs != 2 {
		t.Errorf("NumProcs: expected 2, got %d", prog.NumProcs)
	}
	if prog.ProcessName != "%(program_name)s_%(process_num)02d" {
		t.Errorf("ProcessName: expected %%(program_name)s_%%(process_num)02d, got %s", prog.ProcessName)
	}
	if prog.KillsAsGroup != true {
		t.Errorf("KillsAsGroup: expected true")
	}
	if prog.StopAsGroup != true {
		t.Errorf("StopAsGroup: expected true")
	}
}

// TestRoundTrip_Defaults verifies that default values are applied
// when not specified in the config file.
func TestRoundTrip_Defaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "minimal.ini")

	if err := os.WriteFile(configPath, []byte(`
[program:minimal]
command=echo hello
`), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	prog := cfg.Programs["minimal"]
	if prog == nil {
		t.Fatal("expected minimal program")
	}

	// Check defaults
	if prog.AutoStart != true {
		t.Error("AutoStart default should be true")
	}
	if prog.AutoRestart != true {
		t.Error("AutoRestart default should be true")
	}
	if prog.StartSecs != 1 {
		t.Errorf("StartSecs default should be 1, got %d", prog.StartSecs)
	}
	if prog.StartRetries != 3 {
		t.Errorf("StartRetries default should be 3, got %d", prog.StartRetries)
	}
	if prog.StopSecs != 10 {
		t.Errorf("StopSecs default should be 10, got %d", prog.StopSecs)
	}
	if prog.StopSignal != "SIGTERM" {
		t.Errorf("StopSignal default should be SIGTERM, got %s", prog.StopSignal)
	}
	if prog.RedirectStdout != true {
		t.Error("RedirectStdout default should be true")
	}
	if prog.RedirectStderr != true {
		t.Error("RedirectStderr default should be true")
	}
	if prog.Priority != 999 {
		t.Errorf("Priority default should be 999, got %d", prog.Priority)
	}
	if prog.Umask != 022 {
		t.Errorf("Umask default should be 18 (octal 022), got %d", prog.Umask)
	}
	if prog.Environment == nil {
		t.Error("Environment default should be non-nil map")
	}
	if prog.DependsOn == nil {
		t.Error("DependsOn default should be non-nil slice")
	}
	if prog.RestartCodes == nil {
		t.Error("RestartCodes default should be non-nil slice")
	}
	if prog.NoRestartCodes == nil {
		t.Error("NoRestartCodes default should be non-nil slice")
	}
	if prog.HealthCheckInterval != 30 {
		t.Errorf("HealthCheckInterval default should be 30, got %d", prog.HealthCheckInterval)
	}
	if prog.HealthCheckTimeout != 5 {
		t.Errorf("HealthCheckTimeout default should be 5, got %d", prog.HealthCheckTimeout)
	}
	if prog.HealthCheckUnhealthyThreshold != 3 {
		t.Errorf("HealthCheckUnhealthyThreshold default should be 3, got %d", prog.HealthCheckUnhealthyThreshold)
	}
	if prog.CPUThresholdPercent != 90.0 {
		t.Errorf("CPUThresholdPercent default should be 90.0, got %f", prog.CPUThresholdPercent)
	}
	if prog.MemoryThresholdBytes != 2*1024*1024*1024 {
		t.Errorf("MemoryThresholdBytes default should be 2147483648, got %d", prog.MemoryThresholdBytes)
	}
	if prog.RestartWindowSecs != 60 {
		t.Errorf("RestartWindowSecs default should be 60, got %d", prog.RestartWindowSecs)
	}
	if prog.StdoutLogMaxBytes != 50*1024*1024 {
		t.Errorf("StdoutLogMaxBytes default should be 52428800, got %d", prog.StdoutLogMaxBytes)
	}
	if prog.StdoutLogBackupCount != 10 {
		t.Errorf("StdoutLogBackupCount default should be 10, got %d", prog.StdoutLogBackupCount)
	}
	if prog.StderrLogMaxBytes != 50*1024*1024 {
		t.Errorf("StderrLogMaxBytes default should be 52428800, got %d", prog.StderrLogMaxBytes)
	}
	if prog.StderrLogBackupCount != 10 {
		t.Errorf("StderrLogBackupCount default should be 10, got %d", prog.StderrLogBackupCount)
	}
	// NumProcs and ProcessName are only set by YAML/JSON applyDefaults,
	// not by the INI parser, so they remain at Go zero values.
	if prog.NumProcs != 0 {
		t.Errorf("NumProcs default from INI parser should be 0, got %d", prog.NumProcs)
	}
	if prog.ProcessName != "" {
		t.Errorf("ProcessName default from INI parser should be \"\", got %s", prog.ProcessName)
	}
	if prog.KillsAsGroup != false {
		t.Error("KillsAsGroup default should be false")
	}
	if prog.StopAsGroup != false {
		t.Error("StopAsGroup default should be false")
	}
	if prog.HealthCheckRestart != false {
		t.Error("HealthCheckRestart default should be false")
	}
}

// TestRoundTrip_NumProcsExpansion verifies process name expansion.
func TestRoundTrip_NumProcsExpansion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "numprocs.ini")

	ini := `[program:worker]
command=echo worker_%(process_num)d
process_name=%(program_name)s_%(process_num)02d
numprocs=3
`
	if err := os.WriteFile(configPath, []byte(ini), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Expand should create 3 instances
	expanded := ExpandProgramConfig(cfg.Programs["worker"])
	if len(expanded) != 3 {
		t.Fatalf("expected 3 expanded configs, got %d", len(expanded))
	}

	if expanded[0].Name != "worker_01" {
		t.Errorf("expected worker_01, got %s", expanded[0].Name)
	}
	if expanded[1].Name != "worker_02" {
		t.Errorf("expected worker_02, got %s", expanded[1].Name)
	}
	if expanded[2].Name != "worker_03" {
		t.Errorf("expected worker_03, got %s", expanded[2].Name)
	}

	// Verify command template was expanded too
	if expanded[0].Command != "echo worker_1" {
		t.Errorf("expected 'echo worker_1', got %s", expanded[0].Command)
	}
	if expanded[2].Command != "echo worker_3" {
		t.Errorf("expected 'echo worker_3', got %s", expanded[2].Command)
	}

	// Each expanded config should have NumProcs=1
	for i, ec := range expanded {
		if ec.NumProcs != 1 {
			t.Errorf("expanded[%d].NumProcs should be 1, got %d", i, ec.NumProcs)
		}
	}

	// Original should not be modified
	if cfg.Programs["worker"].NumProcs != 3 {
		t.Errorf("original NumProcs should remain 3, got %d", cfg.Programs["worker"].NumProcs)
	}
}

// TestRoundTrip_InetHttpServer verifies that the [inet_http_server] section
// maps fields correctly into ServerConfig.
func TestRoundTrip_InetHttpServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "inet_http.ini")

	ini := `[inet_http_server]
port=:8080
username=webadmin
password=webpass
cert=/tmp/server.crt
key=/tmp/server.key

[program:app]
command=echo ok
`
	if err := os.WriteFile(configPath, []byte(ini), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server == nil {
		t.Fatal("expected Server config")
	}
	if cfg.Server.WebAddr != ":8080" {
		t.Errorf("WebAddr: expected :8080, got %s", cfg.Server.WebAddr)
	}
	if cfg.Server.WebUser != "webadmin" {
		t.Errorf("WebUser: expected webadmin, got %s", cfg.Server.WebUser)
	}
	if cfg.Server.WebPass != "webpass" {
		t.Errorf("WebPass: expected webpass, got %s", cfg.Server.WebPass)
	}
	if cfg.Server.WebCert != "/tmp/server.crt" {
		t.Errorf("WebCert: expected /tmp/server.crt, got %s", cfg.Server.WebCert)
	}
	if cfg.Server.WebKey != "/tmp/server.key" {
		t.Errorf("WebKey: expected /tmp/server.key, got %s", cfg.Server.WebKey)
	}
}
