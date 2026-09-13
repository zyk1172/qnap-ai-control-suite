#!/usr/bin/env bash
set -euo pipefail

script="qpkg/shared/qnap-ai-control-agent.sh"

test -f "$script"

grep -Fq 'JOBDIR="$CONFIG_DIR/jobs"' "$script"
grep -Fq 'LEGACY_JOBDIR=/var/lib/qnap-ai-control-agent' "$script"
grep -Fq 'JOURNAL="$JOBDIR/jobs.jsonl"' "$script"
grep -Fq '"journal_path": "$JOURNAL"' "$script"
grep -Fq 'migrate_legacy_job_state' "$script"
grep -Fq '"journal_path": "/etc/config/qnap-ai-control-agent/jobs/jobs.jsonl"' "$script"

# The generated QPKG configuration must never point new installs back to the
# legacy volatile location. The only remaining legacy path references are the
# explicit upgrade migration source and the exact-value rewrite condition.
if awk '/cat > "\$CONFIG" <<EOF/,/^EOF$/' "$script" | grep -Fq '/var/lib/qnap-ai-control-agent/jobs.jsonl'; then
  echo "generated config still uses legacy /var/lib job journal" >&2
  exit 1
fi

# Migration is copy-before-rewrite: history must be copied into the persistent
# location before config.json is switched to it.
copy_line=$(grep -n 'cp -p "$LEGACY_JOURNAL" "$JOURNAL"' "$script" | head -1 | cut -d: -f1)
rewrite_line=$(grep -n 'sed .*journal_path.*var/lib' "$script" | head -1 | cut -d: -f1)
if [[ -z "$copy_line" || -z "$rewrite_line" || "$copy_line" -ge "$rewrite_line" ]]; then
  echo "legacy journal migration must copy state before rewriting config" >&2
  exit 1
fi

echo "QPKG persistent job-state contract OK"
