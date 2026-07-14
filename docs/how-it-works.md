# How devps works

devps answers "what is on port N?" the way a developer asks it — project
and branch, not pid — by joining three views of the kernel's proc
filesystem and one view of git metadata. No external commands are
executed: not `lsof`, not `ss`, not even `git`.

## 1. Listening sockets

`/proc/net/tcp` and `/proc/net/tcp6` list every TCP socket with its state,
local address, owning uid, and — crucially — its **inode**. devps keeps
rows in state `0A` (LISTEN) and decodes the kernel's hex address notation:
each 32-bit word is printed in host (little-endian) byte order, so
`0100007F:1F90` is `127.0.0.1:8080` and a v4-mapped `::ffff:127.0.0.1`
listener is unmapped back to the dotted quad the user actually typed.

## 2. Socket → process

The socket tables name no processes. The join key is the inode:
`/proc/<pid>/fd/` contains one symlink per open descriptor, and socket
descriptors read `socket:[<inode>]`. devps walks every pid directory once,
builds an inode → pid index, and joins. Descriptor tables of other users'
processes are unreadable without root; those listeners are reported as a
count ("rerun with sudo to map"), never guessed at.

## 3. Process → project, branch, age

For each owning pid, three more procfs reads complete the row:

| Source | Yields |
|---|---|
| `/proc/<pid>/cwd` (symlink) | working directory → project |
| `/proc/<pid>/cmdline`, `comm`, `exe` | classification + display label |
| `/proc/<pid>/stat` field 22 + `/proc/stat` btime | start time → age |

`starttime` is measured in USER_HZ ticks (fixed at 100 for the proc
interface) since boot, so `started = btime + ticks/100` needs no syscalls.
The stat parser locates fields after the **last** `)` in the line, because
a process may rename itself to something hostile like `a) S 99 (b`.

From the working directory, devps walks upward looking for `.git`. A
directory is read for `HEAD` directly; a `.git` *file* (linked worktree or
submodule) is followed via its `gitdir:` pointer. `ref: refs/heads/X`
yields the branch, a bare hash yields `detached@<short-hash>`. Reading
files instead of running `git` keeps a full scan under a few milliseconds
and works on machines where git is not installed.

## 4. Classification and the default view

Command lines are matched against a curated rule table (vite, next,
django runserver, rails server, `go run` build-cache binaries, php -S, …)
producing a label and one of three kinds:

- **dev** — recognized dev server: always shown.
- **other** — unrecognized: shown when its cwd is inside a git repository,
  which is the working definition of "a dev server you left running".
- **infra** — sshd, postgres, docker-proxy and friends: hidden unless
  `--all` is passed, and refused by `devps kill` unless `--force` is.

Nothing is inferred from port numbers or traffic; a row's classification
can always be explained by pointing at its argv.

## Testing strategy

Every layer takes an explicit proc root, so the test suite fabricates
complete proc trees (socket tables, stat files, fd symlinks, git repos) in
temp directories and runs the real pipeline against them — deterministic,
offline, no root required. `scripts/smoke.sh` additionally starts a real
loopback listener inside a real repository and asserts devps finds and
kills it via the live `/proc`.
