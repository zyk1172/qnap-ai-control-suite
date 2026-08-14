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

start() {
  ensure_config
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "$QPKG_NAME is already running"
    exit 0
  fi
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
    if kill -0 "$PID" 2>/dev/null; then
      kill -TERM "$PID"
      COUNT=0
      while kill -0 "$PID" 2>/dev/null && [ "$COUNT" -lt 10 ]; do
        sleep 1
        COUNT=$((COUNT + 1))
      done
      kill -0 "$PID" 2>/dev/null && kill -KILL "$PID"
    fi
    rm -f "$PIDFILE"
  fi
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
