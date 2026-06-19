#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${1:-$ROOT/configs/dev-agent-config.json}"

if [ ! -f "$CONFIG" ]; then
  (cd "$ROOT/agent" && go build -o /tmp/qnap-ai-control-agent ./cmd/qnap-ai-control-agent)
  TOKEN="$(/tmp/qnap-ai-control-agent -generate-token)"
  HASH="$(printf "%s" "$TOKEN" | /tmp/qnap-ai-control-agent -print-token-hash)"
  cat > "$CONFIG" <<EOF
{
  "listen": "127.0.0.1:8756",
  "token_sha256": "$HASH",
  "allowed_roots": ["$ROOT"],
  "allowed_commands": ["/bin/df", "/bin/ps", "/bin/uname", "/sbin/getcfg", "/sbin/ifconfig", "/sbin/qpkg_cli", "/usr/bin/uptime"],
  "allow_shell": false,
  "audit_log": "$ROOT/dist/dev-audit.jsonl",
  "max_read_bytes": 2097152,
  "command_timeout_seconds": 30
}
EOF
  echo "Generated dev token: $TOKEN"
fi

cd "$ROOT/agent"
go run ./cmd/qnap-ai-control-agent -config "$CONFIG"
