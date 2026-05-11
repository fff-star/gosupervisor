//go:build burnin

package process

import (
	"math/rand"
	"runtime"
	"testing"
	"time"

	"go.uber.org/goleak"

	"gosupervisor/internal/config"
	"gosupervisor/internal/logger"
)

// TestBurnIn runs a 5-hour stress test cycling through random start/stop/restart
// operations on multiple managed processes, checking for goroutine leaks, memory
// growth, and deadlocks.
//
// Run with:
//
//	go test -tags burnin -run TestBurnIn -timeout 5h ./internal/process/
func TestBurnIn(t *testing.T) {
	const duration = 5 * time.Hour
	const processCount = 6

	// --- Setup ---
	logDir := t.TempDir()
	logManager, err := logger.NewDefaultLogger(logDir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logManager.Close()

	pm := NewProcessManager(logManager)

	names := []string{"sleep", "echo", "cat", "shloop", "date", "yes"}
	for _, name := range names {
		var cmd string
		switch name {
		case "sleep":
			cmd = "sleep 60"
		case "echo":
			cmd = "echo 'burn-in test running'"
		case "cat":
			cmd = "cat /dev/null"
		case "shloop":
			cmd = "while true; do sleep 1; done"
		case "date":
			cmd = "date"
		case "yes":
			cmd = "yes | head -n 1000000 > /dev/null"
		}
		pm.AddProcess(&config.ProgramConfig{
			Name:         name,
			Command:      cmd,
			AutoStart:    false,
			AutoRestart:  false,
			StartSecs:    0,
			StartRetries: 3,
			StopSecs:     2,
			StopSignal:   "SIGTERM",
			Environment:  make(map[string]string),
		})
	}

	// Start monitor
	mon := NewMonitor(pm)
	mon.Start()

	startGoroutines := runtime.NumGoroutine()
	var memStatsStart runtime.MemStats
	runtime.ReadMemStats(&memStatsStart)

	t.Logf("Burn-in 测试启动: duration=%v, processes=%d, start_goroutines=%d",
		duration, processCount, startGoroutines)

	// --- Run ---
	startTime := time.Now()
	deadline := startTime.Add(duration)
	iteration := 0
	failures := 0
	stats := map[string]int{"start": 0, "stop": 0, "restart": 0, "kill": 0, "status": 0}

	for time.Now().Before(deadline) {
		iteration++
		name := names[rand.Intn(len(names))]
		action := rand.Intn(5) // 0=start, 1=stop, 2=restart, 3=kill, 4=status

		switch action {
		case 0:
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
		case 1:
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
		case 2:
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
		case 3:
			// Force kill: stop then restart
			p := pm.GetProcess(name)
			if p != nil {
				p.Stop()
				time.Sleep(100 * time.Millisecond)
				_ = p.Start()
			}
			stats["kill"]++
		case 4:
			// Read status (tests concurrent read safety)
			pm.RangeProcesses(func(n string, p *Process) {
				p.Snapshot()
			})
			stats["status"]++
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
	t.Logf("stats: start=%d stop=%d restart=%d kill=%d status=%d",
		stats["start"], stats["stop"], stats["restart"], stats["kill"], stats["status"])

	// Stop monitor first so it won't restart processes during shutdown
	mon.Stop()

	// Stop all remaining processes
	pm.StopAll()

	// Allow goroutines to settle (monitorResources ticker is 5s)
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
