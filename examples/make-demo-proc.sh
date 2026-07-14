#!/usr/bin/env bash
# Fabricate a demo proc tree + fake projects so you can try every devps
# flag without leaving real servers running. Usage:
#
#   bash examples/make-demo-proc.sh /tmp/devps-demo
#   devps list --proc-root /tmp/devps-demo/proc
#
# The tree contains: vite on 5173 (feature/checkout), next on 3000 (main),
# a django runserver on 8000 (fix/api-timeout), an unnamed go binary on
# 9090 inside a repo, and postgres + sshd noise that stays hidden.
set -euo pipefail

DEST="${1:?usage: make-demo-proc.sh <dest-dir>}"
rm -rf "$DEST"
PROC="$DEST/proc"
mkdir -p "$PROC/net"

NOW="$(date +%s)"
printf 'cpu  0 0 0 0 0 0 0 0 0 0\nbtime %s\nprocesses 1\n' "$((NOW - 9*86400))" > "$PROC/stat"

repo() { # repo <dir> <branch>
  mkdir -p "$1/.git"
  printf 'ref: refs/heads/%s\n' "$2" > "$1/.git/HEAD"
}
repo "$DEST/src/shop-frontend" "feature/checkout"
repo "$DEST/src/store-web" "main"
repo "$DEST/src/billing-api" "fix/api-timeout"
repo "$DEST/src/metrics-relay" "main"

# hexport <port> prints the kernel's uppercase hex port.
hexport() { printf '%04X' "$1"; }

TCP="$PROC/net/tcp"
printf '  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n' > "$TCP"
sock() { # sock <sl> <hexaddr> <port> <uid> <inode>
  printf '   %s: %s:%s 00000000:0000 0A 00000000:00000000 00:00000000 00000000  %s        0 %s 1 0000000000000000 100 0 0 10 0\n' \
    "$1" "$2" "$(hexport "$3")" "$4" "$5" >> "$TCP"
}
sock 0 0100007F 5173 1000 501   # 127.0.0.1:5173 vite
sock 1 0100007F 3000 1000 502   # 127.0.0.1:3000 next
sock 2 00000000 8000 1000 503   # 0.0.0.0:8000   django
sock 3 0100007F 9090 1000 504   # 127.0.0.1:9090 go binary
sock 4 0100007F 5432 70   505   # 127.0.0.1:5432 postgres (hidden)
sock 5 00000000 22   0    506   # 0.0.0.0:22     sshd (hidden)

proc() { # proc <pid> <comm> <cwd> <inode> <start_ticks> <argv...>
  local pid="$1" comm="$2" cwd="$3" inode="$4" ticks="$5"; shift 5
  mkdir -p "$PROC/$pid/fd"
  printf '%s (%s) S 1 1 1 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 %s 1024 100 0\n' \
    "$pid" "$comm" "$ticks" > "$PROC/$pid/stat"
  printf 'Name:\t%s\nUid:\t1000\t1000\t1000\t1000\n' "$comm" > "$PROC/$pid/status"
  printf '%s\0' "$@" > "$PROC/$pid/cmdline"
  ln -s "$cwd" "$PROC/$pid/cwd"
  ln -s "socket:[$inode]" "$PROC/$pid/fd/3"
}
# start_ticks are seconds-after-boot × 100 (USER_HZ).
proc 4102 node "$DEST/src/shop-frontend" 501 "$((7*86400*100))" \
  node "$DEST/src/shop-frontend/node_modules/.bin/vite"
proc 3987 node "$DEST/src/store-web" 502 "$(( (9*86400 - 3*3600 - 12*60) * 100 ))" \
  node "$DEST/src/store-web/node_modules/.bin/next" dev
proc 5210 python3 "$DEST/src/billing-api" 503 "$(( (9*86400 - 26*3600) * 100 ))" \
  python3 manage.py runserver 0.0.0.0:8000
proc 6001 metrics-relay "$DEST/src/metrics-relay" 504 "$(( (9*86400 - 45*60) * 100 ))" \
  ./metrics-relay --listen 127.0.0.1:9090
proc 900 postgres / 505 100 /usr/lib/postgresql/16/bin/postgres
proc 811 sshd / 506 100 /usr/sbin/sshd -D

# go run fingerprint for the metrics relay: exe under the build cache.
ln -s /tmp/go-build1234/b001/exe/metrics-relay "$PROC/6001/exe"

echo "demo tree ready — try:"
echo "  devps list --proc-root $PROC"
echo "  devps list --all --format json --proc-root $PROC"
echo "  devps kill --dry-run --proc-root $PROC 5173"
