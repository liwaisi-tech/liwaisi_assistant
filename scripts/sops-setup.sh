#!/usr/bin/env bash
set -euo pipefail

AGE_KEY_DIR="${HOME}/.config/sops/age"
AGE_KEY_FILE="${AGE_KEY_DIR}/keys.txt"

echo "=== Liwaisi Assistant -- SOPS + age setup ==="
echo ""

# ── Check dependencies ──────────────────────────────────────
missing=0

if ! command -v age-keygen &>/dev/null; then
    echo "[MISSING] age is not installed."
    echo "  macOS:  brew install age"
    echo "  Linux:  sudo apt install age   (or see https://github.com/FiloSottile/age)"
    missing=1
fi

if ! command -v sops &>/dev/null; then
    echo "[MISSING] sops is not installed."
    echo "  macOS:  brew install sops"
    echo "  Linux:  see https://github.com/getsops/sops/releases"
    missing=1
fi

if [ "$missing" -eq 1 ]; then
    echo ""
    echo "Install the missing tools above and re-run this script."
    exit 1
fi

echo "[OK] age and sops are installed."
echo ""

# ── Generate age keypair ────────────────────────────────────
if [ -f "$AGE_KEY_FILE" ]; then
    echo "[OK] Age key already exists at ${AGE_KEY_FILE}"
else
    echo "Generating a new age keypair..."
    mkdir -p "$AGE_KEY_DIR"
    age-keygen -o "$AGE_KEY_FILE" 2>&1
    echo "[OK] Key written to ${AGE_KEY_FILE}"
fi

echo ""

# ── Extract and display public key ──────────────────────────
PUBLIC_KEY=$(grep -oP 'age1\w+' "$AGE_KEY_FILE" | head -1)

echo "Your age public key:"
echo ""
echo "  ${PUBLIC_KEY}"
echo ""
echo "Next steps:"
echo "  1. Replace the placeholder in .sops.yaml with this public key."
echo "  2. Run 'make sops-encrypt' to encrypt .env.sops.yaml."
echo "  3. Run 'make sops-edit' to edit secrets in your editor."
echo ""
echo "[WARNING] Back up your private key (${AGE_KEY_FILE})."
echo "          If you lose it, you will not be able to decrypt secrets."
