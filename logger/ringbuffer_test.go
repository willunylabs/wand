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

	go rb.Consume(func(events []LogEvent) {
		// No-op consumer
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		evt := LogEvent{Message: "bench"}
		for pb.Next() {
			for !rb.TryWrite(evt) {
				runtime.Gosched()
			}
		}
	})
}
