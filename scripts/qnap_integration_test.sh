#!/bin/sh
set -eu

BASE_URL="${QACS_BASE_URL:?QACS_BASE_URL is required}"
TOKEN="${QACS_TOKEN:?QACS_TOKEN is required}"
AUTH="Authorization: Bearer $TOKEN"

call() { curl -fsS --max-time 30 -H "$AUTH" "$BASE_URL$1"; }
call /v1/health >/dev/null
call /v1/capabilities >/dev/null
call /v1/qnap/discovery >/dev/null
call /v1/system/overview >/dev/null
call /v1/storage/overview >/dev/null
call /v1/network/info >/dev/null
printf '%s\n' "QNAP agent integration checks passed"
