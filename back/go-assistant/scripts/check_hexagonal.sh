#!/usr/bin/env bash
# check_hexagonal.sh — AC-010 enforcement.
#
# Fails if any file under back/go-assistant/cpn/ imports a forbidden OS-level
# package. The domain must stay platform-neutral so infra adapters can be
# substituted freely (OSHostAdapter today, docker/containerd tomorrow).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CPN_DIR="$ROOT/cpn"
# Forbidden imports (as they appear inside `import ( ... )` blocks).
FORBIDDEN=(
  '"os"'
  '"os/exec"'
  '"syscall"'
  '"github.com/creack/pty"'
)

fail=0
for pat in "${FORBIDDEN[@]}"; do
  # Only scan production .go files (no tests): the static check is meant to
  # prevent domain code from growing OS dependencies, not to block tests
  # from exercising their own enforcement.
  # shellcheck disable=SC2086
  matches=$(grep -RIn --include '*.go' --exclude '*_test.go' -F "$pat" "$CPN_DIR" || true)
  if [[ -n "$matches" ]]; then
    echo "AC-010 violation: $pat found in cpn/"
    echo "$matches"
    fail=1
  fi
done

if (( fail != 0 )); then
  exit 1
fi

echo "AC-010 OK: cpn/ does not import os, os/exec, syscall, or github.com/creack/pty"
