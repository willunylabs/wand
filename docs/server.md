# Server Best Practices

These templates use standard `net/http` knobs only. They are intentionally minimal and framework-agnostic.

## Production Template

```go
srv := &http.Server{
	Addr:              ":8080",
	Handler:           handler,
	ReadHeaderTimeout: 5 * time.Second,
	ReadTimeout:       15 * time.Second,
	WriteTimeout:      30 * time.Second,
	IdleTimeout:       60 * time.Second,
	MaxHeaderBytes:    1 << 20, // 1 MiB
}
```

Notes:
- `ReadHeaderTimeout` protects against slowloris-style attacks.
- `ReadTimeout` and `WriteTimeout` bound request/response lifetimes.
- `IdleTimeout` prevents idle keep-alive connections from piling up.
- `MaxHeaderBytes` caps memory usage from large headers.

## Development Template

```go
srv := &http.Server{
	Addr:              ":8080",
	Handler:           handler,
	ReadHeaderTimeout: 5 * time.Second,
	ReadTimeout:       0,
	WriteTimeout:      0,
	IdleTimeout:       0,
	MaxHeaderBytes:    1 << 20, // 1 MiB
}
```

Notes:
- Consider disabling `ReadTimeout`/`WriteTimeout` for long-lived local debugging.
- Keep `ReadHeaderTimeout` even in dev to avoid accidental slowloris hangs.

## Reverse Proxy Alignment

If you run behind a proxy (Nginx, Envoy, Cloudflare), ensure:
- Path normalization/decoding happens **once** (avoid double-decoding).
- Timeouts are aligned (proxy timeouts should be >= app timeouts).
