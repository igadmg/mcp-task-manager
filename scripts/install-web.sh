#!/usr/bin/env bash
# Build and install both binaries, then start the read-only dashboard
# (mcp-task-manager-web) detached for this project.
#
# Usage: scripts/install-web.sh [--addr host:port] [--no-open] [--restart]
set -euo pipefail

ADDR="127.0.0.1:7777"
OPEN=1
RESTART=0
while [ $# -gt 0 ]; do
  case "$1" in
    --addr)    ADDR="${2:?--addr needs a value}"; shift 2 ;;
    --no-open) OPEN=0; shift ;;
    --restart) RESTART=1; shift ;;
    -h|--help) sed -n '2,6p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export GOWORK=off

echo "==> build"
go build ./...

echo "==> install"
go install ./cmd/mcp-task-manager ./cmd/mcp-task-manager-web
BIN="$(go env GOBIN)"; BIN="${BIN:-$(go env GOPATH)/bin}"
WEB="$BIN/mcp-task-manager-web"
if [ "$(command -v mcp-task-manager-web || true)" != "$WEB" ]; then
  echo "warning: $BIN is not first on PATH for mcp-task-manager-web" >&2
fi

HOSTPORT="${ADDR}"
PORT="${HOSTPORT##*:}"
if lsof -iTCP:"$PORT" -sTCP:LISTEN -n -P >/dev/null 2>&1; then
  if [ "$RESTART" = 1 ]; then
    echo "==> stopping dashboard on :$PORT"
    # shellcheck disable=SC2046
    kill $(lsof -tiTCP:"$PORT" -sTCP:LISTEN) 2>/dev/null || true
    sleep 1
  else
    echo "==> :$PORT is already serving; leaving it (use --restart to replace)"
    [ "$OPEN" = 1 ] && command -v open >/dev/null && open "http://$ADDR/"
    echo "http://$ADDR/"
    exit 0
  fi
fi

echo "==> start dashboard on $ADDR"
LOG="${TMPDIR:-/tmp}/mcp-task-manager-web.log"
MCP_PROJECT_DIR="${MCP_PROJECT_DIR:-$ROOT}" \
  nohup "$WEB" --addr "$ADDR" >"$LOG" 2>&1 &
disown

for _ in $(seq 1 20); do
  if curl -fs "http://$ADDR/healthz" >/dev/null 2>&1; then
    echo "up: http://$ADDR/  (log: $LOG)"
    [ "$OPEN" = 1 ] && command -v open >/dev/null && open "http://$ADDR/"
    exit 0
  fi
  sleep 0.25
done
echo "dashboard did not come up; see $LOG" >&2
tail -n 20 "$LOG" >&2
exit 1
