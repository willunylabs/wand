# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]
### Breaking
- Require Go 1.24.12+ (security-patched standard library).

### Changed
- CI now targets Go 1.24.x only.

## [1.0.0] - 2026-02-03
### Breaking
- `RegisterPprof` now requires an explicit allow policy via `RegisterPprofWith` (returns error otherwise).

### Added
- `auth` package with minimal `Identity`/`Authenticator` interfaces.
- `middleware` additions: `Logger` (text/JSON), `CORS`, `Static`, trusted proxy helpers, and client IP resolution with CIDR trust.
- `router` helper `RegisterPprofWith` with access control.
- Production/security documentation: server templates, observability, integrations, auth, security guide, and production checklist.
- CI hardening: `-race` in CI, gosec, govulncheck, SBOM generation, and dependabot.

### Changed
- Request IDs are now cryptographically random by default (fallback to counter on RNG failure).
- Text logs sanitize CR/LF to prevent log injection.
- CORS rejects wildcard origin with credentials for safety.
- Route handling shares a single preprocessing path between Router and FrozenRouter.
