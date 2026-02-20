package logger

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRingBuffer_Basic(t *testing.T) {
	rb, err := NewRingBuffer(1024)
	if err != nil {
		t.Fatalf("failed to create: %v", err)
	}
	if rb.Cap() != 1024 {
		t.Fatalf("expected cap 1024, got %d", rb.Cap())
	}

	// Single Produce Single Consume
	count := 0
	done := make(chan struct{})

	go rb.Consume(func(events []LogEvent) {
		count += len(events)
		if count >= 100 {
			close(done)
		}
	})

	for i := 0; i < 100; i++ {
		ok := rb.TryWrite(LogEvent{Message: "msg"})
		if !ok {
			t.Fatalf("write failed at %d", i)
		}
	}

	select {
	case <-done:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for consumer")
	}
}

func TestRingBuffer_Concurrent(t *testing.T) {
	// Small buffer to force contentions and wraps
	rb, _ := NewRingBuffer(16)

	const producerCount = 4
	const messagesPerProducer = 1000
	const expectations = producerCount * messagesPerProducer

	var received int64
	done := make(chan struct{})

	// Consumer
	go rb.Consume(func(events []LogEvent) {
		n := len(events)
		newVal := atomic.AddInt64(&received, int64(n))
		if newVal >= int64(expectations) {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})

	var wg sync.WaitGroup
	wg.Add(producerCount)

	for i := 0; i < producerCount; i++ {
		go func(pid int) {
			defer wg.Done()
			for j := 0; j < messagesPerProducer; j++ {
				// Spin until accepted
				for !rb.TryWrite(LogEvent{Message: fmt.Sprintf("p%d-m%d", pid, j)}) {
					// busy wait
					runtime.Gosched()
				}
			}
		}(i)
	}

	wg.Wait()

	select {
	case <-done:
		if atomic.LoadInt64(&received) != int64(expectations) {
			t.Fatalf("missing messages: expected %d, got %d", expectations, received)
		}
	case <-time.After(3 * time.Second):
		t.Errorf("timeout: expected %d, got %d", expectations, atomic.LoadInt64(&received))
	}
}

func BenchmarkRingBuffer_Throughput(b *testing.B) {
	rb, _ := NewRingBuffer(4096)
	done := make(chan struct{})

	go func() {
		rb.Consume(func(events []LogEvent) {
			// No-op consumer
		})
		close(done)
	}()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		evt := LogEvent{Message: "bench"}
		for pb.Next() {
			for !rb.TryWrite(evt) {
				runtime.Gosched()
			}
		}
	})
	b.StopTimer()
	rb.Close()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		b.Fatal("timed out waiting for consumer shutdown")
	}
}

func BenchmarkRingBuffer_Contention(b *testing.B) {
	rb, _ := NewRingBuffer(256)
	done := make(chan struct{})

	go func() {
		rb.Consume(func(events []LogEvent) {
			// No-op consumer
		})
		close(done)
	}()

	b.SetParallelism(16)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		evt := LogEvent{Message: "contention"}
		for pb.Next() {
			for !rb.TryWrite(evt) {
				runtime.Gosched()
			}
		}
	})
	b.StopTimer()
	rb.Close()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		b.Fatal("timed out waiting for consumer shutdown")
	}
}

func BenchmarkRingBuffer_TryWriteFull(b *testing.B) {
	rb, _ := NewRingBuffer(2)
	if !rb.TryWrite(LogEvent{Message: "a"}) || !rb.TryWrite(LogEvent{Message: "b"}) {
		b.Fatal("failed to prefill ring buffer")
	}

	evt := LogEvent{Message: "full"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rb.TryWrite(evt) {
			b.Fatal("unexpected successful write on full ring buffer")
		}
	}
}

func TestRingBuffer_BackoffProgression(t *testing.T) {
	retries := 0
	for i := 0; i < producerSpinLimit+4; i++ {
		backoffCAS(&retries)
	}
	if retries != producerSpinLimit+4 {
		t.Fatalf("unexpected retries count: got %d", retries)
	}

	retries = 0
	for i := 0; i < slotSpinLimit+4; i++ {
		backoffSlot(&retries)
	}
	if retries != slotSpinLimit+4 {
		t.Fatalf("unexpected slot retries count: got %d", retries)
	}
}

func TestRingBuffer_ConsumePanicHandler(t *testing.T) {
	rb, err := NewRingBuffer(8)
	if err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	panicCh := make(chan any, 1)
	rb.PanicHandler = func(v any) {
		select {
		case panicCh <- v:
		default:
		}
	}

	done := make(chan struct{})
	go func() {
		rb.Consume(func(events []LogEvent) {
			panic("boom")
		})
		close(done)
	}()

	if !rb.TryWrite(LogEvent{Message: "panic"}) {
		t.Fatal("expected write to succeed")
	}

	select {
	case v := <-panicCh:
		if v == nil {
			t.Fatal("expected panic value")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for panic handler")
	}

	rb.Close()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for consumer to finish")
	}
}

func TestNewRingBuffer_Validation(t *testing.T) {
	if _, err := NewRingBuffer(0); err == nil {
		t.Fatalf("expected error for zero capacity")
	}
	if _, err := NewRingBuffer(3); err == nil {
		t.Fatalf("expected error for non-power-of-two capacity")
	}
	maxInt := ^uint(0) >> 1
	if _, err := NewRingBuffer(uint64(maxInt) + 1); err == nil {
		t.Fatalf("expected error for oversized capacity")
	}
}

func TestRingBuffer_TryWrite_FullAndClosed(t *testing.T) {
	rb, err := NewRingBuffer(2)
	if err != nil {
		t.Fatalf("failed to create ring buffer: %v", err)
	}
	if ok := rb.TryWrite(LogEvent{Message: "a"}); !ok {
		t.Fatalf("expected first write to succeed")
	}
	if ok := rb.TryWrite(LogEvent{Message: "b"}); !ok {
		t.Fatalf("expected second write to succeed")
	}
	if ok := rb.TryWrite(LogEvent{Message: "c"}); ok {
		t.Fatalf("expected write to fail when buffer is full")
	}

	rb.Close()
	if ok := rb.TryWrite(LogEvent{Message: "d"}); ok {
		t.Fatalf("expected write to fail after close")
	}
}
