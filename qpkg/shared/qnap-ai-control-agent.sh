#!/bin/sh

CONF=/etc/config/qpkg.conf
QPKG_NAME=QnapAIControl
QPKG_ROOT=$(/sbin/getcfg "$QPKG_NAME" Install_Path -f "$CONF")
BIN="$QPKG_ROOT/bin/qnap-ai-control-agent"
CONFIG_DIR=/etc/config/qnap-ai-control-agent
CONFIG="$CONFIG_DIR/config.json"
PIDFILE=/var/run/qnap-ai-control-agent.pid
LOGDIR=/var/log/qnap-ai-control-agent
STDOUT_LOG="$LOGDIR/service.log"

ensure_config() {
  mkdir -p "$CONFIG_DIR" "$LOGDIR"
  if [ ! -f "$CONFIG" ]; then
    TOKEN=$("$BIN" -generate-token)
    HASH=$(printf "%s" "$TOKEN" | "$BIN" -print-token-hash)
    umask 077
    cat > "$CONFIG" <<EOF
{
  "version": 1,
  "listen": "0.0.0.0:8756",
  "auth": {"type": "bearer", "token_sha256": "$HASH"},
  "profile": "full_trust",
  "permissions": {"allowed_roots": ["/"], "allow_any_command": true, "allowed_commands": [], "allow_shell": true},
  "privacy": {"redact_secrets": false},
  "confirmation": {"mode": "off", "ttl_seconds": 600},
  "command": {"timeout_seconds": 30, "max_output_bytes": 8388608},
  "files": {"max_inline_bytes": 4194304},
  "jobs": {"max_history": 200},
  "audit": {"enabled": true, "path": "$LOGDIR/audit.jsonl", "redact_secrets": false},
  "docker_paths": [
    "/share/CACHEDEV1_DATA/.qpkg/container-station/bin/docker",
    "/share/CACHEDEV1_DATA/.qpkg/container-station/usr/bin/docker",
    "/share/CACHEDEV2_DATA/.qpkg/container-station/bin/docker",
    "/share/CACHEDEV2_DATA/.qpkg/container-station/usr/bin/docker",
    "/share/CACHEDEV3_DATA/.qpkg/container-station/bin/docker",
    "/share/CACHEDEV3_DATA/.qpkg/container-station/usr/bin/docker",
    "/share/CACHEDEV4_DATA/.qpkg/container-station/bin/docker",
    "/share/CACHEDEV4_DATA/.qpkg/container-station/usr/bin/docker",
    "/share/CACHEDEV5_DATA/.qpkg/container-station/bin/docker",
    "/share/CACHEDEV5_DATA/.qpkg/container-station/usr/bin/docker",
    "/usr/bin/docker",
    "/usr/local/bin/docker",
    "/bin/docker"
  ]
}
EOF
    cat > "$CONFIG_DIR/initial-token.txt" <<EOF
$TOKEN
EOF
    chmod 600 "$CONFIG" "$CONFIG_DIR/initial-token.txt"
  fi
}

stop_agent_pid() {
  case "$1" in
    ''|*[!0-9]*) return 0 ;;
  esac
  if kill -0 "$1" 2>/dev/null; then
    kill -TERM "$1" 2>/dev/null || true
    COUNT=0
    while kill -0 "$1" 2>/dev/null && [ "$COUNT" -lt 10 ]; do
      sleep 1
      COUNT=$((COUNT + 1))
    done
    kill -0 "$1" 2>/dev/null && kill -KILL "$1" 2>/dev/null || true
  fi
}

agent_pid_running() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
  esac
  /bin/ps -ef 2>/dev/null | /bin/awk -v pid="$1" -v bin="$BIN" '$1 == pid && index($0, bin " -config ") { found=1 } END { exit !found }'
}

stop_stale_agents() {
  # QDK upgrades can replace the binary while a pre-v1 process survives with
  # a missing or stale pidfile. Match only this QPKG's exact agent command.
  for PID in $(/bin/ps -ef 2>/dev/null | /bin/awk -v bin="$BIN" 'index($0, bin " -config ") { print $1 }'); do
    stop_agent_pid "$PID"
  done
}

start() {
  ensure_config
  if [ -f "$PIDFILE" ]; then
    PID=$(cat "$PIDFILE")
    if agent_pid_running "$PID"; then
      echo "$QPKG_NAME is already running"
      exit 0
    fi
    rm -f "$PIDFILE"
  fi
  stop_stale_agents
  (
    trap '' HUP
    exec "$BIN" -config "$CONFIG" >> "$STDOUT_LOG" 2>&1
  ) &
  echo $! > "$PIDFILE"
  echo "$QPKG_NAME started"
}

stop() {
  if [ -f "$PIDFILE" ]; then
    PID=$(cat "$PIDFILE")
    agent_pid_running "$PID" && stop_agent_pid "$PID"
    rm -f "$PIDFILE"
  fi
  stop_stale_agents
  echo "$QPKG_NAME stopped"
}

status() {
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "$QPKG_NAME running pid $(cat "$PIDFILE")"
    exit 0
  fi
  echo "$QPKG_NAME stopped"
  exit 1
}

case "$1" in
  start) start ;;
  stop) stop ;;
  restart)
    stop
    start
    ;;
  status) status ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
