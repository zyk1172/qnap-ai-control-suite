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
  "listen": "0.0.0.0:8756",
  "token_sha256": "$HASH",
  "allowed_roots": ["/share"],
  "allowed_commands": ["/bin/df", "/bin/ps", "/bin/uname", "/sbin/getcfg", "/sbin/ifconfig", "/sbin/qpkg_cli", "/usr/bin/uptime"],
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
  ],
  "allow_shell": false,
  "audit_log": "$LOGDIR/audit.jsonl",
  "max_read_bytes": 2097152,
  "command_timeout_seconds": 30
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
  "$BIN" -config "$CONFIG" >> "$STDOUT_LOG" 2>&1 &
  echo $! > "$PIDFILE"
  echo "$QPKG_NAME started"
}

stop() {
  if [ -f "$PIDFILE" ]; then
    PID=$(cat "$PIDFILE")
    if kill -0 "$PID" 2>/dev/null; then
      kill "$PID"
      sleep 2
      kill -0 "$PID" 2>/dev/null && kill -9 "$PID"
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
