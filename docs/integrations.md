# Third-Party Integrations

These examples show how to compose Wand with common ecosystem middleware.

## Compression (gzip)

```go
import gziphandler "github.com/nytimes/gziphandler"

_ = r.Use(func(next http.Handler) http.Handler {
	return gziphandler.GzipHandler(next)
})
```

## Rate Limiting

```go
import "golang.org/x/time/rate"

limiter := rate.NewLimiter(100, 200) // 100 req/s, burst 200
_ = r.Use(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
})
```

## Trusted Proxy Headers

Use the helper functions in `middleware/trusted_proxy.go` to parse
`X-Forwarded-*` headers and apply your own trust policy.

```go
ips := middleware.XForwardedFor(r)
proto := middleware.XForwardedProto(r)
host := middleware.XForwardedHost(r)
```

To safely resolve the client IP, only honor `X-Forwarded-For` when the
immediate peer is trusted:

```go
trust, _ := middleware.NewCIDRTrustFunc([]string{"10.0.0.0/8"})
clientIP := middleware.ClientIP(r, trust)
```
