# Wand Router: High-Performance Zero-Allocation HTTP Router

Wand Router is a blazing fast, zero-allocation HTTP router kernel for Go, built on a Radix Trie structure. It prioritizes memory efficiency, strict correctness, and rigorous conflict detection.

**Status**: Production-Ready Kernel (`v1.0`)

## Features

- **Zero Allocation**: 0 bytes/op and 0 allocs/op during request serving (verified by benchmarks).
- **Strict Conflict Detection**:
  - Detects conflicting parameters (e.g., `/users/:id` vs `/users/:name`).
  - Detects Param vs. Wildcard conflicts.
  - Detects duplicate parameter names in the same path.
- **Fail-Fast**: Returns explicit errors during registration instead of runtime panics.
- **Standard Compatible**: Fully compatible with `net/http` (`http.Handler`, `http.ResponseWriter`).
- **Method Semantics**: Automatic `HEAD` fallback to `GET`, `OPTIONS` with `Allow`, and `405 Method Not Allowed` with `Allow`.
- **DoS Protection**: Enforces maximum path depth and path length.
- **Frozen Router**: Immutable, compacted static chains with fast span comparisons (Radix-like path compression).

## Performance

Benchmarks run on Apple M4 Pro:

```text
BenchmarkRouter_Dynamic-14      11683674	       101.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouter_Static-14       34663963	        34.13 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouter_Wildcard-14     14938099	        79.29 ns/op	       0 B/op	       0 allocs/op
BenchmarkFrozen_Dynamic-14      10687927	       114.3 ns/op	       0 B/op	       0 allocs/op
BenchmarkFrozen_Static-14       34877637	        33.76 ns/op	       0 B/op	       0 allocs/op
BenchmarkFrozen_Wildcard-14     14494634	        81.86 ns/op	       0 B/op	       0 allocs/op
```

Notes:
- Frozen shines when there are long static chains due to span comparison.
- Router can be slightly faster for dynamic/wildcard-heavy routes because it avoids static span checks.

## Internal Architecture

### 1. Trie Structure
The router uses a modified Trie with structured children for O(1) matching:
- `staticChildren`: small slice for <= 4 children, upgraded to `map[string]*node` above the threshold.
- `paramChild`: `*node` (one named parameter per level).
- `wildChild`: `*node` (one wildcard per level, must be last).

### 2. Zero-Allocation Strategy
We achieve **strict zero-allocation** through aggressive object pooling and sentinel optimization:

- **`sync.Pool` for Context**: Request contexts (`Params` container) are pooled.
- **`sync.Pool` for Segments**: Path splitting does not allocate new strings. We use a pooled `pathSegments` struct that stores:
  - Original `path` string.
  - `indices` slice pointing to segment starts.
- **Sentinel Optimization**: The `indices` slice always contains a sentinel `len(path)` at the end. This allows O(1) wildcard capturing by slicing the original path string directly (`path[indices[i]:]`) without bounds checking branches.

### 3. Fast-Path Optimization
Static routes bypass the parameter extraction logic entirely:
```go
if !node.hasParams {
    node.handler(w, req) // Direct call, no overhead
    return
}
```

### 4. Frozen Router (Path Compression)
`Freeze()` builds an immutable router with compressed static chains:
- Consecutive static segments are joined into a single span (`"a/b/c"`).
- Matching uses a single string compare on the original path substring.
- Ideal for production deployments where routes are finalized at startup.

## Usage

### Basic Example

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"github.com/WillunyLabs-LLC/wand/router"
)

func main() {
	r := router.NewRouter()

	// 1. Static Route
	r.GET("/", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("Welcome to Wand"))
	})

	// 2. Param Route (Zero-Alloc param retrieval)
	r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := router.Param(w, "id")
		fmt.Fprintf(w, "User ID: %s", id)
	})

	// 3. Wildcard Route
	r.GET("/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := router.Param(w, "filepath")
		fmt.Fprintf(w, "Serving file: %s", fp)
	})

	log.Println("Server running on :8080")
http.ListenAndServe(":8080", r)
}
```

### Middleware & Groups

Middleware chains are pre-composed at registration time for zero per-request overhead.
Call `Use` before registering routes:

```go
r := router.NewRouter()
_ = r.Use(middleware.Recovery)

api := r.Group("/api")
_ = api.GET("/health", handler)
```

To adapt HandleFunc-style middleware:

```go
_ = r.Use(router.WrapHandle(func(next router.HandleFunc) router.HandleFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// do something
		next(w, r)
	}
}))
```

### Frozen Router (Production)

```go
fr, err := r.Freeze()
if err != nil {
    log.Fatalf("freeze failed: %v", err)
}
http.ListenAndServe(":8080", fr)
```

### Error Handling
Unlike many frameworks that panic, Wand Router returns errors on invalid registration:

```go
err := r.GET("/users/:id/posts/:id", handler)
if err != nil {
    log.Fatalf("Invalid route: %v", err) // "conflict: duplicate param name 'id'..."
}
```

## Safety Notes

- Runtime registration is supported, but it is serialized with an RWMutex and blocks concurrent reads while updating.
- If a handler panics, pooled objects are not returned; use a recovery middleware if you need hard guarantees.
- For untrusted traffic, consider limiting max request line length at the HTTP server or reverse proxy.

## Roadmap

- [x] Core Router (Zero Alloc)
- [x] RingBuffer Logger (Core)
- [x] Middleware (Recovery, RequestID, AccessLog, Timeout, BodySizeLimit)
- [x] Router-level middleware chaining (`Use`, `Group`)
- [ ] Async log sinks

## License

MIT
