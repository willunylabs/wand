# Wand Roadmap

## Mission
To provide a **high-performance, zero-allocation HTTP router** for Go, fully compatible with the standard library (`net/http`) without imposing a heavy framework structure.

## Philosophy
Wand is a **router**, not a framework. We believe:
- 🎯 **Do One Thing Well**: Route HTTP requests efficiently, nothing more.
- 🧩 **Compose, Don't Replace**: Integrate with Go's ecosystem instead of reinventing it.
- ⚡ **Performance Matters**: Zero allocations on the hot path, but not at the cost of usability.
- 📖 **Explicit Over Magic**: No reflection, no code generation, no surprises.

*If you need a batteries-included framework, consider Gin or Echo. If you want control, you're in the right place.*

---

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

---

## Track B: Middleware Ecosystem (Extensions)
*Focus: Reusable components in the `middleware/` package. Keep it small and composable.*

### Essential Middlewares (official implementations)
- [ ] `Logger`: Request logging with customizable format (baseline exists, to be refined).
- [ ] `Recovery`: Panic recovery with stack trace (baseline exists, to be refined).
- [ ] `CORS`: Standard implementation.
- [ ] `Static`: Efficient file serving.

### Third-Party Integration Guides (examples only, not official packages)
- [ ] **Compression**: How to wrap `nytimes/gziphandler`.
- [ ] **Rate Limiting**: How to integrate `golang.org/x/time/rate`.
- [ ] **Trusted Proxy**: Helper functions for `X-Forwarded-*` parsing (no security policy).

### Auth Interfaces (interfaces only)
- [ ] Define `Identity` and `Authenticator` interfaces.
- [ ] Provide JWT/Session examples (outside core repo).

---

## Track C: Server & Observability (Integrations)
*Focus: Thin adapters to make ecosystem integrations easy. No framework bloat.*

### Server Helpers (docs + snippets, not a new package)
- [ ] Best-practice docs for `http.Server` timeouts (`ReadHeader`, `Idle`).
- [ ] Example Production/Development templates.
- [ ] **No wrapper** around `ListenAndServe`.

### Observability Adapters (thin helpers)
- [ ] **Prometheus**: `PrometheusMiddleware(registry)` for count, duration, size (minimal API).
- [ ] **OpenTelemetry**: `OTELMiddleware(tracer)` for span creation + context propagation (minimal API).
- [ ] **Pprof**: `RegisterPprof(router, prefix)` helper.

---

## Non-Goals (Scope Boundaries)
*Things Wand will explicitly **NOT** do to avoid bloat.*

### What We Won't Build ❌
- ❌ **Certificate Management**: Use `autocert`, Caddy, or your cloud provider.
- ❌ **Proprietary Metrics**: Integrate with Prometheus/OTEL instead.
- ❌ **Full Framework**: No ORM, no "App" struct, no magic.
- ❌ **Complex Binding/Validation**: Use `go-playground/validator` directly.
- ❌ **Template Engine**: Use `html/template` or your preferred engine.
- ❌ **WebSocket Wrapper**: Use `gorilla/websocket` directly.
- ❌ **Session Store**: Integrate Redis/Memcached yourself.

### What You Should Use Instead ✅
- **Config Management**: `spf13/viper` or `koanf`
- **Validation**: `go-playground/validator`
- **Database**: `sqlx`, `gorm`, or raw `database/sql`
- **Caching**: `go-redis/redis`, `bradfitz/gomemcache`
- **WebSocket**: `gorilla/websocket`, `nhooyr/websocket`

---

## Contributing
See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on proposing new features or submitting PRs.

**Before opening a feature request**, please check:
1. Is this feature in our **Non-Goals** list? If yes, it won't be accepted.
2. Does this add significant value to **most users**? If it's niche, consider a third-party package.
3. Can this be achieved with **existing primitives**? Sometimes composition is better than addition.

*Note: This roadmap is directional and does not imply delivery dates or guarantees.*
