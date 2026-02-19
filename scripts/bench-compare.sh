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

THRESHOLD="${BENCH_MAX_REGRESSION_PCT:-5}"
awk -v thr="$THRESHOLD" '
  function normalize(name) {
    sub(/-[0-9]+$/, "", name)
    return name
  }

  function pct(base, latest) {
    if (base == 0) {
      if (latest == 0) {
        return 0
      }
      return 1000
    }
    return ((latest - base) / base) * 100
  }

  FNR == NR {
    if ($1 ~ /^Benchmark/) {
      name = normalize($1)
      base_ns[name] = $3 + 0
      base_b[name] = $5 + 0
      base_alloc[name] = $7 + 0
    }
    next
  }

  {
    if ($1 ~ /^Benchmark/) {
      name = normalize($1)
      latest_ns[name] = $3 + 0
      latest_b[name] = $5 + 0
      latest_alloc[name] = $7 + 0
    }
  }

  END {
    printf("benchmark regression threshold: %s%%\n", thr)
    for (name in latest_ns) {
      if (!(name in base_ns)) {
        printf("new benchmark (no baseline): %s\n", name)
        continue
      }

      ns_pct = pct(base_ns[name], latest_ns[name])
      b_pct = pct(base_b[name], latest_b[name])
      alloc_pct = pct(base_alloc[name], latest_alloc[name])

      printf("%s ns/op: %.2f -> %.2f (%+.2f%%), B/op: %.2f -> %.2f (%+.2f%%), allocs/op: %.2f -> %.2f (%+.2f%%)\n",
             name,
             base_ns[name], latest_ns[name], ns_pct,
             base_b[name], latest_b[name], b_pct,
             base_alloc[name], latest_alloc[name], alloc_pct)

      if (ns_pct > thr || b_pct > thr || alloc_pct > thr) {
        printf("regression > %s%%: %s\n", thr, name) > "/dev/stderr"
        fail = 1
      }
    }

    for (name in base_ns) {
      if (!(name in latest_ns)) {
        printf("missing benchmark in latest: %s\n", name) > "/dev/stderr"
        fail = 1
      }
    }

    exit fail
  }
' "$BASELINE" "$LATEST" | tee benchmarks/compare.txt
