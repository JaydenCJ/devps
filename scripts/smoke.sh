#!/usr/bin/env bash
# End-to-end smoke test for devps: builds the binary, checks it against a
# fabricated proc tree with known answers, then starts a REAL listener
# inside a real git checkout and asserts devps finds it on the live /proc
# and can kill it. Loopback only, no external network, idempotent.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
SRV_PID=""
cleanup() {
  [ -n "$SRV_PID" ] && kill -9 "$SRV_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/devps"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/devps) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" --version | grep -qx "devps 0.1.0" || fail "--version mismatch"

echo "3. fabricate a proc tree with known listeners"
PROC="$WORKDIR/fakeproc"
PROJ="$WORKDIR/projects/shop-frontend"
mkdir -p "$PROC/net" "$PROJ/.git"
printf 'ref: refs/heads/feature/checkout\n' > "$PROJ/.git/HEAD"
NOW="$(date +%s)"
# Boot 3 days ago; the vite process started 1000 ticks (10 s) after boot.
printf 'cpu  0 0 0 0 0 0 0 0 0 0\nbtime %s\nprocesses 1\n' "$((NOW - 3*86400))" > "$PROC/stat"
# 127.0.0.1:5173 (0100007F:1435) LISTEN, inode 500 — vite.
# 0.0.0.0:22    (00000000:0016) LISTEN, inode 600 — sshd.
{
  printf '  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n'
  printf '   0: 0100007F:1435 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 500 1 0000000000000000 100 0 0 10 0\n'
  printf '   1: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 600 1 0000000000000000 100 0 0 10 0\n'
} > "$PROC/net/tcp"
mkproc() { # mkproc <pid> <comm> <cwd> <inode> <argv...>
  local pid="$1" comm="$2" cwd="$3" inode="$4"; shift 4
  mkdir -p "$PROC/$pid/fd"
  printf '%s (%s) S 1 1 1 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 1000 1024 100 0\n' "$pid" "$comm" > "$PROC/$pid/stat"
  printf 'Name:\t%s\nUid:\t1000\t1000\t1000\t1000\n' "$comm" > "$PROC/$pid/status"
  printf '%s\0' "$@" > "$PROC/$pid/cmdline"
  ln -s "$cwd" "$PROC/$pid/cwd"
  ln -s "socket:[$inode]" "$PROC/$pid/fd/3"
}
mkproc 4102 node "$PROJ" 500 node "$PROJ/node_modules/.bin/vite"
mkproc 811 sshd / 600 /usr/sbin/sshd -D

echo "4. table joins port, project, and branch"
OUT="$("$BIN" list --proc-root "$PROC")"
echo "$OUT" | grep -q "5173" || fail "port 5173 missing"
echo "$OUT" | grep -q "vite" || fail "vite label missing"
echo "$OUT" | grep -q "shop-frontend" || fail "project column missing"
echo "$OUT" | grep -q "feature/checkout" || fail "branch column missing"
echo "$OUT" | grep -q "2d23h" || fail "age should be 2d23h (boot-3d + 10s start)"
if echo "$OUT" | grep -q "sshd"; then fail "sshd must be hidden by default"; fi
echo "$OUT" | grep -q "1 other listener hidden" || fail "hidden note missing"

echo "5. --all reveals infra; JSON schema is stable"
"$BIN" list --all --proc-root "$PROC" | grep -q "sshd" || fail "--all must show sshd"
JSON="$("$BIN" list --format json --proc-root "$PROC")"
echo "$JSON" | grep -q '"tool": "devps"' || fail "json envelope missing"
echo "$JSON" | grep -q '"branch": "feature/checkout"' || fail "json branch missing"
echo "$JSON" | grep -q '"hidden": 1' || fail "json hidden count missing"

echo "6. port filter and dry-run kill"
"$BIN" --proc-root "$PROC" 5173 | grep -q "vite" || fail "bare-port filter failed"
"$BIN" kill --dry-run --proc-root "$PROC" 5173 \
  | grep -q "would send SIGTERM to pid 4102" || fail "dry-run wrong"
if "$BIN" kill --dry-run --proc-root "$PROC" 22 2>/dev/null; then
  fail "kill must refuse sshd without --force"
fi

echo "7. real end-to-end: live listener in a real repo on the live /proc"
REPO="$WORKDIR/billing-api"
mkdir -p "$REPO/.git"
printf 'ref: refs/heads/fix/rounding\n' > "$REPO/.git/HEAD"
cat > "$WORKDIR/demosrv.go" <<'EOF'
// demosrv: bind a loopback port, record it, then serve until killed.
package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(os.Args[1], []byte(fmt.Sprint(port)), 0o644); err != nil {
		panic(err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Close()
	}
}
EOF
(cd "$ROOT" && go build -o "$WORKDIR/demosrv" "$WORKDIR/demosrv.go")
rm -f "$WORKDIR/port.txt"
(cd "$REPO" && exec "$WORKDIR/demosrv" "$WORKDIR/port.txt") &
SRV_PID=$!
for _ in $(seq 1 50); do
  [ -s "$WORKDIR/port.txt" ] && break
  sleep 0.1
done
[ -s "$WORKDIR/port.txt" ] || fail "demo server never reported its port"
PORT="$(cat "$WORKDIR/port.txt")"

OUT="$("$BIN" list "$PORT")"
echo "$OUT" | grep -q "$PORT" || fail "live port $PORT not listed"
echo "$OUT" | grep -q "billing-api" || fail "live project not resolved"
echo "$OUT" | grep -q "fix/rounding" || fail "live branch not resolved"

echo "8. kill the live listener by port"
"$BIN" kill "$PORT" | grep -q "sent SIGTERM to pid $SRV_PID" || fail "kill confirmation wrong"
wait "$SRV_PID" 2>/dev/null || true
if kill -0 "$SRV_PID" 2>/dev/null; then
  fail "listener survived devps kill"
fi
SRV_PID=""

echo "9. usage errors exit 2"
set +e
"$BIN" list --format yaml >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
"$BIN" kill >/dev/null 2>&1
[ $? -eq 2 ] || fail "kill without a port should exit 2"
set -e

echo "SMOKE OK"
