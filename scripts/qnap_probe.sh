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
CONTAINER_DOCKER=""
for p in /share/CACHEDEV*_DATA/.qpkg/container-station/bin/system-docker /share/CACHEDEV*_DATA/.qpkg/container-station/usr/bin/.libs/docker; do
  if [ -x "$p" ]; then CONTAINER_DOCKER="$p"; break; fi
done
ADAPTER_BINARIES="$(find /share/CACHEDEV*_DATA/.qpkg -maxdepth 5 -type f -perm -111 2>/dev/null | grep -Ei 'qkvm|virtual|switch|network|hbs|hybrid|iscsi|cert|smb|nfs|share|firmware|notify|notification|storage|snapshot' | head -160 || true)"
CERTIFICATE_PATHS="$(find /etc/config -maxdepth 4 -type f \( -name '*.pem' -o -name '*.crt' -o -name '*.cer' \) 2>/dev/null | head -100 || true)"
exists() { [ -e "$1" ] && printf true || printf false; }

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
    "zfs": "$(json_string "$(command_path zfs)")",
    "qcli_storage": "$(json_string "$(command_path qcli_storage)")",
    "snapshot_util": "$(json_string "$(command_path snapshot_util)")",
    "upsc": "$(json_string "$(command_path upsc)")",
    "ip": "$(json_string "$(command_path ip)")",
    "container_station_docker": "$(json_string "$CONTAINER_DOCKER")"
  },
  "configs": {
    "smb": $(exists /etc/config/smb.conf),
    "nfs_exports": $(exists /etc/config/exports),
    "iscsi": $(exists /etc/config/iscsi.conf)
  },
  "adapter_executables": "$(json_string "$ADAPTER_BINARIES")",
  "certificate_paths": "$(json_string "$CERTIFICATE_PATHS")",
  "mount": "$(json_string "$MOUNTS")",
  "mdstat": "$(json_string "$MDSTAT")",
  "qpkg_conf": "$(json_string "$QPKG")"
}
EOF
printf '%s\n' "$OUT"
