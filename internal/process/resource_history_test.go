package process

import (
	"sync"
	"testing"
	"time"
)

func TestNewResourceHistory(t *testing.T) {
	rh := NewResourceHistory(30)
	if rh == nil {
		t.Fatal("NewResourceHistory returned nil")
	}
	if rh.capacity != 30 {
		t.Errorf("expected capacity 30, got %d", rh.capacity)
	}
	if rh.pos != 0 {
		t.Errorf("expected pos 0, got %d", rh.pos)
	}
	if rh.full {
		t.Error("new buffer should not be full")
	}
}

func TestResourceHistoryPushAndSnapshot(t *testing.T) {
	rh := NewResourceHistory(10)

	now := time.Now()
	samples := []ResourceSample{
		{Timestamp: now.Add(-9 * time.Second), CPU: 10.0, Memory: 1024},
		{Timestamp: now.Add(-8 * time.Second), CPU: 20.0, Memory: 2048},
		{Timestamp: now.Add(-7 * time.Second), CPU: 30.0, Memory: 3072},
	}

	for _, s := range samples {
		rh.Push(s)
	}

	result := rh.Snapshot(0)
	if len(result) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(result))
	}
	if result[0].CPU != 10.0 {
		t.Errorf("first sample CPU should be 10.0, got %f", result[0].CPU)
	}
	if result[2].Memory != 3072 {
		t.Errorf("last sample Memory should be 3072, got %d", result[2].Memory)
	}
}

func TestResourceHistoryOverflow(t *testing.T) {
	rh := NewResourceHistory(5)

	now := time.Now()
	for i := 0; i < 10; i++ {
		rh.Push(ResourceSample{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			CPU:       float64(i),
			Memory:    uint64(i * 100),
		})
	}

	if !rh.full {
		t.Error("buffer should be full after overflow")
	}

	result := rh.Snapshot(0)
	if len(result) != 5 {
		t.Fatalf("expected 5 samples, got %d", len(result))
	}
	// Oldest sample should be i=5 (the 6th pushed)
	if result[0].CPU != 5.0 {
		t.Errorf("oldest CPU should be 5.0, got %f", result[0].CPU)
	}
	// Newest sample should be i=9
	if result[4].CPU != 9.0 {
		t.Errorf("newest CPU should be 9.0, got %f", result[4].CPU)
	}
}

func TestResourceHistorySnapshotWithSince(t *testing.T) {
	rh := NewResourceHistory(60)

	now := time.Now()
	// Push samples spanning 12 minutes
	for i := 0; i < 12; i++ {
		rh.Push(ResourceSample{
			Timestamp: now.Add(-time.Duration(12-i) * time.Minute),
			CPU:       float64(i),
			Memory:    100,
		})
	}

	// Get last 5 minutes
	result := rh.Snapshot(5 * time.Minute)
	if len(result) == 0 {
		t.Fatal("expected some samples within 5 minutes")
	}
	for _, s := range result {
		if s.Timestamp.Before(now.Add(-5 * time.Minute)) {
			t.Errorf("sample at %v is older than 5 minute cutoff", s.Timestamp)
		}
	}
}

func TestResourceHistorySnapshotZeroDuration(t *testing.T) {
	rh := NewResourceHistory(10)
	now := time.Now()
	for i := 0; i < 5; i++ {
		rh.Push(ResourceSample{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			CPU:       float64(i),
			Memory:    100,
		})
	}

	result := rh.Snapshot(0)
	if len(result) != 5 {
		t.Errorf("Snapshot(0) should return all samples, got %d", len(result))
	}
}

func TestResourceHistorySnapshotNegativeDuration(t *testing.T) {
	rh := NewResourceHistory(10)
	rh.Push(ResourceSample{Timestamp: time.Now(), CPU: 1.0, Memory: 100})

	result := rh.Snapshot(-1 * time.Minute)
	if len(result) != 1 {
		t.Errorf("Snapshot(-duration) should return all samples, got %d", len(result))
	}
}

func TestResourceHistoryConcurrent(t *testing.T) {
	rh := NewResourceHistory(500)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				rh.Push(ResourceSample{
					Timestamp: time.Now(),
					CPU:       float64(i),
					Memory:    uint64(i),
				})
			}
		}()
	}
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = rh.Snapshot(0)
				_ = rh.Snapshot(5 * time.Minute)
			}
		}()
	}

	wg.Wait()

	// Final snapshot should be consistent
	result := rh.Snapshot(0)
	if len(result) > 500 {
		t.Errorf("snapshot larger than capacity: %d", len(result))
	}
}

func TestResourceHistoryEmptySnapshot(t *testing.T) {
	rh := NewResourceHistory(10)
	result := rh.Snapshot(0)
	if len(result) != 0 {
		t.Errorf("empty buffer snapshot should be empty, got %d", len(result))
	}
}

func TestResourceHistoryFullCycle(t *testing.T) {
	rh := NewResourceHistory(3)

	// Fill exactly
	rh.Push(ResourceSample{Timestamp: time.Now(), CPU: 1, Memory: 10})
	rh.Push(ResourceSample{Timestamp: time.Now(), CPU: 2, Memory: 20})
	rh.Push(ResourceSample{Timestamp: time.Now(), CPU: 3, Memory: 30})

	if !rh.full {
		t.Error("buffer should be full after pos wraps to 0 at capacity")
	}

	// One more push causes wrap
	rh.Push(ResourceSample{Timestamp: time.Now(), CPU: 4, Memory: 40})

	if !rh.full {
		t.Error("buffer should be full after wrap")
	}

	result := rh.Snapshot(0)
	if len(result) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(result))
	}
	if result[0].CPU != 2 {
		t.Errorf("oldest after wrap should be CPU=2 (original 2nd element), got %f", result[0].CPU)
	}
}
