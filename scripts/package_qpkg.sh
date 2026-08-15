#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="${1:-amd64}"
QPKG_ARCH="${QPKG_ARCH:-generic}"
BUILD_DIR="$ROOT/build/qpkg"
QDK_DIR="${QDK_DIR:-$ROOT/tools/QDK/shared}"
QBUILD_BIN="${QBUILD_BIN:-}"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"

if [ "$(uname -s)" = "Darwin" ] && [ -x "$QDK_DIR/bin/qbuild" ]; then
  mkdir -p "$ROOT/tools/qdk-macos/bin"
  cp "$QDK_DIR/bin/qbuild" "$ROOT/tools/qdk-macos/bin/qbuild"
  perl -0pi \
    -e 's#/bin/grep#/usr/bin/grep#g;' \
    -e 's#/bin/sed#/usr/bin/sed#g;' \
    -e 's#/bin/gzip#/usr/bin/gzip#g;' \
    -e 's#/bin/gunzip#/usr/bin/gunzip#g;' \
    -e 's#/bin/bunzip2#/usr/bin/bunzip2#g;' \
    -e 's#/bin/cut#/usr/bin/cut#g;' \
    -e 's#/bin/rsync#/usr/bin/rsync#g;' \
    -e 's#/bin/hexdump#/usr/bin/hexdump#g;' \
    -e 's#/bin/basename#/usr/bin/basename#g;' \
    -e 's#/bin/tar rf tmp#/usr/bin/tar rf tmp#g;' \
    -e 's#/bin/tar -cvzf#/usr/bin/tar -cvzf#g;' \
    -e 's#/bin/tar -cvjf#/usr/bin/tar -cvjf#g;' \
    -e 's#/bin/tar \$tar_verbose#/usr/bin/tar \$tar_verbose#g;' \
    -e 's#/bin/tar -cf tmp#/usr/bin/tar -cf tmp#g;' \
    -e 's#/bin/tar -xOf#/usr/bin/tar -xOf#g;' \
    -e 's#/bin/tar xzO#/usr/bin/tar xzO#g;' \
    -e 's#/bin/tar t#/usr/bin/tar t#g;' \
    -e 's#\|[[:space:]]*grep #| /usr/bin/grep #g;' \
    -e 's#/usr/usr/bin/#/usr/bin/#g;' \
    "$ROOT/tools/qdk-macos/bin/qbuild"
  perl -0pi \
    -e 's#/usr/bin/tar -cvzf#/usr/bin/tar --no-xattrs --format ustar -cvzf#g;' \
    -e 's#/usr/bin/tar -cvjf#/usr/bin/tar --no-xattrs --format ustar -cvjf#g;' \
    -e 's#/usr/bin/tar -cf tmp#/usr/bin/tar --no-xattrs --format ustar -cf tmp#g;' \
    -e 's#/usr/bin/tar \$tar_verbose -cf#/usr/bin/tar --no-xattrs --format ustar \$tar_verbose -cf#g;' \
    "$ROOT/tools/qdk-macos/bin/qbuild"
  # Keep NAS-side bootstrap paths intact. QNAP has /bin/grep but not
  # /usr/bin/grep on TS-264C/QTS, while macOS needs /usr/bin/grep for qbuild.
  perl -0pi \
    -e 's#/usr/bin/grep "/mnt/HDA_ROOT"#/bin/grep "/mnt/HDA_ROOT"#g;' \
    "$ROOT/tools/qdk-macos/bin/qbuild"
  perl -0pi -e 's#\t/usr/bin/sed -i "s/SCRIPT_LEN/\$script_len/" \$QDK_QPKG_FILE#\t/usr/bin/perl -0pi -e "s/SCRIPT_LEN/\$script_len/" "\$QDK_QPKG_FILE"#g' "$ROOT/tools/qdk-macos/bin/qbuild"
  chmod +x "$ROOT/tools/qdk-macos/bin/qbuild"
fi

if [ -d "$ROOT/tools/QDK/src" ] && [ ! -x "$ROOT/tools/QDK/src/bin/qpkg_encrypt" ]; then
  make -C "$ROOT/tools/QDK/src"
fi

if [ -z "$QBUILD_BIN" ] && [ -x "$ROOT/tools/qdk-macos/bin/qbuild" ]; then
  QBUILD_BIN="$ROOT/tools/qdk-macos/bin/qbuild"
elif [ -z "$QBUILD_BIN" ] && command -v qbuild >/dev/null 2>&1; then
  QBUILD_BIN="$(command -v qbuild)"
elif [ -z "$QBUILD_BIN" ] && [ -x "$QDK_DIR/bin/qbuild" ]; then
  QBUILD_BIN="$QDK_DIR/bin/qbuild"
fi

rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/shared/bin" "$ROOT/dist"

GOOS=linux GOARCH="$ARCH" "$ROOT/scripts/build_agent.sh"
cp "$ROOT/dist/linux-$ARCH/qnap-ai-control-agent" "$BUILD_DIR/shared/bin/"
cp "$ROOT/scripts/qnap_probe.sh" "$BUILD_DIR/shared/bin/qnap-ai-control-probe"
cp "$ROOT/qpkg/qpkg.cfg" "$BUILD_DIR/"
perl -0pi -e "s/__VERSION__/$VERSION/g" "$BUILD_DIR/qpkg.cfg"
cp "$ROOT/qpkg/shared/qnap-ai-control-agent.sh" "$BUILD_DIR/shared/"
if [ -d "$ROOT/qpkg/icons" ]; then
  mkdir -p "$BUILD_DIR/icons"
  cp "$ROOT/qpkg/icons"/QnapAIControl* "$BUILD_DIR/icons/"
fi
if [ -f "$ROOT/qpkg/package_routines" ]; then
  cp "$ROOT/qpkg/package_routines" "$BUILD_DIR/package_routines"
elif [ -f "$QDK_DIR/template/package_routines" ]; then
  cp "$QDK_DIR/template/package_routines" "$BUILD_DIR/package_routines"
else
  touch "$BUILD_DIR/package_routines"
fi
chmod +x "$BUILD_DIR/shared/bin/qnap-ai-control-agent" "$BUILD_DIR/shared/bin/qnap-ai-control-probe" "$BUILD_DIR/shared/qnap-ai-control-agent.sh"

if command -v xattr >/dev/null 2>&1; then
  xattr -cr "$BUILD_DIR" 2>/dev/null || true
fi

cat > "$BUILD_DIR/qdk.conf" <<EOF
QDK_VERSION=2.5.2
QDK_PATH="$QDK_DIR"
EOF

if [ -n "$QBUILD_BIN" ]; then
  cd "$BUILD_DIR"
  if [ "$QPKG_ARCH" = "generic" ]; then
    COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 PATH="$ROOT/tools/QDK/src/bin:$QDK_DIR/bin:$PATH" QDK_PATH="$BUILD_DIR/no-qdk-conf-here" "$QBUILD_BIN" --build-dir "$ROOT/dist"
  else
    COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 PATH="$ROOT/tools/QDK/src/bin:$QDK_DIR/bin:$PATH" QDK_PATH="$BUILD_DIR/no-qdk-conf-here" "$QBUILD_BIN" --build-arch "$QPKG_ARCH" --build-dir "$ROOT/dist"
  fi
  find "$ROOT/dist" -maxdepth 1 -name "*.qpkg" -print
else
  tar -C "$BUILD_DIR" -czf "$ROOT/dist/QnapAIControl-$VERSION-linux-$ARCH.qpkg-staging.tar.gz" .
  echo "qbuild not found; wrote staging archive:"
  echo "$ROOT/dist/QnapAIControl-$VERSION-linux-$ARCH.qpkg-staging.tar.gz"
  echo "Install QNAP QDK and rerun this script to produce a real .qpkg."
fi
