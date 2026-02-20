# Production Security Checklist

Use this checklist before deploying Wand-based services.

## Server Defaults
- Use Go 1.24.13+ (patched standard library).
- Set `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`.
- Set `MaxHeaderBytes` to cap header memory usage.
- Enforce request body limits at the proxy or with middleware.

## Reverse Proxy Alignment
- Normalize and decode paths exactly once.
- Align proxy timeouts with app timeouts (proxy >= app).
- Enforce request size limits at the edge.

## Trusted Proxy Headers
- Only trust `X-Forwarded-*` from known proxy CIDRs.
- Derive client IP via `middleware.ClientIP` with a trust function.

## CORS
- Avoid `AllowedOrigins: ["*"]` with credentials.
- Use an explicit allowlist or `AllowOriginFunc`.

## Observability
- Expose metrics on internal endpoints only.
- Keep pprof internal or protected with allowlist.

## pprof
- Use `RegisterPprofWith` and an explicit allow policy.
- Never expose pprof on public interfaces.

## Logging
- Prefer JSON logs for structured ingestion.
- Keep request IDs enabled for traceability.

## Supply Chain
- Run `govulncheck` and `gosec` in CI.
- Keep dependencies updated (Dependabot or equivalent).
- Generate and archive SBOMs for releases.
