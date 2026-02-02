package logger

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
)

// LogEvent represents a log entry.
type LogEvent struct {
	Level     uint8
	Timestamp int64
	Message   string

	Method        string
	Path          string
	Status        uint16
	Bytes         int64
	DurationNanos int64
	RemoteAddr    string
}

const (
	slotEmpty   = 0
	slotWriting = 1
	slotReady   = 2
)

// RingBuffer is a high-performance lock-free ring buffer (MPSC).
// Cache padding prevents false sharing.
type RingBuffer struct {
	// Producer Index (Atomic)
	// Producers contend on this index to reserve slots.
	head uint64
	_    [56]byte // Padding: Ensures head is on its own cache line.

	// Consumer Index (Atomic)
	// Consumer updates this index; producers read it to check for full.
	tail uint64
	_    [56]byte // Padding: Ensures tail is on its own cache line.

	mask uint64     // cap - 1 (fast modulo)
	data []LogEvent // preallocated event array

	// properties for slot state to ensure data integrity in MPSC
	// 0: Empty, 1: Writing, 2: Ready
	state []uint32

	closed uint32
}

// NewRingBuffer creates a ring buffer with the given capacity.
// capacity must be a power of two.
func NewRingBuffer(capacity uint64) (*RingBuffer, error) {
	if capacity == 0 || (capacity&(capacity-1)) != 0 {
		return nil, fmt.Errorf("capacity must be power of 2")
	}
	maxInt := int(^uint(0) >> 1)
	if capacity > uint64(maxInt) {
		return nil, fmt.Errorf("capacity too large")
	}

	capInt := int(capacity)
	return &RingBuffer{
		mask:  capacity - 1,
		data:  make([]LogEvent, capInt),
		state: make([]uint32, capInt),
	}, nil
}

// Cap returns capacity.
func (rb *RingBuffer) Cap() uint64 {
	return rb.mask + 1
}

// Close marks the buffer as closed.
// Producers should stop before calling Close.
func (rb *RingBuffer) Close() {
	atomic.StoreUint32(&rb.closed, 1)
}

// TryWrite attempts to write a log event into the buffer.
// Returns false if the buffer is full (strategy: drop).
// This is lock-free and thread-safe for multiple producers.
func (rb *RingBuffer) TryWrite(event LogEvent) bool {
	if atomic.LoadUint32(&rb.closed) != 0 {
		return false
	}
	for {
		head := atomic.LoadUint64(&rb.head)
		tail := atomic.LoadUint64(&rb.tail)

		if head-tail >= rb.Cap() {
			return false // Buffer Full, drop log
		}

		if atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
			// Success reserving slot `head`
			slotIdx := head & rb.mask

			for atomic.LoadUint32(&rb.state[slotIdx]) != slotEmpty {
				runtime.Gosched() // Prevent starvation
			}

			// Mark as Writing
			atomic.StoreUint32(&rb.state[slotIdx], slotWriting)

			// Write Data
			rb.data[slotIdx] = event

			// Commit (Ready)
			atomic.StoreUint32(&rb.state[slotIdx], slotReady)
			return true
		}
		// CAS failed, retry
		runtime.Gosched() // Allow other goroutines to progress
	}
}

// Consume processes events in batch.
// handler is called with a batch of events.
// This function blocks and runs until Close is called and the buffer is drained.
// The handler must not retain the slice beyond the call.
// Usage: go rb.Consume(handler)
func (rb *RingBuffer) Consume(handler func([]LogEvent)) {
	curr := atomic.LoadUint64(&rb.tail)
	idle := 0
	for {
		if atomic.LoadUint32(&rb.closed) != 0 {
			if atomic.LoadUint64(&rb.tail) == atomic.LoadUint64(&rb.head) {
				return
			}
		}
		// Calculate target slot
		slotIdx := curr & rb.mask

		// Check if data is ready
		if atomic.LoadUint32(&rb.state[slotIdx]) == slotReady {
			idle = 0

			batchLimit := rb.Cap()
			if batchLimit > 128 {
				batchLimit = 128
			}
			available := uint64(0)
			// We can peek ahead
			for i := uint64(0); i < batchLimit; i++ {
				idx := (curr + i) & rb.mask
				if atomic.LoadUint32(&rb.state[idx]) == slotReady {
					available++
				} else {
					break
				}
			}

			if available > 0 {
				// Case A: No wrap-around in batch
				nextSlot := slotIdx
				endSlot := (curr + available) & rb.mask

				if endSlot > nextSlot {
					// Contiguous
					consumeBatch(handler, rb.data[nextSlot:endSlot])
				} else {
					// Wraps around
					consumeBatch(handler, rb.data[nextSlot:rb.Cap()])
					consumeBatch(handler, rb.data[0:endSlot])
				}

				// Commit consumption
				for i := uint64(0); i < available; i++ {
					idx := (curr + i) & rb.mask
					atomic.StoreUint32(&rb.state[idx], slotEmpty)
				}

				curr += available
				atomic.StoreUint64(&rb.tail, curr)
			}
		} else {
			if atomic.LoadUint32(&rb.state[slotIdx]) == slotWriting {
				runtime.Gosched()
				continue
			}
			idle++
			if idle < 10 {
				runtime.Gosched()
			} else {
				shift := idle - 10
				if shift > 10 {
					shift = 10
				}
				time.Sleep(time.Microsecond << shift)
			}
		}
	}
}

func consumeBatch(handler func([]LogEvent), batch []LogEvent) {
	defer func() {
		_ = recover()
	}()
	handler(batch)
}
