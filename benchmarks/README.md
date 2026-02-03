# Benchmarks

This directory stores benchmark results for regression tracking.

Workflow:
- Run `scripts/bench.sh` to generate `benchmarks/latest.txt`.
- Review results, then run `scripts/bench-update.sh` to promote as baseline.
- CI will compare `baseline.txt` to `latest.txt` when a baseline exists and fail on regressions above `BENCH_MAX_REGRESSION_PCT` (default: 5).
