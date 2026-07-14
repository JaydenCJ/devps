// Package procfstest fabricates deterministic proc trees on disk so unit
// and CLI tests can exercise the full scan pipeline offline, without root
// and without depending on whatever the host machine happens to run.
//
// The encoders here mirror the kernel's formatting rules independently of
// the parsers in internal/procfs; the parser tests additionally assert
// against captured real-world lines so the two cannot drift together.
package procfstest

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sock is one socket-table row to fabricate.
type Sock struct {
	Addr  string // "127.0.0.1", "0.0.0.0", "::", "::1", …
	Port  int
	UID   int
	Inode uint64
	State string // hex state; "" means LISTEN ("0A")
}

// Proc is one process directory to fabricate.
type Proc struct {
	PID        int
	Comm       string
	Cmdline    []string
	CWD        string // symlink target; "" omits the link
	Exe        string // symlink target; "" omits the link
	UID        int
	StartTicks uint64
	Sockets    []uint64 // socket inodes exposed under fd/
}

// Tree is a complete fake proc filesystem.
type Tree struct {
	Btime int64
	TCP   []Sock // rows for net/tcp (IPv4)
	TCP6  []Sock // rows for net/tcp6
	Procs []Proc
}

// Write materializes the tree under dir/proc and returns that root.
func (tr Tree) Write(t testing.TB, dir string) string {
	t.Helper()
	root := filepath.Join(dir, "proc")
	mustMkdir(t, filepath.Join(root, "net"))

	stat := fmt.Sprintf("cpu  0 0 0 0 0 0 0 0 0 0\nbtime %d\nprocesses 1\n", tr.Btime)
	mustWrite(t, filepath.Join(root, "stat"), stat)
	mustWrite(t, filepath.Join(root, "net", "tcp"), encodeTable(t, tr.TCP, false))
	mustWrite(t, filepath.Join(root, "net", "tcp6"), encodeTable(t, tr.TCP6, true))

	for _, p := range tr.Procs {
		pdir := filepath.Join(root, fmt.Sprintf("%d", p.PID))
		mustMkdir(t, filepath.Join(pdir, "fd"))
		mustWrite(t, filepath.Join(pdir, "comm"), p.Comm+"\n")
		mustWrite(t, filepath.Join(pdir, "cmdline"), encodeCmdline(p.Cmdline))
		mustWrite(t, filepath.Join(pdir, "stat"), encodeStat(p))
		mustWrite(t, filepath.Join(pdir, "status"),
			fmt.Sprintf("Name:\t%s\nUid:\t%d\t%d\t%d\t%d\n", p.Comm, p.UID, p.UID, p.UID, p.UID))
		if p.CWD != "" {
			mustSymlink(t, p.CWD, filepath.Join(pdir, "cwd"))
		}
		if p.Exe != "" {
			mustSymlink(t, p.Exe, filepath.Join(pdir, "exe"))
		}
		mustSymlink(t, "/dev/null", filepath.Join(pdir, "fd", "0"))
		for i, ino := range p.Sockets {
			mustSymlink(t, fmt.Sprintf("socket:[%d]", ino),
				filepath.Join(pdir, "fd", fmt.Sprintf("%d", i+3)))
		}
	}
	return root
}

// EncodeStatLine is the stat encoder, exported so parser tests can feed it
// hostile comm values ("a) b (c") and prove the parser survives them.
func EncodeStatLine(pid int, comm string, startTicks uint64) string {
	return encodeStat(Proc{PID: pid, Comm: comm, StartTicks: startTicks})
}

func encodeStat(p Proc) string {
	// pid (comm) state + 18 filler fields + starttime (field 22) + tail.
	return fmt.Sprintf("%d (%s) S 1 1 1 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 %d 1024 100 18446744073709551615\n",
		p.PID, p.Comm, p.StartTicks)
}

func encodeCmdline(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return strings.Join(argv, "\x00") + "\x00"
}

// EncodeSockLine formats one kernel socket-table row, exported so parser
// tests can round-trip arbitrary addresses through it.
func EncodeSockLine(sl int, s Sock) string {
	state := s.State
	if state == "" {
		state = "0A"
	}
	return fmt.Sprintf("%4d: %s:%04X 00000000:0000 %s 00000000:00000000 00:00000000 00000000 %5d        0 %d 1 0000000000000000 100 0 0 10 0",
		sl, hexAddr(s.Addr), s.Port, state, s.UID, s.Inode)
}

func encodeTable(t testing.TB, socks []Sock, v6 bool) string {
	t.Helper()
	var b strings.Builder
	if v6 {
		b.WriteString("  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")
	} else {
		b.WriteString("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")
	}
	for i, s := range socks {
		addr, err := netip.ParseAddr(s.Addr)
		if err != nil {
			t.Fatalf("procfstest: bad addr %q: %v", s.Addr, err)
		}
		if addr.Is4() == v6 {
			t.Fatalf("procfstest: %q is in the wrong table (v6=%v)", s.Addr, v6)
		}
		b.WriteString(EncodeSockLine(i, s))
		b.WriteString("\n")
	}
	return b.String()
}

// hexAddr encodes an address the way the kernel prints it: a sequence of
// 32-bit words, each in host (little-endian) byte order.
func hexAddr(addr string) string {
	a := netip.MustParseAddr(addr)
	var raw []byte
	if a.Is4() {
		b := a.As4()
		raw = b[:]
	} else {
		b := a.As16()
		raw = b[:]
	}
	var out strings.Builder
	for i := 0; i < len(raw); i += 4 {
		fmt.Fprintf(&out, "%02X%02X%02X%02X", raw[i+3], raw[i+2], raw[i+1], raw[i])
	}
	return out.String()
}

// GitRepo fabricates a minimal repository at dir: a .git directory whose
// HEAD points at branch (or at a raw hash when detached is true).
func GitRepo(t testing.TB, dir, branch string, detached bool) {
	t.Helper()
	mustMkdir(t, filepath.Join(dir, ".git"))
	head := "ref: refs/heads/" + branch + "\n"
	if detached {
		head = branch + "\n"
	}
	mustWrite(t, filepath.Join(dir, ".git", "HEAD"), head)
}

func mustMkdir(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("procfstest: mkdir %s: %v", dir, err)
	}
}

func mustWrite(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("procfstest: write %s: %v", path, err)
	}
}

func mustSymlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("procfstest: symlink %s: %v", link, err)
	}
}
