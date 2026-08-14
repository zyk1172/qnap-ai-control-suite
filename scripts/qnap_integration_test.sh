#!/bin/sh
set -eu

# Run from a trusted Mac or directly on the NAS. The script only performs
# authenticated GET requests plus executor dry-runs; it never changes NAS state.
BASE_URL="${QNAP_AI_CONTROL_URL:-${QACS_BASE_URL:-http://127.0.0.1:8756}}"
TOKEN="${QNAP_AI_CONTROL_TOKEN:-${QACS_TOKEN:-}}"

if [ -z "$TOKEN" ]; then
  echo "QNAP_AI_CONTROL_TOKEN (or legacy QACS_TOKEN) is required" >&2
  exit 2
fi

request() {
  method="$1"
  path="$2"
  body="${3:-}"
  if [ -n "$body" ]; then
    curl -fsS --connect-timeout 5 --max-time 30 \
      -X "$method" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      --data "$body" "$BASE_URL$path"
  else
    curl -fsS --connect-timeout 5 --max-time 30 \
      -X "$method" -H "Authorization: Bearer $TOKEN" "$BASE_URL$path"
  fi
}

assert_ok() {
  name="$1"
  response="$2"
  case "$response" in
    *'"ok":true'*) printf 'PASS %s\n' "$name" ;;
    *) printf 'FAIL %s: %s\n' "$name" "$response" >&2; return 1 ;;
  esac
}

check_required() {
  name="$1"
  method="$2"
  path="$3"
  body="${4:-}"
  response="$(request "$method" "$path" "$body")" || {
    printf 'FAIL %s: request failed\n' "$name" >&2
    return 1
  }
  assert_ok "$name" "$response"
}

check_optional() {
  name="$1"
  method="$2"
  path="$3"
  body="${4:-}"
  if response="$(request "$method" "$path" "$body" 2>/dev/null)"; then
    assert_ok "$name" "$response" || return 1
  else
    printf 'SKIP %s: optional subsystem unavailable\n' "$name"
  fi
}

printf 'Testing %s\n' "$BASE_URL"
check_required health GET /v1/health
check_required capabilities GET /v1/capabilities
check_required discovery GET /v1/qnap/discovery
check_required system-info GET /v1/system/info
check_required system-resources GET /v1/system/resources
check_required thermal GET /v1/system/thermal
check_required storage GET /v1/storage/overview
check_required network GET /v1/network/info
check_required logs GET /v1/logs
check_required executor-dry-run POST /v1/exec '{"argv":["/bin/true"],"dry_run":true}'
check_required shell-dry-run POST /v1/shell '{"shell":"/bin/sh","script":"true","dry_run":true}'

check_optional docker-info GET /v1/docker/info
check_optional qpkg GET /v1/qnap/qpkg
check_optional ups GET /v1/qnap/ups
check_optional users GET /v1/users
check_optional groups GET /v1/groups
check_optional shares GET /v1/shares
check_optional ecosystem GET /v1/qnap/ecosystem

printf 'PASS integration checks completed\n'
