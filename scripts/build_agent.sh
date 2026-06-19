#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
OUT="$ROOT/dist/$GOOS-$GOARCH/qnap-ai-control-agent"

mkdir -p "$(dirname "$OUT")"
cd "$ROOT/agent"
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags="-s -w" -o "$OUT" ./cmd/qnap-ai-control-agent
echo "$OUT"

