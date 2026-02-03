#!/usr/bin/env bash
set -euo pipefail

BASELINE="benchmarks/baseline.txt"
LATEST="benchmarks/latest.txt"

if [ ! -f "$BASELINE" ] || ! grep -q '^Benchmark' "$BASELINE"; then
  echo "baseline missing or empty; skipping compare"
  exit 0
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
