// Tests for the join: sockets × processes × git context. Every case runs
// against a fabricated proc tree and fabricated repositories, with a fixed
// clock, so results are byte-stable across machines.
package scan

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/JaydenCJ/devps/internal/procfstest"
)

// fixedNow is one hour after the fixture boot time.
var fixedNow = time.Unix(1_700_003_600, 0)

func numericUser(uid int) string { return "u" + strconv.Itoa(uid) }

func options(root string) Options {
	return Options{ProcRoot: root, Now: fixedNow, LookupUser: numericUser}
}

func TestJoinsSocketToProcessProjectAndBranch(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "shop-frontend")
	procfstest.GitRepo(t, proj, "feature/checkout", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 5173, UID: 1000, Inode: 500}},
		Procs: []procfstest.Proc{{
			PID: 42, Comm: "node", UID: 1000, StartTicks: 100,
			Cmdline: []string{"node", proj + "/node_modules/.bin/vite"},
			CWD:     proj, Exe: "/usr/bin/node", Sockets: []uint64{500},
		}},
	}.Write(t, dir)

	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 1 {
		t.Fatalf("got %d listeners, want 1", len(res.Listeners))
	}
	l := res.Listeners[0]
	if l.Port != 5173 || l.PID != 42 || l.Command != "vite" {
		t.Fatalf("row = %+v, want vite pid 42 on 5173", l)
	}
	if l.Project != "shop-frontend" || l.Branch != "feature/checkout" || l.GitRoot != proj {
		t.Fatalf("git join = %+v, want shop-frontend @ feature/checkout", l)
	}
	if l.User != "u1000" {
		t.Fatalf("user = %q, want u1000 via injected lookup", l.User)
	}
}

func TestAgeComesFromBootTimePlusStartTicks(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "main", false)
	// Started 1000 ticks (10 s) after boot; now is 3600 s after boot.
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 3000, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 5, Comm: "node", StartTicks: 1000,
			Cmdline: []string{"node", "server.js"}, CWD: proj, Sockets: []uint64{1},
		}},
	}.Write(t, dir)
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.Listeners[0].Age, 3590*time.Second; got != want {
		t.Fatalf("age = %v, want %v", got, want)
	}
}

func TestDualStackListenerCollapsesToOneRow(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "0.0.0.0", Port: 8080, Inode: 1}},
		TCP6:  []procfstest.Sock{{Addr: "::", Port: 8080, Inode: 2}},
		Procs: []procfstest.Proc{{
			PID: 5, Comm: "node", StartTicks: 10,
			Cmdline: []string{"node", "srv.js"}, CWD: proj, Sockets: []uint64{1, 2},
		}},
	}.Write(t, dir)
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 1 {
		t.Fatalf("got %d rows, want 1 — v4+v6 on the same pid/port is one server", len(res.Listeners))
	}
	addrs := res.Listeners[0].Addresses
	if len(addrs) != 2 || addrs[0] != "0.0.0.0" || addrs[1] != "::" {
		t.Fatalf("addresses = %v, want [0.0.0.0 ::] with v4 first", addrs)
	}
}

func TestInfraHiddenByDefaultShownWithAll(t *testing.T) {
	dir := t.TempDir()
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "0.0.0.0", Port: 22, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 9, Comm: "sshd", StartTicks: 10,
			Cmdline: []string{"/usr/sbin/sshd", "-D"}, CWD: "/", Sockets: []uint64{1},
		}},
	}.Write(t, dir)

	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 0 || res.Hidden != 1 {
		t.Fatalf("default view: %d rows / %d hidden, want 0 / 1", len(res.Listeners), res.Hidden)
	}

	opts := options(root)
	opts.All = true
	res, err = Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 1 || res.Hidden != 0 {
		t.Fatalf("--all view: %d rows / %d hidden, want 1 / 0", len(res.Listeners), res.Hidden)
	}
}

func TestUnknownProcessVisibilityDependsOnGitRepo(t *testing.T) {
	// The differentiator: an unrecognized binary running from a repo is
	// still "a dev server you left running" and must surface by default.
	dir := t.TempDir()
	proj := filepath.Join(dir, "side-project")
	procfstest.GitRepo(t, proj, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 9999, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 7, Comm: "myserver", StartTicks: 10,
			Cmdline: []string{"./myserver"}, CWD: proj, Sockets: []uint64{1},
		}},
	}.Write(t, dir)
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 1 || res.Listeners[0].Project != "side-project" {
		t.Fatalf("got %+v, want the in-repo unknown listener visible", res.Listeners)
	}

	// The same unknown binary outside any repository is noise and hides.
	dir2 := t.TempDir()
	root2 := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 9999, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 7, Comm: "mystery", StartTicks: 10,
			Cmdline: []string{"/opt/mystery"}, CWD: "/opt", Sockets: []uint64{1},
		}},
	}.Write(t, dir2)
	res, err = Scan(options(root2))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 0 || res.Hidden != 1 {
		t.Fatalf("%d rows / %d hidden, want 0 / 1", len(res.Listeners), res.Hidden)
	}
}

func TestPortFilterKeepsOnlyRequestedPorts(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "main", false)
	mk := func(pid int, port int, ino uint64) procfstest.Proc {
		return procfstest.Proc{
			PID: pid, Comm: "node", StartTicks: 10,
			Cmdline: []string{"node", "s.js"}, CWD: proj, Sockets: []uint64{ino},
		}
	}
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP: []procfstest.Sock{
			{Addr: "127.0.0.1", Port: 3000, Inode: 1},
			{Addr: "127.0.0.1", Port: 4000, Inode: 2},
		},
		Procs: []procfstest.Proc{mk(5, 3000, 1), mk(6, 4000, 2)},
	}.Write(t, dir)
	opts := options(root)
	opts.Ports = []int{4000}
	res, err := Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 1 || res.Listeners[0].Port != 4000 {
		t.Fatalf("got %+v, want only port 4000", res.Listeners)
	}
	if res.Hidden != 0 {
		t.Fatalf("hidden = %d, want 0 — port-filtered rows are not 'hidden'", res.Hidden)
	}
}

func TestUnownedPortsAreCountedNotInvented(t *testing.T) {
	// A socket whose inode maps to no visible process (another user's,
	// scanned without root) must be surfaced as a count, never a fake row.
	dir := t.TempDir()
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 6000, Inode: 404}},
		Procs: []procfstest.Proc{},
	}.Write(t, dir)
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 0 || res.Unowned != 1 {
		t.Fatalf("%d rows / %d unowned, want 0 / 1", len(res.Listeners), res.Unowned)
	}
}

func TestRowsSortByPortThenPID(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP: []procfstest.Sock{
			{Addr: "127.0.0.1", Port: 9000, Inode: 1},
			{Addr: "127.0.0.1", Port: 3000, Inode: 2},
		},
		Procs: []procfstest.Proc{
			{PID: 50, Comm: "node", StartTicks: 10, Cmdline: []string{"node", "a.js"}, CWD: proj, Sockets: []uint64{1}},
			{PID: 51, Comm: "node", StartTicks: 10, Cmdline: []string{"node", "b.js"}, CWD: proj, Sockets: []uint64{2}},
		},
	}.Write(t, dir)
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listeners) != 2 || res.Listeners[0].Port != 3000 || res.Listeners[1].Port != 9000 {
		t.Fatalf("order = %+v, want ports ascending", res.Listeners)
	}
}

func TestNoGitSkipsRepositoryLookup(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "repo-proj")
	procfstest.GitRepo(t, proj, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 5173, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 5, Comm: "node", StartTicks: 10,
			Cmdline: []string{"node", proj + "/node_modules/.bin/vite"},
			CWD:     proj, Sockets: []uint64{1},
		}},
	}.Write(t, dir)
	opts := options(root)
	opts.NoGit = true
	res, err := Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	l := res.Listeners[0]
	if l.Branch != "" || l.GitRoot != "" {
		t.Fatalf("git fields = %+v, want empty under --no-git", l)
	}
	if l.Project != "repo-proj" {
		t.Fatalf("project = %q, want cwd basename fallback", l.Project)
	}
}

func TestKernelThreadListenerFallsBackToCommArgv(t *testing.T) {
	dir := t.TempDir()
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "0.0.0.0", Port: 2049, UID: 0, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 3, Comm: "nfsd", StartTicks: 10, Sockets: []uint64{1},
		}},
	}.Write(t, dir)
	opts := options(root)
	opts.All = true
	res, err := Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Listeners[0].Argv != "[nfsd]" {
		t.Fatalf("argv = %q, want bracketed comm for empty cmdline", res.Listeners[0].Argv)
	}
}

func TestDetachedHeadPropagates(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "9fceb02d0ae598e95dc970b74767f19372d61af8", true)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 3000, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 5, Comm: "node", StartTicks: 10,
			Cmdline: []string{"node", "s.js"}, CWD: proj, Sockets: []uint64{1},
		}},
	}.Write(t, dir)
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	l := res.Listeners[0]
	if !l.Detached || l.Branch != "9fceb02" {
		t.Fatalf("got %+v, want detached at 9fceb02", l)
	}
}

func TestUIDFallsBackToSocketOwnerWhenStatusUnreadable(t *testing.T) {
	// procfstest always writes status, so point the check at the code
	// path: UID -1 from the process must be replaced by the socket's uid.
	dir := t.TempDir()
	proj := filepath.Join(dir, "p")
	procfstest.GitRepo(t, proj, "main", false)
	root := procfstest.Tree{
		Btime: 1_700_000_000,
		TCP:   []procfstest.Sock{{Addr: "127.0.0.1", Port: 3000, UID: 1234, Inode: 1}},
		Procs: []procfstest.Proc{{
			PID: 5, Comm: "node", StartTicks: 10,
			Cmdline: []string{"node", "s.js"}, CWD: proj, Sockets: []uint64{1},
		}},
	}.Write(t, dir)
	// Remove status so UID comes back -1 from the snapshot.
	if err := removeStatus(root, 5); err != nil {
		t.Fatal(err)
	}
	res, err := Scan(options(root))
	if err != nil {
		t.Fatal(err)
	}
	if res.Listeners[0].UID != 1234 {
		t.Fatalf("uid = %d, want 1234 from the socket table", res.Listeners[0].UID)
	}
}

// removeStatus deletes a fabricated status file to simulate a process
// whose status is unreadable.
func removeStatus(root string, pid int) error {
	return os.Remove(filepath.Join(root, strconv.Itoa(pid), "status"))
}
