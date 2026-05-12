//go:build burnin

package process

import (
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

func init() {
	Logf = func(format string, args ...interface{}) {}
}

// TestBurnIn runs a 5-hour stress test cycling through random start/stop/restart
// operations on multiple managed processes, checking for goroutine leaks, memory
// growth, and deadlocks.
//
// Run with:
//
//	go test -tags burnin -run TestBurnIn -timeout 5h ./internal/process/
func TestBurnIn(t *testing.T) {
	duration := 5 * time.Hour
	if s := os.Getenv("BURNIN_TIME"); s != "" {
		s = strings.TrimSuffix(s, "h")
		if h, err := strconv.Atoi(s); err == nil && h > 0 {
			duration = time.Duration(h) * time.Hour
		}
	}
	const processCount = 7

	// --- Setup ---
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	type procDef struct {
		name, cmd, group string
		healthCheck      string
		webhook          string
		autoRestart      bool
		restartMaxCount  int
	}
	procs := []procDef{
		{name: "sleep", cmd: "sleep 60", group: "g1"},
		{name: "echo", cmd: "echo 'burn-in test running'", group: "g1"},
		{name: "cat", cmd: "cat /dev/null", group: "g2"},
		{name: "shloop", cmd: "while true; do sleep 1; done", group: "g2"},
		{name: "date", cmd: "date", group: ""},
		{name: "yes", cmd: "yes | head -n 1000000 > /dev/null", group: ""},
		{
			name:            "healthcheck",
			cmd:             "while true; do sleep 1; done",
			group:           "g3",
			healthCheck:     "http://127.0.0.1:19999/health",
			webhook:         "http://127.0.0.1:19999/webhook",
			autoRestart:     true,
			restartMaxCount: 2,
		},
	}
	names := make([]string, len(procs))
	groups := make(map[string][]string)
	for i, d := range procs {
		names[i] = d.name
		if d.group != "" {
			groups[d.group] = append(groups[d.group], d.name)
		}
		pm.AddProcess(&config.ProgramConfig{
			Name:             d.name,
			Command:          d.cmd,
			AutoStart:        false,
			AutoRestart:      d.autoRestart,
			StartSecs:        0,
			StartRetries:     3,
			StopSecs:         2,
			StopSignal:       "SIGTERM",
			Group:            d.group,
			HealthCheckURL:                d.healthCheck,
			HealthCheckInterval:           5,
			HealthCheckTimeout:            1,
			HealthCheckUnhealthyThreshold: 2,
			HealthCheckRestart:            true,
			WebhookURL:                    d.webhook,
			WebhookTimeout:                1,
			WebhookRetries:                1,
			RestartWindowSecs:             60,
			RestartMaxCount:               d.restartMaxCount,
			Environment:      make(map[string]string),
		})
	}

	// Start monitor
	mon := NewMonitor(pm)
	mon.Start()

	startGoroutines := runtime.NumGoroutine()
	var memStatsStart runtime.MemStats
	runtime.ReadMemStats(&memStatsStart)

	t.Logf("Burn-in 测试启动: duration=%v, processes=%d, groups=%d, start_goroutines=%d",
		duration, processCount, len(groups), startGoroutines)

	// --- Run ---
	startTime := time.Now()
	deadline := startTime.Add(duration)
	iteration := 0
	failures := 0
	stats := map[string]int{
		"start": 0, "stop": 0, "restart": 0, "kill": 0, "status": 0,
		"signal": 0, "group": 0, "event": 0, "resource": 0,
	}

	for time.Now().Before(deadline) {
		iteration++
		action := rand.Intn(9) // 0-8

		switch action {
		case 0: // start
			name := names[rand.Intn(len(names))]
			p := pm.GetProcess(name)
			if p != nil {
				st := p.GetState()
				if st == StateStopped || st == StateExited || st == StateFatal {
					if err := p.Start(); err != nil {
						failures++
					}
				}
			}
			stats["start"]++
		case 1: // stop
			name := names[rand.Intn(len(names))]
			p := pm.GetProcess(name)
			if p != nil {
				st := p.GetState()
				if st == StateRunning || st == StateStarting {
					if err := p.Stop(); err != nil {
						failures++
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
			stats["stop"]++
		case 2: // restart
			name := names[rand.Intn(len(names))]
			p := pm.GetProcess(name)
			if p != nil {
				st := p.GetState()
				if st == StateRunning {
					if err := p.Restart(); err != nil {
						failures++
					}
				}
			}
			stats["restart"]++
		case 3: // kill (stop + restart)
			name := names[rand.Intn(len(names))]
			p := pm.GetProcess(name)
			if p != nil {
				p.Stop()
				time.Sleep(100 * time.Millisecond)
				_ = p.Start()
			}
			stats["kill"]++
		case 4: // status (tests concurrent read safety)
			pm.RangeProcesses(func(n string, p *Process) {
				p.Snapshot()
			})
			stats["status"]++
		case 5: // signal
			name := names[rand.Intn(len(names))]
			p := pm.GetProcess(name)
			if p != nil && p.GetState() == StateRunning {
				sigs := []string{"SIGHUP", "SIGUSR1"}
				sig, _ := ParseSignal(sigs[rand.Intn(len(sigs))])
				_ = p.Signal(sig)
			}
			stats["signal"]++
		case 6: // group operation
			if len(groups) > 0 {
				groupNames := make([]string, 0, len(groups))
				for g := range groups {
					groupNames = append(groupNames, g)
				}
				g := groupNames[rand.Intn(len(groupNames))]
				switch rand.Intn(3) {
				case 0:
					pm.StartGroup(g)
				case 1:
					pm.StopGroup(g)
				case 2:
					pm.RestartGroup(g)
				}
			}
			stats["group"]++
		case 7: // event buffer snapshot (concurrent read safety)
			_ = GlobalEventBuffer.Snapshot(50)
			stats["event"]++
		case 8: // resource history snapshot (concurrent read safety)
			name := names[rand.Intn(len(names))]
			p := pm.GetProcess(name)
			if p != nil && p.ResourceHistory != nil {
				_ = p.ResourceHistory.Snapshot(5 * time.Minute)
			}
			stats["resource"]++
		}

		// Every 100 iterations, check health
		if iteration%100 == 0 {
			goroutinesNow := runtime.NumGoroutine()
			var memStatsNow runtime.MemStats
			runtime.ReadMemStats(&memStatsNow)

			memDelta := int64(memStatsNow.HeapAlloc) - int64(memStatsStart.HeapAlloc)
			gorDelta := goroutinesNow - startGoroutines

			t.Logf("iter=%d elapsed=%v goroutines=%d (Δ=%+d) heap=%d KB (Δ=%+d KB) failures=%d",
				iteration, time.Since(startTime).Round(time.Second), goroutinesNow, gorDelta,
				memStatsNow.HeapAlloc/1024, memDelta/1024, failures)

			// Hard limits: goroutines shouldn't grow unbounded
			if goroutinesNow > startGoroutines+50 {
				t.Errorf("goroutine 泄漏: started=%d current=%d", startGoroutines, goroutinesNow)
			}

			// Memory shouldn't grow beyond 100MB
			if memStatsNow.HeapAlloc > 100*1024*1024 {
				t.Errorf("内存使用过高: %d KB", memStatsNow.HeapAlloc/1024)
			}
		}

		// Random sleep between actions (1-200ms)
		time.Sleep(time.Duration(rand.Intn(200)+1) * time.Millisecond)
	}

	// --- Final checks ---
	t.Logf("Burn-in 完成: iterations=%d elapsed=%v failures=%d",
		iteration, time.Since(startTime).Round(time.Second), failures)
	t.Logf("stats: start=%d stop=%d restart=%d kill=%d status=%d signal=%d group=%d event=%d resource=%d",
		stats["start"], stats["stop"], stats["restart"], stats["kill"], stats["status"],
		stats["signal"], stats["group"], stats["event"], stats["resource"])

	// Stop monitor first so it won't restart processes during shutdown
	mon.Stop()

	// Stop all remaining processes
	pm.StopAll()

	// Allow goroutines to settle (monitorResources ticker is 5s, webhook retry may be in-flight)
	time.Sleep(6 * time.Second)

	// Check for goroutine leaks
	goroutinesEnd := runtime.NumGoroutine()
	var memStatsEnd runtime.MemStats
	runtime.ReadMemStats(&memStatsEnd)

	t.Logf("end: goroutines=%d heap=%d KB", goroutinesEnd, memStatsEnd.HeapAlloc/1024)

	if goroutinesEnd > startGoroutines+10 {
		t.Errorf("测试结束后仍有过多 goroutine: started=%d end=%d", startGoroutines, goroutinesEnd)
	}

	// goleak final check
	goleak.VerifyNone(t)
}
