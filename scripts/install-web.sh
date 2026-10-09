#!/usr/bin/env bash
# Build and install both binaries, then start the read-only dashboard
# (mcp-task-manager-web) detached for this project.
#
# Always rebuilds from source and replaces any dashboard already listening on
# the port, so what opens is the fresh build.
#
# Usage: scripts/install-web.sh [--addr host:port] [--no-open] [--keep]
#   --keep  leave an already running dashboard alone (old build stays up)
set -euo pipefail

ADDR="127.0.0.1:7777"
OPEN=1
KEEP=0
while [ $# -gt 0 ]; do
  case "$1" in
    --addr)    ADDR="${2:?--addr needs a value}"; shift 2 ;;
    --no-open) OPEN=0; shift ;;
    --keep)    KEEP=1; shift ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
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

PORT="${ADDR##*:}"
listeners() { lsof -tiTCP:"$PORT" -sTCP:LISTEN -n -P 2>/dev/null || true; }
if [ -n "$(listeners)" ]; then
  if [ "$KEEP" = 1 ]; then
    echo "==> :$PORT is already serving; leaving it (--keep), it may be an old build"
    [ "$OPEN" = 1 ] && command -v open >/dev/null && open "http://$ADDR/"
    echo "http://$ADDR/"
    exit 0
  fi
  echo "==> stopping dashboard on :$PORT"
  # shellcheck disable=SC2046
  kill $(listeners) 2>/dev/null || true
  for _ in $(seq 1 20); do [ -z "$(listeners)" ] && break; sleep 0.25; done
  if [ -n "$(listeners)" ]; then
    # shellcheck disable=SC2046
    kill -9 $(listeners) 2>/dev/null || true
    sleep 0.5
  fi
  if [ -n "$(listeners)" ]; then
    echo "could not free :$PORT" >&2
    exit 1
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
