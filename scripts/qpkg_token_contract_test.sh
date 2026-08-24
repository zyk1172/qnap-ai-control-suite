#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVICE="$ROOT/qpkg/shared/qnap-ai-control-agent.sh"

# The service script may secure a legacy file, but it must not copy legacy
# plaintext into the canonical token path. Hash validation and migration are
# owned by the Go TokenStore.
if grep -Fq 'cp "$CONFIG_DIR/initial-token.txt"' "$SERVICE" || grep -Fq '.token.migrate.tmp' "$SERVICE"; then
  echo "QPKG service script still performs unverified legacy token migration" >&2
  exit 1
fi
grep -Fq 'printf' "$SERVICE"
grep -Fq '.token.install.tmp' "$SERVICE"
grep -Fq 'chmod 600 "$CONFIG_DIR/initial-token.txt"' "$SERVICE"
