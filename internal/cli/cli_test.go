// In-process CLI integration tests: Run() is exercised end-to-end against
// fabricated proc trees, asserting on real stdout/stderr and exit codes.
// The signal sender is stubbed, so kill semantics are tested without ever
// signalling a real process.
package cli

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/JaydenCJ/devps/internal/procfstest"
	"github.com/JaydenCJ/devps/internal/version"
)

// run executes the CLI in-process and returns (stdout, stderr, exit code).
func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := Run(args, &out, &errBuf)
	return out.String(), errBuf.String(), code
}

// devTree fabricates a proc root with one vite dev server on 5173 (in a
// repo on feature/checkout), one plain node process in a repo on main
// (port 3000), and one sshd on 22. Returns (procRoot, projectDir).
func devTree(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	shop := filepath.Join(dir, "shop-frontend")
	api := filepath.Join(dir, "billing-api")
	procfstest.GitRepo(t, shop, "feature/checkout", false)
	procfstest.GitRepo(t, api, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP: []procfstest.Sock{
			{Addr: "127.0.0.1", Port: 5173, UID: 1000, Inode: 1},
			{Addr: "127.0.0.1", Port: 3000, UID: 1000, Inode: 2},
			{Addr: "0.0.0.0", Port: 22, UID: 0, Inode: 3},
		},
		Procs: []procfstest.Proc{
			{
				PID: 4102, Comm: "node", UID: 1000, StartTicks: 100,
				Cmdline: []string{"node", shop + "/node_modules/.bin/vite"},
				CWD:     shop, Exe: "/usr/bin/node", Sockets: []uint64{1},
			},
			{
				PID: 4200, Comm: "node", UID: 1000, StartTicks: 200,
				Cmdline: []string{"node", "server.js"},
				CWD:     api, Exe: "/usr/bin/node", Sockets: []uint64{2},
			},
			{
				PID: 811, Comm: "sshd", UID: 0, StartTicks: 50,
				Cmdline: []string{"/usr/sbin/sshd", "-D"}, CWD: "/", Sockets: []uint64{3},
			},
		},
	}.Write(t, dir)
	return root, shop
}

func TestVersionAndHelp(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		out, _, code := run(t, arg)
		if code != ExitOK || out != "devps "+version.Version+"\n" {
			t.Fatalf("%s: out=%q code=%d", arg, out, code)
		}
	}
	out, _, code := run(t, "--help")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"Usage:", "devps kill", "--proc-root", "Exit codes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage missing %q:\n%s", want, out)
		}
	}
}

func TestListShowsDevServersWithProjectAndBranch(t *testing.T) {
	root, _ := devTree(t)
	out, _, code := run(t, "list", "--proc-root", root)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"5173", "vite", "shop-frontend", "feature/checkout", "3000", "billing-api", "main"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sshd") {
		t.Fatalf("sshd must be hidden by default:\n%s", out)
	}
	if !strings.Contains(out, "1 other listener hidden") {
		t.Fatalf("hidden note missing:\n%s", out)
	}
}

func TestListAllIncludesInfra(t *testing.T) {
	root, _ := devTree(t)
	out, _, code := run(t, "list", "--all", "--proc-root", root)
	if code != ExitOK || !strings.Contains(out, "sshd") {
		t.Fatalf("--all must show sshd (exit %d):\n%s", code, out)
	}
}

func TestBareArgsAreTreatedAsListPorts(t *testing.T) {
	root, _ := devTree(t)
	// `devps 5173` — no explicit subcommand, flags-then-port also valid.
	out, _, code := run(t, "--proc-root", root, "5173")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "5173") || strings.Contains(out, "3000") {
		t.Fatalf("port filter not applied:\n%s", out)
	}
}

func TestListJSONFormat(t *testing.T) {
	root, _ := devTree(t)
	out, _, code := run(t, "list", "--format", "json", "--proc-root", root)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{`"tool": "devps"`, `"schema_version": 1`, `"branch": "feature/checkout"`, `"port": 5173`, `"hidden": 1`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json missing %s:\n%s", want, out)
		}
	}
}

func TestListWideShowsFullDirectory(t *testing.T) {
	root, shop := devTree(t)
	out, _, code := run(t, "list", "--wide", "--proc-root", root)
	if code != ExitOK || !strings.Contains(out, shop) {
		t.Fatalf("--wide must print the full project dir (exit %d):\n%s", code, out)
	}
}

func TestListNoGitOmitsBranches(t *testing.T) {
	root, _ := devTree(t)
	out, _, code := run(t, "list", "--all", "--no-git", "--proc-root", root)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "feature/checkout") {
		t.Fatalf("--no-git must skip branch resolution:\n%s", out)
	}
}

func TestListUsageErrors(t *testing.T) {
	_, errOut, code := run(t, "list", "abc")
	if code != ExitUsage || !strings.Contains(errOut, "not a port") {
		t.Fatalf("exit=%d err=%q, want usage error", code, errOut)
	}
	_, _, code = run(t, "list", "70000")
	if code != ExitUsage {
		t.Fatalf("out-of-range port: exit=%d, want %d", code, ExitUsage)
	}
	_, errOut, code = run(t, "list", "--format", "yaml")
	if code != ExitUsage || !strings.Contains(errOut, "yaml") {
		t.Fatalf("unknown format: exit=%d err=%q", code, errOut)
	}
}

func TestBadProcRootIsRuntimeError(t *testing.T) {
	_, errOut, code := run(t, "list", "--proc-root", t.TempDir())
	if code != ExitRuntime || errOut == "" {
		t.Fatalf("exit=%d err=%q, want runtime error", code, errOut)
	}
}

// withStubSignal swaps the signal sender for the duration of a test and
// records every (pid, sig) delivery.
func withStubSignal(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	orig := sendSignal
	sendSignal = func(pid int, sig syscall.Signal) error {
		calls = append(calls, fmt.Sprintf("%d:%d", pid, int(sig)))
		return err
	}
	t.Cleanup(func() { sendSignal = orig })
	return &calls
}

func TestKillSendsSIGTERMToDevServer(t *testing.T) {
	root, _ := devTree(t)
	calls := withStubSignal(t, nil)
	out, _, code := run(t, "kill", "--proc-root", root, "5173")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(*calls) != 1 || (*calls)[0] != fmt.Sprintf("4102:%d", int(syscall.SIGTERM)) {
		t.Fatalf("signals sent = %v, want exactly SIGTERM to 4102", *calls)
	}
	if !strings.Contains(out, "sent SIGTERM to pid 4102") || !strings.Contains(out, "shop-frontend @ feature/checkout") {
		t.Fatalf("confirmation must name the project:\n%s", out)
	}
}

func TestKillDryRunSignalsNothing(t *testing.T) {
	root, _ := devTree(t)
	calls := withStubSignal(t, nil)
	out, _, code := run(t, "kill", "--dry-run", "--proc-root", root, "5173")
	if code != ExitOK || len(*calls) != 0 {
		t.Fatalf("dry-run must not signal (exit %d, calls %v)", code, *calls)
	}
	if !strings.Contains(out, "would send SIGTERM to pid 4102") {
		t.Fatalf("dry-run output:\n%s", out)
	}
}

func TestKillRefusesInfraWithoutForce(t *testing.T) {
	root, _ := devTree(t)
	calls := withStubSignal(t, nil)
	_, errOut, code := run(t, "kill", "--proc-root", root, "22")
	if code != ExitNone || len(*calls) != 0 {
		t.Fatalf("must refuse sshd (exit %d, calls %v)", code, *calls)
	}
	if !strings.Contains(errOut, "refusing") || !strings.Contains(errOut, "--force") {
		t.Fatalf("refusal message:\n%s", errOut)
	}
}

func TestKillForceOverridesTheGuard(t *testing.T) {
	root, _ := devTree(t)
	calls := withStubSignal(t, nil)
	_, _, code := run(t, "kill", "--force", "--proc-root", root, "22")
	if code != ExitOK || len(*calls) != 1 || (*calls)[0] != fmt.Sprintf("811:%d", int(syscall.SIGTERM)) {
		t.Fatalf("--force must signal sshd (exit %d, calls %v)", code, *calls)
	}
}

func TestKillCustomSignalSpellings(t *testing.T) {
	root, _ := devTree(t)
	calls := withStubSignal(t, nil)
	for _, spelling := range []string{"KILL", "SIGKILL", "kill", "9"} {
		*calls = nil
		_, _, code := run(t, "kill", "--signal", spelling, "--proc-root", root, "5173")
		if code != ExitOK || len(*calls) != 1 || (*calls)[0] != fmt.Sprintf("4102:%d", int(syscall.SIGKILL)) {
			t.Fatalf("--signal %s: exit %d, calls %v", spelling, code, *calls)
		}
	}
}

func TestKillUsageErrors(t *testing.T) {
	_, errOut, code := run(t, "kill", "--signal", "BOGUS", "3000")
	if code != ExitUsage || !strings.Contains(errOut, "unknown signal") {
		t.Fatalf("bogus signal: exit=%d err=%q", code, errOut)
	}
	_, errOut, code = run(t, "kill", "--signal", "99", "3000")
	if code != ExitUsage {
		t.Fatalf("out-of-range numeric signal: exit=%d err=%q", code, errOut)
	}
}

func TestKillWithNoListenerOnPortExitsOne(t *testing.T) {
	root, _ := devTree(t)
	calls := withStubSignal(t, nil)
	_, errOut, code := run(t, "kill", "--proc-root", root, "8123")
	if code != ExitNone || len(*calls) != 0 {
		t.Fatalf("exit=%d calls=%v, want no-match verdict", code, *calls)
	}
	if !strings.Contains(errOut, "no listener on port 8123") {
		t.Fatalf("stderr:\n%s", errOut)
	}
}

func TestKillRequiresAtLeastOnePort(t *testing.T) {
	_, errOut, code := run(t, "kill")
	if code != ExitUsage || !strings.Contains(errOut, "at least one port") {
		t.Fatalf("exit=%d err=%q", code, errOut)
	}
}

func TestKillSignalsAPidOnlyOnceAcrossPorts(t *testing.T) {
	// One process listening on two requested ports gets one signal, not two.
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP: []procfstest.Sock{
			{Addr: "127.0.0.1", Port: 3000, Inode: 1},
			{Addr: "127.0.0.1", Port: 3001, Inode: 2},
		},
		Procs: []procfstest.Proc{{
			PID: 42, Comm: "node", StartTicks: 10,
			Cmdline: []string{"node", "s.js"}, CWD: proj, Sockets: []uint64{1, 2},
		}},
	}.Write(t, dir)
	calls := withStubSignal(t, nil)
	_, _, code := run(t, "kill", "--proc-root", root, "3000", "3001")
	if code != ExitOK || len(*calls) != 1 {
		t.Fatalf("exit=%d calls=%v, want a single signal", code, *calls)
	}
}

func TestKillReportsSendFailure(t *testing.T) {
	root, _ := devTree(t)
	withStubSignal(t, syscall.EPERM)
	_, errOut, code := run(t, "kill", "--proc-root", root, "5173")
	if code != ExitNone || !strings.Contains(errOut, "operation not permitted") {
		t.Fatalf("exit=%d err=%q, want EPERM surfaced", code, errOut)
	}
}
