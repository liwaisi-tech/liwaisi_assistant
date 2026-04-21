#!/usr/bin/env bash
# benchstat_gate.sh — SC-17 REQ-1704 regression gate.
#
# Compares two `go test -bench` outputs (baseline vs current) and fails
# (exit 1) if any benchmark regresses by more than 10%. Uses `benchstat`
# when available for statistical comparison; falls back to a plain
# ns/op delta parser otherwise.
#
# Install benchstat once with:
#   go install golang.org/x/perf/cmd/benchstat@latest
#
# Usage:
#   scripts/benchstat_gate.sh <baseline.txt> <current.txt>
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <baseline.txt> <current.txt>" >&2
  exit 2
fi

BASELINE="$1"
CURRENT="$2"
THRESHOLD_PCT="${REGRESSION_THRESHOLD_PCT:-10}"

if [[ ! -f "$BASELINE" ]]; then
  echo "baseline file not found: $BASELINE" >&2
  exit 2
fi
if [[ ! -f "$CURRENT" ]]; then
  echo "current file not found: $CURRENT" >&2
  exit 2
fi

if command -v benchstat >/dev/null 2>&1; then
  echo "== benchstat =="
  benchstat "$BASELINE" "$CURRENT" || true
fi

# Deterministic fallback: extract ns/op from each Benchmark line and diff.
# Works on both plain go-test output and benchstat-normalised tables.
awk_compare() {
  awk -v threshold="$THRESHOLD_PCT" '
    function key(name) {
      sub(/-[0-9]+$/, "", name)
      return name
    }
    BEGIN { regressed = 0 }
    FNR == NR {
      if ($1 ~ /^Benchmark/) {
        k = key($1)
        for (i = 2; i <= NF; i++) {
          if ($i == "ns/op") {
            base[k] = $(i-1) + 0
          }
        }
      }
      next
    }
    {
      if ($1 ~ /^Benchmark/) {
        k = key($1)
        for (i = 2; i <= NF; i++) {
          if ($i == "ns/op") {
            cur = $(i-1) + 0
            if (k in base && base[k] > 0) {
              delta = (cur - base[k]) / base[k] * 100.0
              printf "%-48s base=%.0fns cur=%.0fns delta=%+.2f%%\n", k, base[k], cur, delta
              if (delta > threshold) {
                printf "  REGRESSION: %s exceeds %s%% threshold\n", k, threshold
                regressed = 1
              }
            }
          }
        }
      }
    }
    END {
      if (regressed) {
        print "FAIL: one or more benchmarks regressed beyond threshold"
        exit 1
      }
      print "OK: all benchmarks within " threshold "% of baseline"
    }
  ' "$1" "$2"
}

echo "== regression gate (threshold=${THRESHOLD_PCT}%) =="
awk_compare "$BASELINE" "$CURRENT"
