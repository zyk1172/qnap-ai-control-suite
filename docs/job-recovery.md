# Job Persistence and Restart Recovery

QACS is a deterministic control layer. Restart recovery records execution facts; it does not replace Hermes/OpenClaw/Codex planning or autonomously replay operations.

## Durable files

For QPKG installs the default state lives under:

```text
/etc/config/qnap-ai-control-agent/jobs/
├── jobs.jsonl
├── jobs.jsonl.snapshot.json
└── jobs.jsonl.logs/
    └── <job-id>.jsonl
```

`jobs.jsonl` is a write-ahead lifecycle journal. Once it reaches the compaction threshold, QACS writes the latest retained Job records to a temporary snapshot, fsyncs it, atomically renames it, then truncates the journal. If snapshot creation fails, the journal is not truncated.

Each per-Job log file stores bounded JSON-string records. When audit redaction is enabled, command output is redacted before it enters the in-memory Job record or the per-Job file; reading an older file after restart applies the same redaction at the response boundary. Log pagination continues to work after agent/QPKG restart. The same existing in-memory limits still apply: at most 1000 retained lines and 16 MiB per Job.

## Persisted result policy

QACS never writes raw command stdout/stderr into the lifecycle journal. For command execution it persists only a summary such as exit code, duration, dry-run state, output byte counts and truncation flags.

Non-command structured results are passed through the existing audit sanitization rules and are persisted only when their serialized form is at most 256 KiB. Larger or unserializable results set `result_truncated=true` instead.

## Restart semantics

On startup QACS loads snapshot state first and then replays newer journal records. A Job that had reached a terminal state remains terminal.

A Job that was `queued` or `running` when the previous process stopped is converted to:

```json
{
  "status": "interrupted",
  "recovered": true,
  "recovery_status": "needs_inspection",
  "retriable": false,
  "error": "agent restarted before job completed"
}
```

`retriable=false` is intentional. A generic Job manager cannot know whether a firmware write, snapshot restore, storage mutation, VM action or another side effect partially completed. QACS therefore never blindly repeats an interrupted operation. The caller should re-read the affected resource and decide whether the requested operation is already complete, should be issued again, or needs a different corrective action.

When the QACS service receives a graceful stop or restart, the Job manager enters a stopping state so new Job submissions are rejected, cancels active Job contexts, and waits for their executors to terminate before the process exits. Command executors terminate the complete Unix process group (`SIGTERM`, then `SIGKILL` after the grace period). The lifecycle record is deliberately kept in its last `queued` or `running` state during this shutdown so the next process still recovers it as `interrupted`; QACS does not persist a misleading `cancelled` result and never replays the command.

## QPKG upgrade migration

Older QPKG versions generated:

```text
/var/lib/qnap-ai-control-agent/jobs.jsonl
```

The service startup script now treats that exact path as the legacy default. During upgrade it copies any existing journal/snapshot/log directory into `/etc/config/qnap-ai-control-agent/jobs/` before atomically rewriting only that exact generated `journal_path` value in `config.json`. Custom user-selected journal paths are not rewritten.

This makes the default Job lifecycle state survive both QPKG restart and a normal NAS reboot while preserving backwards compatibility with existing installations.
