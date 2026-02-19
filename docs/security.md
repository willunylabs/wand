# Security Guide

This document provides deployment-time hardening recommendations. Wand is a router, not a full security framework.

## 1. Server Timeouts (Required)

Always set timeouts on `http.Server` to mitigate slowloris and resource exhaustion:

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

## 2. Go Toolchain

- Use **Go 1.24.13+** (patched standard library).
- Keep Go patch versions up to date to receive security fixes.

## 3. Reverse Proxy Alignment

If running behind Nginx/Envoy/Cloudflare:
- **Normalize once**: avoid double decoding of `%2F`, `%2e`, etc.
- **Match timeouts**: proxy timeouts should be >= app timeouts.
- **Limit request size** at the proxy as the first line of defense.

### Example Configs

#### Nginx (keep URI handling predictable)

```nginx
location / {
    proxy_pass http://app_upstream;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host $host;
    client_max_body_size 10m;
}
```

#### Envoy (single normalization layer)

```yaml
http_connection_manager:
  normalize_path: true
  merge_slashes: true
  path_with_escaped_slashes_action: KEEP_UNCHANGED
```

#### Cloudflare

- If your origin uses default decoded matching, keep URL normalization at the edge and keep `UseRawPath=false`.
- If your origin intentionally matches encoded routes (`UseRawPath=true`), avoid path rewrite/normalization rules that change escaped path semantics before requests reach origin.

## 4. Trusted Proxy Headers (X-Forwarded-*)

- Only trust `X-Forwarded-*` headers when the **immediate peer** is a trusted proxy.
- Never use `X-Forwarded-Host/Proto` for security decisions unless the proxy is trusted.

```go
trust, _ := middleware.NewCIDRTrustFunc([]string{"10.0.0.0/8"})
clientIP := middleware.ClientIP(r, trust)
```

## 5. CORS Safety

- `AllowedOrigins: ["*"]` with `AllowCredentials: true` is rejected by Wand.
- Prefer explicit allowlists or `AllowOriginFunc`.

## 6. Logging Safety

- Text logs sanitize CR/LF to prevent log injection.
- Prefer JSON logs if logs are consumed by parsers or SIEM tools.

## 7. pprof Exposure

- Only enable pprof on internal networks or behind authentication.
- Use `RegisterPprofWith` with an explicit allow policy (required).
- `RegisterPprof` returns an error unless an allow policy is provided.

## 8. Dependency & Supply Chain

- `govulncheck` and `gosec` run in CI.
- Dependabot updates dependencies weekly.
- SBOM generation runs in CI for auditing.

## Production Checklist

See `docs/production_checklist.md` for a deployment checklist.
