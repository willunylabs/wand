# Wand 🪄

[![CI](https://github.com/WillunyLabs-LLC/wand/actions/workflows/ci.yml/badge.svg)](https://github.com/WillunyLabs-LLC/wand/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/WillunyLabs-LLC/wand)](https://goreportcard.com/report/github.com/WillunyLabs-LLC/wand)
[![Go Reference](https://pkg.go.dev/badge/github.com/WillunyLabs-LLC/wand.svg)](https://pkg.go.dev/github.com/WillunyLabs-LLC/wand)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**High-Performance, Zero-Allocation HTTP Router for Go**

`wand` is a minimalist, infrastructure-grade HTTP router and toolkit designed for services where latency and memory efficiency are critical. It features a lock-free design, zero-allocation routing paths, and effective DoS protection.

> **Wand** /wɒnd/ - A symbol of magic and control. Elegantly directing traffic with precision and speed.

## Features

- **Zero Allocation**: Optimized hot paths (static, dynamic, and wildcard routes) generate **0 bytes** of garbage per request.
- **Zero-Alloc Param Extraction**: Params are captured via pooled slices and index offsets—no maps or context allocations.
- **High Performance**: 
    - **Static Routes**: ~35ns
    - **Dynamic Routes**: ~100ns
- **DoS Protection**: Built-in limits for `MaxPathLength` (4096) and `MaxDepth` (50) to prevent algorithmic complexity attacks.
- **Frozen Mode**: Innovative `FrozenRouter` flattens static path segments for extreme read-heavy performance.
- **Lock-Free Logger**: Specific high-throughput `RingBuffer` logger implementation.
- **Minimalist Middleware**: Includes essential middlewares (Recovery, RequestID, AccessLog, Timeout, BodySizeLimit).
- **Pre-composed Middleware**: `Router.Use` and `Group` build middleware chains at registration time (no per-request wrapping).
- **Custom 404/405**: Optional `NotFound` and `MethodNotAllowed` handlers.

## Installation

```bash
go get github.com/WillunyLabs-LLC/wand
```

## Quick Start

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/WillunyLabs-LLC/wand/logger"
	"github.com/WillunyLabs-LLC/wand/middleware"
	"github.com/WillunyLabs-LLC/wand/router"
	"github.com/WillunyLabs-LLC/wand/server"
)

func main() {
	// Create a new router
	r := router.NewRouter()

	// Setup high-performance logger
	rb, _ := logger.NewRingBuffer(1024)
	go rb.Consume(func(events []logger.LogEvent) {
		// batch process logs
	})

	// Compose middleware (must be called before registering routes)
	if err := r.Use(
		middleware.Recovery,
		middleware.RequestID,
		func(next http.Handler) http.Handler { return middleware.BodySizeLimit(1<<20, next) },
		func(next http.Handler) http.Handler { return middleware.Timeout(5*time.Second, next) },
		func(next http.Handler) http.Handler { return middleware.AccessLog(rb, next) },
	); err != nil {
		panic(err)
	}

	api := r.Group("/api")

	// Register routes
	_ = api.GET("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	_ = api.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := router.Param(w, "id")
		w.Write([]byte("User ID: " + id))
	})

	// Start graceful server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
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
Run on Apple M4 Pro (Go 1.23).

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
