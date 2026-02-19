# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]
### Added
- Coverage gate script (`scripts/coverage-check.sh`) and CI coverage thresholds.
- New tests for `auth` interfaces, `router` wrapper pass-through behavior, and `middleware` status-writer pass-through behavior.
- New router benchmarks for `405 Method Not Allowed` and `OPTIONS`.
- Migration guide from Gin/Echo (`docs/migration_gin_echo.md`).

### Changed
- Go toolchain is now pinned with `toolchain go1.24.13` in `go.mod`.
- CI/workflows now use fixed Go patch version `1.24.13`.
- `gosec` and `govulncheck` installs are pinned to fixed versions in workflows.
- `allowedMethodsInTable` now uses a standard-method bitset plus custom-method slice instead of per-request map aggregation.
- `RingBuffer.TryWrite` now uses bounded spin + exponential backoff under contention.
- Bench baseline refreshed and benchmark docs updated.

## [1.0.0] - 2026-02-03
### Breaking
- Require Go 1.24.13+ (security-patched standard library).
- `RegisterPprof` now requires an explicit allow policy via `RegisterPprofWith` (returns error otherwise).

### Added
- `auth` package with minimal `Identity`/`Authenticator` interfaces.
- `middleware` additions: `Logger` (text/JSON), `CORS`, `Static`, trusted proxy helpers, and client IP resolution with CIDR trust.
- `router` helper `RegisterPprofWith` with access control.
- Production/security documentation: server templates, observability, integrations, auth, security guide, and production checklist.
- CI hardening: `-race` in CI, gosec, govulncheck, SBOM generation, and dependabot.

### Changed
- CI now targets Go 1.24.x only.
- Request IDs are now cryptographically random by default (fallback to counter on RNG failure).
- Text logs sanitize CR/LF to prevent log injection.
- CORS rejects wildcard origin with credentials for safety.
- Route handling shares a single preprocessing path between Router and FrozenRouter.
