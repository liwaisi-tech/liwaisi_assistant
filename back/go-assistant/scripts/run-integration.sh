#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# ── Validate environment ────────────────────────────────────────────────────

if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  ERROR: OPENROUTER_API_KEY must be set                         ║"
    echo "║                                                                ║"
    echo "║  Usage:                                                        ║"
    echo "║    OPENROUTER_API_KEY=sk-or-... ./scripts/run-integration.sh   ║"
    echo "║                                                                ║"
    echo "║  Or source from .env:                                          ║"
    echo "║    source integration/testdata/.env && ./scripts/run-integration.sh ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    exit 1
fi

# ── Configuration ────────────────────────────────────────────────────────────

export DEFAULT_MODEL="${DEFAULT_MODEL:-minimax/minimax-m2.7}"
export LOG_LEVEL="${LOG_LEVEL:-error}"

cd "$PROJECT_DIR"

echo "============================================"
echo "  Liwaisi Integration Tests"
echo "============================================"
echo "  Model:     $DEFAULT_MODEL"
echo "  Log level: $LOG_LEVEL"
echo "  Key:       ${OPENROUTER_API_KEY:0:12}..."
echo "============================================"
echo ""

# ── Run tests ────────────────────────────────────────────────────────────────

exec go test \
    -race \
    -tags integration \
    -timeout 300s \
    -count=1 \
    -v \
    ./integration/ \
    "$@"
