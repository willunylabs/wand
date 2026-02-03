#!/usr/bin/env bash
set -euo pipefail

mkdir -p benchmarks

go test ./router -run=^$ -bench=. -benchmem > benchmarks/latest.txt
