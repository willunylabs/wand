#!/usr/bin/env bash
set -euo pipefail

BASELINE="benchmarks/baseline.txt"
LATEST="benchmarks/latest.txt"

if [ ! -f "$BASELINE" ] || ! grep -q '^Benchmark' "$BASELINE"; then
  echo "baseline missing or empty; skipping compare"
  exit 0
fi

baseline_goos="$(awk '/^goos:/ {print $2; exit}' "$BASELINE")"
baseline_goarch="$(awk '/^goarch:/ {print $2; exit}' "$BASELINE")"
latest_goos="$(awk '/^goos:/ {print $2; exit}' "$LATEST")"
latest_goarch="$(awk '/^goarch:/ {print $2; exit}' "$LATEST")"

if [ -n "${baseline_goos}" ] && [ -n "${baseline_goarch}" ] && [ -n "${latest_goos}" ] && [ -n "${latest_goarch}" ]; then
  if [ "$baseline_goos" != "$latest_goos" ] || [ "$baseline_goarch" != "$latest_goarch" ]; then
    echo "benchmark environment mismatch (baseline=${baseline_goos}/${baseline_goarch}, latest=${latest_goos}/${latest_goarch}); skipping compare"
    exit 0
  fi
fi

if ! command -v benchstat >/dev/null 2>&1; then
  GOBIN="$(go env GOPATH)/bin"
  export PATH="$GOBIN:$PATH"
  go install golang.org/x/perf/cmd/benchstat@latest
fi

THRESHOLD="${BENCH_MAX_REGRESSION_PCT:-5}"
OUT="$(benchstat "$BASELINE" "$LATEST")"
echo "$OUT"
echo "$OUT" > benchmarks/compare.txt

echo "$OUT" | awk -v thr="$THRESHOLD" '
  /^Benchmark/ {
    if (match($0, /([+-][0-9.]+)%/, m)) {
      if (m[1] ~ /^\+/) {
        val = substr(m[1], 2) + 0
        if (val > thr) {
          printf("regression > %s%%: %s\n", thr, $0) > "/dev/stderr"
          fail = 1
        }
      }
    }
  }
  END { exit fail }
'
