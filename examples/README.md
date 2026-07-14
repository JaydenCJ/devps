# devps examples

Two runnable scripts that need nothing but bash and a built `devps` binary.

## make-demo-proc.sh — a safe playground

Fabricates a fake proc tree (four dev servers with git context, plus
postgres/sshd noise) so every flag can be tried without leaving real
servers running:

```bash
bash examples/make-demo-proc.sh /tmp/devps-demo
devps list --proc-root /tmp/devps-demo/proc
devps list --all --format json --proc-root /tmp/devps-demo/proc
devps kill --dry-run --proc-root /tmp/devps-demo/proc 5173
```

The same fabrication technique powers the test suite and
`scripts/smoke.sh`; `--proc-root` is a first-class flag, also useful for
inspecting a container's `/proc` bind-mounted on the host.

## free-port.sh — pre-flight check for your own dev scripts

Answers "is port N free, and if not, who has it?" with devps context
instead of a bare `EADDRINUSE` stack trace:

```bash
bash examples/free-port.sh 3000 && npm run dev
```

If the port is taken, it prints the owning project/branch/age row and
suggests the exact `devps kill 3000` to run. Wire it into a `predev`
script so "port already in use" stops being a debugging session.
