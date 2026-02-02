# Wand Roadmap

## Mission
To provide a **high-performance, zero-allocation HTTP router** for Go, fully compatible with the standard library (`net/http`) without imposing a heavy framework structure.

## Track A: Core Routing (Priority P0) - *The Engine*
*Focus: Routing logic, matching algorithms, and standard compliance.*

- [x] **Host Matching**: 
    - Support routing based on `Host` header (e.g., `api.example.com` -> Subtree).
    - Strategy: `Host -> Router` map or `Host -> Subtree` to avoid per-request scanning.
- [x] **Custom HTTP Methods**: 
    - Support non-standard methods (e.g., `LINK`, `UNLINK`, `PURGE`, WebDAV).
    - Refactor `isStandardMethod` and `Allow` header construction.
- [x] **Case-Insensitive Matching**: 
    - Distinct opt-in setting (`Router.IgnoreCase`).
    - Define behavior for path cleaning and redirects.
- [x] **Router-level Panic Handler**: 
    - A lightweight simple fallback for unhandled panics (distinct from middleware.Recovery).

## Track B: Middleware Ecosystem (Extensions)
*Focus: Reusable components in the `middleware/` package. Does not touch core router.*

- **Essential Middlewares**:
    - [ ] `CORS`: Standard implementation.
    - [ ] `Static`: Efficient file serving (wrapper around `http.FileServer`).
    - [ ] `Compression`: Gzip/Brotli support.
    - [ ] `TrustedProxy`: `X-Forwarded-For` parsing.
- **Auth Interfaces**:
    - [ ] Provide lightweight interfaces for user/session identity.
    - [ ] *No built-in JWT/Session implementation* (provide examples instead).

## Track C: Server & Observability (Integrations)
*Focus: Helpers to run `net/http` server safely and observably.*

- [ ] **Server Hardening Helper**: 
    - Utilities to configure `http.Server` timeouts (`ReadHeader`, `Idle`) easily.
    - TLS helper (`RunTLS`) with modern secure defaults (HTTP/2, ALPN).
- [ ] **Observability Adapters**:
    - [ ] **Prometheus**: Official middleware adapter for `prometheus/client_golang`.
    - [ ] **OpenTelemetry**: Middleware for trace context propagation.
    - [ ] **Pprof**: Helper to register debug routes.

## Non-Goals (Scope Boundaries)
*Things Wand will explicitly **NOT** do to avoid bloat.*

- ❌ **Built-in Certificate Management**: Use Ingress, Caddy, or `autocert` manually.
- ❌ **Proprietary Metrics System**: We integrate with Prometheus/OTEL, we don't invent new counters.
- ❌ **Full Web Framework**: No built-in ORM, no heavy config loaders, no "App" struct.
- ❌ **Complex Binding/Validation**: We may provide simple helpers, but won't reimplement `go-playground/validator`.
