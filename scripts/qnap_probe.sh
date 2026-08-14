#!/bin/sh
set -eu

OUT="${1:-dist/qnap-probe.json}"
mkdir -p "$(dirname "$OUT")"

json_string() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/$/\\n/' | tr -d '\n'; }
command_path() { command -v "$1" 2>/dev/null || true; }
capture() { "$@" 2>&1 | head -c 32768 || true; }

MODEL="$(capture /sbin/getsysinfo model)"
FIRMWARE="$(capture /sbin/getsysinfo version)"
ARCH="$(uname -m 2>/dev/null || true)"
MOUNTS="$(capture mount)"
MDSTAT="$(capture cat /proc/mdstat)"
QPKG="$(capture cat /etc/config/qpkg.conf)"

cat > "$OUT" <<EOF
{
  "captured_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "model": "$(json_string "$MODEL")",
  "firmware": "$(json_string "$FIRMWARE")",
  "arch": "$(json_string "$ARCH")",
  "utilities": {
    "getcfg": "$(json_string "$(command_path getcfg)")",
    "setcfg": "$(json_string "$(command_path setcfg)")",
    "qpkg_cli": "$(json_string "$(command_path qpkg_cli)")",
    "getsysinfo": "$(json_string "$(command_path getsysinfo)")",
    "docker": "$(json_string "$(command_path docker)")",
    "smartctl": "$(json_string "$(command_path smartctl)")",
    "mdadm": "$(json_string "$(command_path mdadm)")",
    "zpool": "$(json_string "$(command_path zpool)")",
    "zfs": "$(json_string "$(command_path zfs)")"
  },
  "mount": "$(json_string "$MOUNTS")",
  "mdstat": "$(json_string "$MDSTAT")",
  "qpkg_conf": "$(json_string "$QPKG")"
}
EOF
printf '%s\n' "$OUT"
