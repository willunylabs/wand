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

	producerSpinLimit = 8
	slotSpinLimit     = 8
	maxBackoffShift   = 10
)

// RingBuffer is a high-performance lock-free ring buffer (MPSC).
// Cache padding prevents false sharing.
type RingBuffer struct {
	// Producer Index (Atomic)
	// Producers contend on this index to reserve slots.
	head uint64
	_    [56]byte // [Padding]: Ensures head is on its own cache line (64 bytes) to prevent "False Sharing".
	// If head and tail were on the same cache line, a write to 'head' by a producer would invalid
	// the cache line for the consumer reading 'tail', causing severe performance degradation.

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

	// PanicHandler is invoked if the consumer handler panics.
	// If nil, the panic is rethrown to avoid silent data loss.
	PanicHandler func(any)
}

// NewRingBuffer creates a ring buffer with the given capacity.
// capacity must be a power of two.
func NewRingBuffer(capacity uint64) (*RingBuffer, error) {
	if capacity == 0 || (capacity&(capacity-1)) != 0 {
		return nil, fmt.Errorf("capacity must be power of 2")
	}
	maxInt := ^uint(0) >> 1
	if capacity > uint64(maxInt) {
		return nil, fmt.Errorf("capacity too large")
	}

	// #nosec G115 -- bounds checked above
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
//
// [Algorithm: MPSC Lock-Free]
// 1. Load Head & Tail to check capacity (loose check).
// 2. CAS(head, old, old+1) to reserve a slot.
// 3. If CAS succeeds, we own the slot. Check slot state to ensure previous consumer is done.
// 4. Write data.
// 5. Commit by setting state to 'Ready'.
func (rb *RingBuffer) TryWrite(event LogEvent) bool {
	if atomic.LoadUint32(&rb.closed) != 0 {
		return false
	}
	retries := 0
	for {
		head := atomic.LoadUint64(&rb.head)
		tail := atomic.LoadUint64(&rb.tail)

		if head-tail >= rb.Cap() {
			return false // Buffer Full, drop log
		}

		if atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
			// Success reserving slot `head`
			slotIdx := head & rb.mask

			slotRetries := 0
			for atomic.LoadUint32(&rb.state[slotIdx]) != slotEmpty {
				backoffSlot(&slotRetries)
			}

			// Mark as Writing
			atomic.StoreUint32(&rb.state[slotIdx], slotWriting)

			// Write Data
			rb.data[slotIdx] = event

			// Commit (Ready)
			atomic.StoreUint32(&rb.state[slotIdx], slotReady)
			return true
		}
		// CAS failed, retry with bounded spin and exponential backoff.
		backoffCAS(&retries)
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
				// Batch consumption with proper wrap-around handling
				nextSlot := slotIdx
				endSlot := (curr + available) & rb.mask

				if endSlot > nextSlot {
					// Contiguous: no wrap-around
					consumeBatch(handler, rb.PanicHandler, rb.data[nextSlot:endSlot])
				} else if endSlot < nextSlot {
					// Wrap-around: spans end of buffer and beginning
					consumeBatch(handler, rb.PanicHandler, rb.data[nextSlot:])
					if endSlot > 0 {
						consumeBatch(handler, rb.PanicHandler, rb.data[:endSlot])
					}
				} else {
					// endSlot == nextSlot: full buffer wrap (available == capacity)
					// This means we need to consume from nextSlot to end, then 0 to nextSlot
					consumeBatch(handler, rb.PanicHandler, rb.data[nextSlot:])
					if nextSlot > 0 {
						consumeBatch(handler, rb.PanicHandler, rb.data[:nextSlot])
					}
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

func consumeBatch(handler func([]LogEvent), panicHandler func(any), batch []LogEvent) {
	defer func() {
		if rec := recover(); rec != nil {
			if panicHandler != nil {
				panicHandler(rec)
				return
			}
			panic(rec)
		}
	}()
	handler(batch)
}

func backoffCAS(retries *int) {
	backoffWithLimit(retries, producerSpinLimit)
}

func backoffSlot(retries *int) {
	backoffWithLimit(retries, slotSpinLimit)
}

func backoffWithLimit(retries *int, spinLimit int) {
	if *retries < spinLimit {
		runtime.Gosched()
		*retries = *retries + 1
		return
	}
	shift := *retries - spinLimit
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	time.Sleep(time.Microsecond << shift)
	*retries = *retries + 1
}
