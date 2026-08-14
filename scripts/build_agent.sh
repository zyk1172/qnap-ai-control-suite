#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
OUT="$ROOT/dist/$GOOS-$GOARCH/qnap-ai-control-agent"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"

mkdir -p "$(dirname "$OUT")"
cd "$ROOT/agent"
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags="-s -w -X qnap-ai-control-suite/agent/internal/api.Version=$VERSION" -o "$OUT" ./cmd/qnap-ai-control-agent
echo "$OUT"
