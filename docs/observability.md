# Observability

These are thin integration examples with common tooling.

## Prometheus

```go
import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

registry := prometheus.NewRegistry()
// register metrics...

// Expose metrics
_ = r.GET("/metrics", func(w http.ResponseWriter, r *http.Request) {
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
})
```

## OpenTelemetry

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

_ = r.Use(func(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http")
})
```

## pprof

pprof endpoints are gated by an explicit allow policy. Use an allowlist in
production; public exposure is unsafe.

```go
allowLocal := func(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host == "127.0.0.1" || host == "::1"
}

if err := router.RegisterPprofWith(r, router.PprofOptions{
	Prefix: "/debug/pprof",
	Allow:  allowLocal,
}); err != nil {
	panic(err)
}
```
