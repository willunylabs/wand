# RingBuffer CPU Profile (2026-02-20)

This report captures before/after data for the `TryWrite` backoff changes.

## Commands

```bash
# before (HEAD baseline)
go test ./logger -run '^$' -bench '^BenchmarkRingBuffer(Throughput|Contention|TryWriteFull)$' -benchtime=3s -benchmem -cpuprofile /tmp/ringbuffer_before.cpu

# after (working tree)
go test ./logger -run '^$' -bench '^BenchmarkRingBuffer(Throughput|Contention|TryWriteFull)$' -benchtime=3s -benchmem -cpuprofile /tmp/ringbuffer_after.cpu

# cpu top
go tool pprof -top /tmp/ringbuffer_before.cpu
go tool pprof -top /tmp/ringbuffer_after.cpu
```

## Benchmark Summary

| Benchmark | Before | After | Delta |
| :--- | ---: | ---: | ---: |
| `BenchmarkRingBufferThroughput` | `46.61 ns/op` | `45.51 ns/op` | `-2.36%` |
| `BenchmarkRingBufferContention` | `255.5 ns/op` | `262.0 ns/op` | `+2.54%` |
| `BenchmarkRingBufferTryWriteFull` | `2.114 ns/op` | `2.149 ns/op` | `+1.66%` |

All three paths remain `0 B/op, 0 allocs/op`.

## CPU Top (Key Deltas)

- `runtime.usleep`: `60.95%` -> `60.58%` (stable dominant wait)
- `github.com/willunylabs/wand/logger.(*RingBuffer).TryWrite` flat: `5.61%` -> `5.59%` (stable)
- `runtime.goschedImpl` cumulative: `93.01%` -> `92.83%` (slightly lower)

Interpretation: contention profile remains dominated by scheduler/wait behavior; the new backoff split keeps throughput neutral-to-better while staying within the regression budget.
