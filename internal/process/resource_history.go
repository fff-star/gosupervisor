package process

import (
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
	*RingBuffer[ResourceSample]
}

// NewResourceHistory creates a ring buffer for the given number of samples.
func NewResourceHistory(capacity int) *ResourceHistory {
	return &ResourceHistory{RingBuffer: NewRingBuffer[ResourceSample](capacity)}
}

// Snapshot returns all samples in chronological order, optionally limited to a duration.
func (rh *ResourceHistory) Snapshot(since time.Duration) []ResourceSample {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	all := rh.snapshotAll()
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
