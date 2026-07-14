# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- Listening-socket discovery from `/proc/net/tcp` and `/proc/net/tcp6`:
  LISTEN-state filtering, little-endian word decoding for IPv4 and IPv6
  addresses, v4-mapped address unmapping, and tolerance for malformed rows
  and IPv6-less kernels.
- Socket → process join via the `/proc/<pid>/fd` inode index, with a
  stat parser that survives hostile comm values, cmdline/cwd/exe reads
  that degrade gracefully, and an explicit "unowned ports" count for
  listeners whose owner is unreadable without root.
- Git context without executing git: upward `.git` walk from each
  process's working directory, `HEAD` decoding for branches, detached
  heads (`detached@<hash>`), and linked worktrees / submodules via
  `gitdir:` pointer files.
- Rule-based classification of listeners into dev / other / infra, with a
  curated table covering the node, python, ruby, php, elixir, go, rust,
  and .NET dev-server ecosystems, including `go run` build-cache binaries
  and `npm run dev`-style script runners.
- Default view showing dev servers plus any listener running inside a git
  repository; `--all` reveals infrastructure daemons, with honest hidden
  and unowned counts in the footer.
- `list` subcommand with aligned tables, `--wide` (user, full directory,
  argv), positional port filters, `--no-git`, and stable JSON
  (`schema_version: 1`) via `--format json`.
- `kill` subcommand: port-addressed signalling with a safety guard that
  refuses infra and non-project listeners without `--force`, `--signal`
  name/number spellings, `--dry-run`, and one signal per pid across ports.
- `--proc-root` on every subcommand for fabricated trees and container
  proc mounts.
- Runnable examples (`examples/make-demo-proc.sh`,
  `examples/free-port.sh`) and an internals walkthrough
  (`docs/how-it-works.md`).
- 90 deterministic offline tests (fabricated proc trees, in-process CLI
  integration, stubbed signalling) and `scripts/smoke.sh`, which also
  kills a real loopback listener through the live `/proc`.

[0.1.0]: https://github.com/JaydenCJ/devps/releases/tag/v0.1.0
