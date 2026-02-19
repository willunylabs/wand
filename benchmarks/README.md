# Benchmarks

This directory stores benchmark results for regression tracking.

Workflow:
- Run `scripts/bench.sh` to generate `benchmarks/latest.txt`.
- Review results, then run `scripts/bench-update.sh` to promote as baseline.
- Keep baseline and compare runs on the same `goos`/`goarch` to avoid false regressions.
- CI will compare `baseline.txt` to `latest.txt` using `scripts/bench-compare.sh` and fail on regressions above `BENCH_MAX_REGRESSION_PCT` (default: 5).
