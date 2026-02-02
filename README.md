# Wand 🪄

[![CI](https://github.com/willuny-labs/wand/actions/workflows/ci.yml/badge.svg)](https://github.com/willuny-labs/wand/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/willuny-labs/wand)](https://goreportcard.com/report/github.com/willuny-labs/wand)
[![Go Reference](https://pkg.go.dev/badge/github.com/willuny-labs/wand.svg)](https://pkg.go.dev/github.com/willuny-labs/wand)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**High-Performance, Zero-Allocation HTTP Router for Go**

`wand` is a minimalist, infrastructure-grade HTTP router and toolkit designed for services where latency and memory efficiency are critical. It features a lock-free design, zero-allocation routing paths, and effective DoS protection.

> **Wand** /wɒnd/ - A symbol of magic and control. Elegantly directing traffic with precision and speed.

## Features

- **Zero Allocation**: Optimized hot paths (static, dynamic, and wildcard routes) generate **0 bytes** of garbage per request.
- **High Performance**: 
    - **Static Routes**: ~35ns
    - **Dynamic Routes**: ~100ns
- **DoS Protection**: Built-in limits for `MaxPathLength` (4096) and `MaxDepth` (50) to prevent algorithmic complexity attacks.
- **Frozen Mode**: Innovative `FrozenRouter` flattens static path segments for extreme read-heavy performance.
- **Lock-Free Logger**: Specific high-throughput `RingBuffer` logger implementation.
- **Minimalist Middleware**: Includes essential middlewares (Recovery, RequestID, AccessLog, Timeout, BodySizeLimit).

## Installation

```bash
go get github.com/willuny-labs/wand
```

## Quick Start

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/willuny-labs/wand/logger"
	"github.com/willuny-labs/wand/middleware"
	"github.com/willuny-labs/wand/router"
	"github.com/willuny-labs/wand/server"
)

func main() {
	// Create a new router
	r := router.NewRouter()
	
	// Register routes
	_ = r.GET("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	_ = r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := router.Param(w, "id")
		w.Write([]byte("User ID: " + id))
	})

	// Setup high-performance logger
	rb, _ := logger.NewRingBuffer(1024)
	go rb.Consume(func(events []logger.LogEvent) {
		// batch process logs
	})

	// Compose middleware
	h := middleware.Recovery(r)
	h = middleware.RequestID(h)
	h = middleware.BodySizeLimit(1<<20, h)
	h = middleware.Timeout(5*time.Second, h)
	h = middleware.AccessLog(rb, h)

	// Start graceful server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: h,
	}

	ctx, cancel := server.SignalContext(context.Background())
	defer cancel()
	_ = server.Run(ctx, srv, 5*time.Second)
}
```

## Components

- `router`: Path-Segment Trie based, zero-alloc HTTP router.
- `middleware`: Essential HTTP middlewares.
- `logger`: Lock-free ring buffer logger for high-throughput scenarios.
- `server`: Helpers for graceful server shutdown.

## Performance

### GitHub API Benchmark

Comparative results for the [GitHub API routing benchmark](https://github.com/smallnest/go-web-framework-benchmark).
Run on Apple M4 Pro (Go 1.25).

| Benchmark name | (1) | (2) | (3) | (4) |
| :--- | :--- | :--- | :--- | :--- |
| **BenchmarkGin_GithubAll** | 143499 | 8386 ns/op | 0 B/op | 0 allocs/op |
| **BenchmarkHttpRouter_GithubAll** | 127113 | 9165 ns/op | 13792 B/op | 167 allocs/op |
| **BenchmarkEcho_GithubAll** | 118155 | 10437 ns/op | 0 B/op | 0 allocs/op |
| **BenchmarkWand_GithubAll** | 46750 | 24595 ns/op | 0 B/op | 0 allocs/op |
| **BenchmarkChi_GithubAll** | 24181 | 49981 ns/op | 130904 B/op | 740 allocs/op |
| **BenchmarkGorillaMux_GithubAll** | 1062 | 1138474 ns/op | 225922 B/op | 1588 allocs/op |

> **(1)**: Total Repetitions (higher is better)
> **(2)**: Latency (ns/op) (lower is better)
> **(3)**: Heap Memory (B/op) (lower is better)
> **(4)**: Allocations (allocs/op) (lower is better)


## License

MIT
