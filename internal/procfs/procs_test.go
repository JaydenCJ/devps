// Tests for the process snapshot: pid directory discovery, stat parsing
// (including hostile comm values), cmdline decoding, fd → socket-inode
// mapping, and boot-time arithmetic.
package procfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JaydenCJ/devps/internal/procfstest"
)

func fakeTree(t *testing.T, procs ...procfstest.Proc) string {
	t.Helper()
	tree := procfstest.Tree{Btime: 1_700_000_000, Procs: procs}
	return tree.Write(t, t.TempDir())
}

func TestSnapshotFindsOnlyNumericPIDDirectories(t *testing.T) {
	root := fakeTree(t, procfstest.Proc{PID: 42, Comm: "node", StartTicks: 100})
	// Non-pid entries the walker must ignore, like the real /proc has.
	for _, name := range []string{"self", "sys", "irq"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Procs) != 1 || table.Procs[42] == nil {
		t.Fatalf("procs = %v, want exactly pid 42", table.Procs)
	}
	if got := table.Procs[42].Comm; got != "node" {
		t.Fatalf("comm = %q, want node from stat", got)
	}
}

func TestCmdlineSplitsOnNULAndDropsTrailingNUL(t *testing.T) {
	root := fakeTree(t, procfstest.Proc{
		PID: 8, Comm: "node", StartTicks: 5,
		Cmdline: []string{"node", "server.js", "--port", "3000"},
	})
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	got := table.Procs[8].Cmdline
	want := []string{"node", "server.js", "--port", "3000"}
	if len(got) != len(want) {
		t.Fatalf("cmdline = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cmdline[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEmptyCmdlineMeansKernelThread(t *testing.T) {
	root := fakeTree(t, procfstest.Proc{PID: 9, Comm: "kworker/0:1", StartTicks: 5})
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := table.Procs[9].Cmdline; got != nil {
		t.Fatalf("cmdline = %q, want nil for an empty cmdline file", got)
	}
}

func TestCWDSymlinkReadAndMissingDegrades(t *testing.T) {
	root := fakeTree(t,
		procfstest.Proc{PID: 10, Comm: "vite", StartTicks: 5, CWD: "/srv/projects/shop"},
		procfstest.Proc{PID: 11, Comm: "x", StartTicks: 5}, // no cwd link
	)
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := table.Procs[10].CWD; got != "/srv/projects/shop" {
		t.Fatalf("cwd = %q, want /srv/projects/shop", got)
	}
	if got := table.Procs[11].CWD; got != "" {
		t.Fatalf("cwd = %q, want empty when the link is unreadable", got)
	}
}

func TestSocketInodesMapToOwningPID(t *testing.T) {
	root := fakeTree(t,
		procfstest.Proc{PID: 20, Comm: "a", StartTicks: 5, Sockets: []uint64{111, 222}},
		procfstest.Proc{PID: 21, Comm: "b", StartTicks: 5, Sockets: []uint64{333}},
	)
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	for ino, wantPID := range map[uint64]int{111: 20, 222: 20, 333: 21} {
		if got := table.SocketPID[ino]; got != wantPID {
			t.Errorf("inode %d → pid %d, want %d", ino, got, wantPID)
		}
	}
}

func TestNonSocketFDLinksAreIgnored(t *testing.T) {
	root := fakeTree(t, procfstest.Proc{PID: 22, Comm: "a", StartTicks: 5})
	// fd/0 is a /dev/null link (created by the fixture); add a pipe too.
	if err := os.Symlink("pipe:[999]", filepath.Join(root, "22", "fd", "5")); err != nil {
		t.Fatal(err)
	}
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.SocketPID) != 0 {
		t.Fatalf("SocketPID = %v, want empty (no socket fds)", table.SocketPID)
	}
}

func TestUnreadableFDTableIsNotFatal(t *testing.T) {
	// Simulate a process whose fd table cannot be listed (another user's
	// process without root): replace fd/ with a plain file so ReadDir fails.
	root := fakeTree(t, procfstest.Proc{PID: 23, Comm: "postgres", StartTicks: 5})
	fd := filepath.Join(root, "23", "fd")
	if err := os.RemoveAll(fd); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fd, []byte("denied"), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := Snapshot(root)
	if err != nil {
		t.Fatalf("unreadable fd table must not break the snapshot: %v", err)
	}
	if table.Procs[23] == nil {
		t.Fatal("process must still be present without its fd map")
	}
}

func TestParseStatSurvivesParenthesesAndSpacesInComm(t *testing.T) {
	// A process can rename itself to something like "a) S 99 (b" — the
	// classic /proc/pid/stat parsing trap. Fields must come from the LAST
	// closing parenthesis.
	line := procfstest.EncodeStatLine(50, "a) S 99 (b", 12345)
	comm, ticks, err := parseStat(line)
	if err != nil {
		t.Fatal(err)
	}
	if comm != "a) S 99 (b" {
		t.Fatalf("comm = %q, want the hostile name intact", comm)
	}
	if ticks != 12345 {
		t.Fatalf("starttime = %d, want 12345", ticks)
	}
}

func TestParseStatRejectsTruncatedLines(t *testing.T) {
	if _, _, err := parseStat("99 (short) S 1 2 3"); err == nil {
		t.Fatal("a stat line without a starttime field must be rejected")
	}
	if _, _, err := parseStat("no parens at all"); err == nil {
		t.Fatal("a stat line without a comm must be rejected")
	}
}

func TestUIDReadFromStatusRealValue(t *testing.T) {
	root := fakeTree(t, procfstest.Proc{PID: 30, Comm: "x", UID: 1000, StartTicks: 5})
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := table.Procs[30].UID; got != 1000 {
		t.Fatalf("uid = %d, want 1000", got)
	}
}

func TestBootTimeAndStartedAtArithmetic(t *testing.T) {
	root := fakeTree(t)
	boot, err := BootTime(root)
	if err != nil {
		t.Fatal(err)
	}
	if boot != 1_700_000_000 {
		t.Fatalf("btime = %d, want 1700000000", boot)
	}
	// 250 ticks at USER_HZ=100 is exactly 2.5 s after boot.
	p := Proc{StartTicks: 250}
	got := p.StartedAt(boot)
	want := time.Unix(1_700_000_002, 500_000_000)
	if !got.Equal(want) {
		t.Fatalf("StartedAt = %v, want %v", got, want)
	}
}

func TestVanishedProcessIsSkippedNotFatal(t *testing.T) {
	root := fakeTree(t, procfstest.Proc{PID: 60, Comm: "ok", StartTicks: 5})
	// A pid directory whose stat is already gone: the exit race.
	if err := os.MkdirAll(filepath.Join(root, "61", "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	table, err := Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if table.Procs[61] != nil {
		t.Fatal("half-vanished process must be skipped")
	}
	if table.Procs[60] == nil {
		t.Fatal("healthy process must survive its neighbor vanishing")
	}
}
