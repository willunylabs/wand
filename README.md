# Wand 🪄

[![CI](https://github.com/willunylabs/wand/actions/workflows/ci.yml/badge.svg)](https://github.com/willunylabs/wand/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/willunylabs/wand)](https://goreportcard.com/report/github.com/willunylabs/wand)
[![Go Reference](https://pkg.go.dev/badge/github.com/willunylabs/wand.svg)](https://pkg.go.dev/github.com/willunylabs/wand)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**High-Performance, Zero-Allocation HTTP Router for Go**

**Go version**: 1.24.12+ (patched standard library).

`wand` is a minimalist, infrastructure-grade HTTP router and toolkit designed for services where latency and memory efficiency are critical. It features a lock-free design, zero-allocation routing paths, and effective DoS protection.

> **Wand** /wɒnd/ - A symbol of magic and control. Elegantly directing traffic with precision and speed.

## Philosophy

Wand is a **router**, not a framework. We believe:

- 🎯 **Do One Thing Well**: Route HTTP requests efficiently, nothing more.
- 🧩 **Compose, Don't Replace**: Integrate with Go's ecosystem instead of reinventing it.
- ⚡ **Performance Matters**: Zero allocations on the hot path, but not at the cost of usability.
- 📖 **Explicit Over Magic**: No reflection, no code generation, no surprises.

If you need a batteries-included framework, consider Gin or Echo. If you want control, you're in the right place.

## Features

- **Zero Allocation**: Optimized hot paths (static, dynamic, and wildcard routes) generate **0 bytes** of garbage per request.
- **Zero-Alloc Param Extraction**: Params are captured via pooled slices and index offsets—no maps or context allocations.
- **High Performance**: 
    - **Static Routes**: ~35ns
    - **Dynamic Routes**: ~100ns
- **DoS Protection**: Built-in limits for `MaxPathLength` (4096) and `MaxDepth` (50) to prevent algorithmic complexity attacks.
- **Frozen Mode**: Innovative `FrozenRouter` flattens static path segments for extreme read-heavy performance.
- **Lock-Free Logger**: Specific high-throughput `RingBuffer` logger implementation.
- **Minimalist Middleware**: Includes essential middlewares (Logger, Recovery, RequestID, AccessLog, Timeout, BodySizeLimit, CORS, Static).
- **Pre-composed Middleware**: `Router.Use` and `Group` build middleware chains at registration time (no per-request wrapping).
- **Custom 404/405**: Optional `NotFound` and `MethodNotAllowed` handlers.
- **Strict Slash (Default: on)**: Redirects `/path` <-> `/path/` to the registered canonical path.
- **UseRawPath (Optional)**: Match on encoded paths and return encoded params. When `RawPath` is valid, matching skips decoded-path cleaning/redirects; invalid `RawPath` falls back to `Path` (see **Path Semantics & Security**).

## Installation

```bash
go get github.com/willunylabs/wand
```

## Quick Start

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/willunylabs/wand/logger"
	"github.com/willunylabs/wand/middleware"
	"github.com/willunylabs/wand/router"
	"github.com/willunylabs/wand/server"
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

### Logger (Text / JSON)

```go
// Text (default)
_ = r.Use(middleware.Logger)

// JSON
_ = r.Use(middleware.LoggerWith(middleware.LoggerOptions{
	JSON: true,
}))
```

### Third-Party Middleware

Wand uses standard `http.Handler` middleware signatures, so you can plug in any
third-party middleware (JWT, OTEL, Prometheus, etc.) directly:

```go
// Example: third-party JWT middleware
// jwtmw := somejwt.New(...)
// _ = r.Use(jwtmw)
```

## Components

- `router`: Path-Segment Trie based, zero-alloc HTTP router.
- `middleware`: Essential HTTP middlewares.
- `logger`: Lock-free ring buffer logger for high-throughput scenarios.
- `server`: Helpers for graceful server shutdown.
- `auth`: Minimal identity/authenticator interfaces.

## Guides

- `docs/server.md` — production/development server templates.
- `docs/security.md` — deployment hardening and safety notes.
- `docs/production_checklist.md` — production security checklist.
- `docs/integrations.md` — compression, rate limiting, trusted proxy parsing.
- `docs/observability.md` — Prometheus/OTel/pprof integration.
- `docs/auth.md` — auth interfaces with JWT/session examples.

## Server Best Practices

See `docs/server.md` for production/development `http.Server` templates and timeout guidance.

## Path Semantics & Security

- **Default behavior**: routing matches against `URL.Path` (decoded). Non-canonical paths are normalized with `cleanPath` and redirected to the canonical path.
- **UseRawPath**: when enabled and `URL.RawPath` is **valid** (`RawPath == EscapedPath()`), routing matches the **encoded** path and returns **encoded** params. In this mode, decoded-path cleaning/redirects are skipped.
- **Fallback**: if `RawPath` is invalid or inconsistent, routing falls back to `URL.Path` (decoded) and canonicalization/redirects apply.
- **StrictSlash + RawPath**: when `UseRawPath` is active, trailing-slash redirects preserve the encoded form to avoid changing path semantics.
- **Security note**: ensure your reverse proxy and app **agree on a single normalization layer**. If an upstream proxy decodes `%2F` to `/` while the router matches encoded paths, you can get route bypass or mismatch. Avoid double decoding and document the chosen layer for your deployment.
- **pprof note**: debug endpoints require an explicit allow policy; see `docs/observability.md` for the safe pattern.

## CORS Notes

- `AllowedOrigins: ["*"]` with `AllowCredentials: true` is **rejected** for safety. Use an explicit allowlist or `AllowOriginFunc`.

## Logger Notes

- `RingBuffer.Consume` will **re-panic** if the consumer handler panics, unless `PanicHandler` is set. Set `PanicHandler` to record/alert on failures, but avoid silently dropping log batches.

## Non-Goals

Wand will **not** include:

- ❌ Certificate management (use `autocert`, Caddy, or your cloud provider)
- ❌ A proprietary metrics system (use Prometheus/OTEL)
- ❌ A full web framework (no ORM, no "App" struct, no magic)
- ❌ Complex binding/validation (use `go-playground/validator`)

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

## Quality Gates

- CI: build, `go vet`, tests, and race (`.github/workflows/ci.yml`).
- Lint: `golangci-lint` (`.github/workflows/linter.yml`).
- Security: `govulncheck` (`.github/workflows/vuln.yml`).
- Static analysis: `gosec` (`.github/workflows/security.yml`).
- Supply chain: SBOM (`.github/workflows/sbom.yml`) + Dependabot (`.github/dependabot.yml`).
- Scheduled fuzz/bench: `.github/workflows/fuzz.yml`, `.github/workflows/bench.yml`.
- Benchmark regression gate: `benchmarks/baseline.txt` + `BENCH_MAX_REGRESSION_PCT` (see `benchmarks/README.md`).

## Security Guide

See `docs/security.md` for deployment hardening, proxy alignment, and CORS/logging safety notes.


## License

MIT
