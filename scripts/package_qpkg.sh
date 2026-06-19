#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="${1:-amd64}"
BUILD_DIR="$ROOT/build/qpkg"
QPKG_DIR="$BUILD_DIR/QnapAIControl"

rm -rf "$BUILD_DIR"
mkdir -p "$QPKG_DIR/bin" "$QPKG_DIR/shared" "$ROOT/dist"

GOOS=linux GOARCH="$ARCH" "$ROOT/scripts/build_agent.sh"
cp "$ROOT/dist/linux-$ARCH/qnap-ai-control-agent" "$QPKG_DIR/bin/"
cp "$ROOT/qpkg/qpkg.cfg" "$BUILD_DIR/"
cp "$ROOT/qpkg/shared/qnap-ai-control-agent.sh" "$QPKG_DIR/shared/"
chmod +x "$QPKG_DIR/bin/qnap-ai-control-agent" "$QPKG_DIR/shared/qnap-ai-control-agent.sh"

if command -v qbuild >/dev/null 2>&1; then
  cd "$BUILD_DIR"
  qbuild
  find "$BUILD_DIR" -maxdepth 2 -name "*.qpkg" -print
else
  tar -C "$BUILD_DIR" -czf "$ROOT/dist/QnapAIControl-0.2.0-linux-$ARCH.qpkg-staging.tar.gz" .
  echo "qbuild not found; wrote staging archive:"
  echo "$ROOT/dist/QnapAIControl-0.2.0-linux-$ARCH.qpkg-staging.tar.gz"
  echo "Install QNAP QDK and rerun this script to produce a real .qpkg."
fi
