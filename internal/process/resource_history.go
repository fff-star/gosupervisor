package process

import (
	"sync"
	"time"
)

// ResourceSample is a single CPU/memory reading.
type ResourceSample struct {
	Timestamp time.Time `json:"timestamp"`
	CPU       float64   `json:"cpu"`
	Memory    uint64    `json:"memory"`
}

// ResourceHistory is a ring buffer of resource samples for a process.
type ResourceHistory struct {
	mu       sync.Mutex
	samples  []ResourceSample
	capacity int
	pos      int
	full     bool
}

// NewResourceHistory creates a ring buffer for the given number of samples.
func NewResourceHistory(capacity int) *ResourceHistory {
	return &ResourceHistory{
		samples:  make([]ResourceSample, capacity),
		capacity: capacity,
	}
}

// Push adds a sample to the buffer.
func (rh *ResourceHistory) Push(s ResourceSample) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.samples[rh.pos] = s
	rh.pos = (rh.pos + 1) % rh.capacity
	if rh.pos == 0 {
		rh.full = true
	}
}

// Snapshot returns all samples in chronological order, optionally limited to a duration.
func (rh *ResourceHistory) Snapshot(since time.Duration) []ResourceSample {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	size := rh.pos
	if rh.full {
		size = rh.capacity
	}

	all := make([]ResourceSample, size)
	if rh.full {
		for i := 0; i < size; i++ {
			all[i] = rh.samples[(rh.pos+i)%rh.capacity]
		}
	} else {
		copy(all, rh.samples[:rh.pos])
	}

	if since <= 0 {
		return all
	}
	cutoff := time.Now().Add(-since)
	n := 0
	for n < len(all) && all[n].Timestamp.Before(cutoff) {
		n++
	}
	return all[n:]
}
