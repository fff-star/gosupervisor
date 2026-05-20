package process

import "sync"

// RingBuffer is a generic thread-safe ring buffer.
type RingBuffer[T any] struct {
	mu       sync.Mutex
	buf      []T
	capacity int
	pos      int
	full     bool
}

// NewRingBuffer creates a new ring buffer with the given capacity.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	return &RingBuffer[T]{
		buf:      make([]T, capacity),
		capacity: capacity,
	}
}

// Push adds a value to the buffer.
func (rb *RingBuffer[T]) Push(v T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.buf[rb.pos] = v
	rb.pos = (rb.pos + 1) % rb.capacity
	if rb.pos == 0 {
		rb.full = true
	}
}

// Len returns the number of elements currently in the buffer.
func (rb *RingBuffer[T]) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.full {
		return rb.capacity
	}
	return rb.pos
}

// snapshotAll returns all elements in chronological order (oldest first).
// Caller must hold rb.mu.
func (rb *RingBuffer[T]) snapshotAll() []T {
	size := rb.pos
	if rb.full {
		size = rb.capacity
	}
	result := make([]T, size)
	if rb.full {
		for i := 0; i < size; i++ {
			result[i] = rb.buf[(rb.pos+i)%rb.capacity]
		}
	} else {
		copy(result, rb.buf[:rb.pos])
	}
	return result
}

// snapshotLast returns the most recent n elements.
// Caller must hold rb.mu.
func (rb *RingBuffer[T]) snapshotLast(n int) []T {
	size := rb.pos
	if rb.full {
		size = rb.capacity
	}
	if n > size {
		n = size
	}
	result := make([]T, n)
	for i := 0; i < n; i++ {
		idx := (rb.pos - n + i + rb.capacity) % rb.capacity
		result[i] = rb.buf[idx]
	}
	return result
}
