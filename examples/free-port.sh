#!/usr/bin/env bash
# free-port.sh <port> — exit 0 if the port is free, otherwise print who is
# holding it (project, branch, age) and exit 1. Designed for `predev`
# hooks: `bash examples/free-port.sh 3000 && npm run dev`.
set -euo pipefail

PORT="${1:?usage: free-port.sh <port>}"
DEVPS="${DEVPS:-devps}"

# --all: even an infra listener makes the port unusable.
OUT="$("$DEVPS" list --all "$PORT")"
if printf '%s\n' "$OUT" | grep -q "^${PORT}[[:space:]]"; then
  echo "port ${PORT} is taken:" >&2
  printf '%s\n' "$OUT" >&2
  echo >&2
  echo "hint: devps kill ${PORT}    # SIGTERM the owner (guarded)" >&2
  exit 1
fi
echo "port ${PORT} is free"
