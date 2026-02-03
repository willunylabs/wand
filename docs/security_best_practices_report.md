# Security Best Practices Report (Go)

## Executive Summary
Overall security posture is strong for a routing library: request ID generation is cryptographically random, text logs sanitize CR/LF, CORS is conservative by default, and diagnostic endpoints require explicit allow policies. CI includes `govulncheck` and `gosec`. The main residual risks are **toolchain patch enforcement** (ensuring production uses a patched Go release) and **runtime server hardening** (timeouts/body limits are the consumer's responsibility). No critical or high issues were found in the library itself.

## Medium Severity

### F-001: Go toolchain patch level not enforced at build time
- **Rule ID:** GO-DEPLOY-001
- **Severity:** Medium
- **Location:** `go.mod:3`, `.github/workflows/vuln.yml:18-27`, `.github/workflows/security.yml:19-26`, `docs/security.md:21-24`
- **Evidence:**
  - `go.mod` sets major/minor only: `go 1.24.0` (line 3).
  - CI uses `go-version-file: go.mod`, which does not pin a patch level.
  - Docs recommend **Go 1.24.12+** but this is not enforced in build tooling.
- **Impact:** Production builds running an outdated patch version can reintroduce known **standard library vulnerabilities** (e.g., `net/http`, `crypto/*`), even if CI uses a newer patch level.
- **Fix:** Enforce a minimum patched toolchain in build/release pipelines.
  - Option A: Add `toolchain go1.24.12` to `go.mod` (Go 1.21+), and ensure your build image/toolchain honors it.
  - Option B: Pin CI and release images to `1.24.12` or newer explicitly.
- **Mitigation:** Current CI runs with latest 1.24.x on GitHub Actions, and docs already instruct 1.24.12+.
- **False positive notes:** If your production images already pin Go >= 1.24.12, this risk is mitigated; verify in your deployment pipeline or container base image.

## Low Severity

### F-002: Runtime HTTP hardening is consumer-controlled (timeouts/body limits not enforced by library)
- **Rule ID:** GO-HTTP-001 (baseline server hardening)
- **Severity:** Low
- **Location:** `server/graceful.go:22-52`, `docs/server.md:7-23`, `docs/security.md:5-19`
- **Evidence:**
  - `server.Run` simply calls `srv.ListenAndServe()` without enforcing any timeouts (`server/graceful.go:22-52`).
  - The recommended timeouts are documented but not enforced (`docs/server.md:7-23`).
- **Impact:** If consumers instantiate `http.Server` without timeouts/MaxHeaderBytes, deployments are more susceptible to slowloris and resource exhaustion.
- **Fix:** In production apps, always configure `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and `MaxHeaderBytes` (see templates in `docs/server.md`).
- **Mitigation:** The repository already documents best-practice server configuration in `docs/server.md` and `docs/security.md`.
- **False positive notes:** If downstream services already enforce timeouts at the edge (Nginx/Envoy/ALB), the risk is reduced but still recommended to set application-level timeouts.

## Informational / Good Practices Observed
- **CRLF log injection mitigated:** text logs sanitize CR/LF (`middleware/logger.go:133-186`).
- **CORS is conservative:** wildcard + credentials is explicitly rejected (`middleware/cors.go:74-77`).
- **Trusted proxy handling:** helper enforces trust boundaries with allowlist function (`middleware/trusted_proxy.go:34-96`).
- **pprof is gated:** `RegisterPprofWith` requires explicit allow policy (`router/pprof.go:27-35`).
- **CI scanning present:** `govulncheck` and `gosec` workflows are configured (`.github/workflows/vuln.yml`, `.github/workflows/security.yml`).

## Notes
This report focuses on the library itself. Any application built on Wand must still implement authentication/authorization and apply production hardening at the server and edge layers.
