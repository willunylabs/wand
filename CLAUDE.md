# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Wand is a high-performance, zero-allocation HTTP router for Go designed for infrastructure-grade services where latency and memory efficiency are critical. It's a **router, not a framework** - focused on doing one thing well.

**Key Philosophy:**
- Zero allocations on hot paths (routing)
- Lock-free design for high throughput
- Explicit over magic (no reflection, no code generation)
- Standard `net/http` compatibility

## Development Commands

### Testing
```bash
# Run all tests
make test
# or: go test ./...

# Run tests with race detector
make race
# or: go test -race ./...

# Run specific package tests
go test ./router
go test ./middleware
go test ./logger
```

### Benchmarking
```bash
# Run benchmarks
make bench
# or: go test ./router -run=^$ -bench=. -benchmem

# Generate benchmark baseline
./scripts/bench.sh              # Creates benchmarks/latest.txt
./scripts/bench-update.sh       # Promotes latest.txt to baseline.txt

# Compare benchmarks (used in CI)
./scripts/bench-compare.sh
```

### Fuzzing
```bash
# Run fuzz tests (30 seconds)
make fuzz
# or: go test ./router -run=^$ -fuzz=FuzzRouter_ -fuzztime=30s

# Specific fuzz target
go test ./router -run=^$ -fuzz=FuzzRouter_FrozenParity -fuzztime=5s
```

### Linting
```bash
make lint
# or: golangci-lint run
```

### Soak Testing
```bash
make soak
# or: ./scripts/soak.sh
```

## Architecture

### Core Components

1. **router/** - Path-Segment Trie-based HTTP router
   - `router.go`: Main Router implementation with sync.Pool for zero-alloc
   - `trie.go`: Radix Trie structure with structured children (static/param/wildcard)
   - `frozen.go`: Immutable FrozenRouter with compressed static chains
   - `group.go`: Route grouping with prefix support
   - `serve.go`: ServeHTTP implementation and request handling
   - `pprof.go`: Debug endpoint registration helpers

2. **middleware/** - Essential HTTP middlewares
   - All middleware uses standard `func(http.Handler) http.Handler` signature
   - Pre-composed at registration time (no per-request wrapping overhead)
   - Includes: Recovery, RequestID, Logger, AccessLog, Timeout, BodySizeLimit, CORS, Static, TrustedProxy

3. **logger/** - Lock-free ring buffer logger
   - `ringbuffer.go`: MPSC (Multi-Producer Single-Consumer) ring buffer
   - Cache-line padding to prevent false sharing
   - Atomic operations for lock-free writes
   - Batch consumption pattern for high throughput

4. **server/** - Graceful shutdown helpers
   - `graceful.go`: Signal handling and graceful server shutdown

5. **auth/** - Minimal identity/authenticator interfaces
   - `interfaces.go`: Identity and Authenticator abstractions

### Zero-Allocation Strategy

The router achieves zero allocations through:

1. **sync.Pool for object reuse:**
   - `paramPool`: Recycles parameter storage
   - `partsPool`: Recycles path segment slices
   - `rwPool`: Recycles ResponseWriter wrappers

2. **Sentinel optimization:**
   - Path segments store indices into original string
   - Wildcard capture uses direct string slicing without bounds checks

3. **Fast-path optimization:**
   - Static routes bypass parameter extraction entirely
   - Direct handler calls when `!node.hasParams`

4. **Frozen Router (production):**
   - Consecutive static segments compressed into single spans
   - Single string comparison for static chains
   - Call `r.Freeze()` after route registration for production

### Trie Structure

The router uses a modified Radix Trie with:
- **staticChildren**: Small slice (≤4 children) or map (>4 children)
- **paramChild**: Single named parameter per level (`:id`)
- **wildChild**: Single wildcard per level (`*filepath`, must be last)

**Conflict Detection:**
- Duplicate parameter names in same path
- Conflicting parameters (e.g., `/users/:id` vs `/users/:name`)
- Param vs wildcard conflicts

### Middleware Composition

**IMPORTANT:** Middleware must be registered with `Use()` **before** registering routes:

```go
r := router.NewRouter()
_ = r.Use(middleware.Recovery, middleware.RequestID)  // Must come first
_ = r.GET("/api/users", handler)                      // Then register routes
```

Groups inherit parent middleware and can add their own:
```go
api := r.Group("/api")
_ = api.Use(authMiddleware)  // Applies only to /api/* routes
```

## Path Semantics & Security

- **Default:** Routes match against `URL.Path` (decoded), non-canonical paths are normalized and redirected
- **UseRawPath:** When enabled and `URL.RawPath` is valid, routes match encoded paths and return encoded params
- **StrictSlash (default: on):** Redirects `/path` ↔ `/path/` to canonical form
- **DoS Protection:** `MaxPathLength=4096`, `MaxDepth=50`

**Security Note:** Ensure reverse proxy and router agree on normalization layer to avoid route bypass (e.g., `%2F` handling).

## Testing Patterns

### Router Tests
- Use `router_test.go` for routing logic tests
- Use `router_fuzz_test.go` for fuzz testing
- Verify zero allocations with `-benchmem` flag

### Middleware Tests
- Test middleware in isolation with mock handlers
- Verify middleware chain composition
- Test panic recovery and error handling

### Benchmark Regression
- CI fails if benchmarks regress >5% from baseline
- Update baseline with `./scripts/bench-update.sh` after intentional changes
- Baseline stored in `benchmarks/baseline.txt`

## CI/CD Pipeline

Workflows in `.github/workflows/`:
- **ci.yml**: Build, vet, test, race detection, short fuzz
- **linter.yml**: golangci-lint checks
- **vuln.yml**: govulncheck for security vulnerabilities
- **security.yml**: gosec static analysis
- **sbom.yml**: Software Bill of Materials generation
- **fuzz.yml**: Scheduled fuzz testing
- **bench.yml**: Scheduled benchmark runs

## Documentation

Key docs in `docs/`:
- `server.md`: Production/development server templates
- `security.md`: Deployment hardening and safety notes
- `production_checklist.md`: Pre-deployment security checklist
- `integrations.md`: Compression, rate limiting, trusted proxy
- `observability.md`: Prometheus/OTel/pprof integration
- `auth.md`: Auth interfaces with JWT/session examples

## Code Style

- Follow standard Go idioms
- Run `go fmt ./...` before committing
- No reflection or code generation
- Explicit error handling (no panics in library code)
- Use `HandleFunc` return errors during registration, not at runtime

## Performance Targets

- Static routes: ~35ns
- Dynamic routes: ~100ns
- Zero allocations on hot path (0 B/op, 0 allocs/op)
- Verify with: `go test ./router -run=^$ -bench=BenchmarkRouter -benchmem`

## Common Patterns

### Parameter Extraction
```go
func handler(w http.ResponseWriter, req *http.Request) {
    id, ok := router.Param(w, "id")
    if !ok {
        http.Error(w, "missing id", 400)
        return
    }
    // use id
}
```

### Frozen Router (Production)
```go
r := router.NewRouter()
// ... register all routes ...
fr, err := r.Freeze()
if err != nil {
    log.Fatal(err)
}
http.ListenAndServe(":8080", fr)
```

### RingBuffer Logger
```go
rb, _ := logger.NewRingBuffer(1024)
go rb.Consume(func(events []logger.LogEvent) {
    // batch process logs
})
defer rb.Close()
```

## Go Version

Requires Go 1.24.0+ (uses patched standard library features).
