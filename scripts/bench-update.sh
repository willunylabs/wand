#!/usr/bin/env bash
set -euo pipefail

if [ ! -f benchmarks/latest.txt ]; then
  echo "benchmarks/latest.txt not found; run scripts/bench.sh first"
  exit 1
fi

cp benchmarks/latest.txt benchmarks/baseline.txt
