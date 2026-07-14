# Contributing to devps

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22 on Linux (devps reads the Linux proc filesystem);
nothing else — no runtime dependencies, and git itself is optional.

```bash
git clone https://github.com/JaydenCJ/devps && cd devps
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, checks it against a fabricated proc
tree with known answers, then starts a real loopback listener inside a
real repository and asserts devps lists and kills it via the live `/proc`;
it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (90 deterministic tests, no network, no root).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules — parsers and the join never touch the live system, only the
   proc root they are handed.

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in the
  PR. devps executes no external commands — not lsof, not ss, not git.
- No network calls, ever, and no telemetry; the only thing devps reads is
  the proc filesystem and `.git` metadata files.
- Classification rules are data: new dev servers or infra daemons go into
  the tables in `internal/classify/classify.go` with a test reproducing
  the real argv shape, and a row in the README reference table.
- `devps kill` safety comes first: anything that widens what can be
  signalled without `--force` needs a very good reason.
- Code comments and doc comments are written in English.
- Determinism first: identical proc trees must produce byte-identical
  output, including all orderings.

## Reporting bugs

Include the output of `devps version`, the full command you ran, and —
for join or classification bugs — the relevant `/proc/<pid>/cmdline`
(NUL-separated), `/proc/<pid>/stat` line, and the socket row from
`/proc/net/tcp{,6}`, since that is exactly what devps sees. For git
context bugs, the `.git/HEAD` content (or `.git` pointer file) of the
affected project.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
