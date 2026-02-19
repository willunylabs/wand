#!/usr/bin/env bash
set -euo pipefail

total_min="${COVERAGE_MIN_TOTAL_PCT:-78}"
router_min="${COVERAGE_MIN_ROUTER_PCT:-80}"
middleware_min="${COVERAGE_MIN_MIDDLEWARE_PCT:-78}"
logger_min="${COVERAGE_MIN_LOGGER_PCT:-90}"
auth_min="${COVERAGE_MIN_AUTH_PCT:-80}"

tmp_profile="$(mktemp)"
trap 'rm -f "$tmp_profile"' EXIT

go test ./... -coverprofile="$tmp_profile" >/dev/null
total_cov="$(go tool cover -func="$tmp_profile" | awk '/^total:/ { gsub("%", "", $3); print $3 }')"

check_cov() {
  local name="$1"
  local actual="$2"
  local minimum="$3"
  if awk -v actual="$actual" -v minimum="$minimum" 'BEGIN { exit(actual+0 >= minimum+0 ? 0 : 1) }'; then
    printf '%s coverage: %s%% (min %s%%)\n' "$name" "$actual" "$minimum"
    return 0
  fi
  printf '%s coverage: %s%% (min %s%%) -- FAILED\n' "$name" "$actual" "$minimum" >&2
  return 1
}

pkg_cov() {
  local pkg="$1"
  go test "$pkg" -cover 2>/dev/null | sed -nE 's/.*coverage: ([0-9.]+)%.*/\1/p' | tail -n1
}

router_cov="$(pkg_cov ./router)"
middleware_cov="$(pkg_cov ./middleware)"
logger_cov="$(pkg_cov ./logger)"
auth_cov="$(pkg_cov ./auth)"

status=0
check_cov "total" "$total_cov" "$total_min" || status=1
check_cov "router" "$router_cov" "$router_min" || status=1
check_cov "middleware" "$middleware_cov" "$middleware_min" || status=1
check_cov "logger" "$logger_cov" "$logger_min" || status=1
check_cov "auth" "$auth_cov" "$auth_min" || status=1

exit "$status"
