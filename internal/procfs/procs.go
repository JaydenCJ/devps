package procfs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// userHZ is the tick unit of the starttime field in /proc/<pid>/stat.
// The proc interface fixes it at 100 regardless of the kernel's HZ.
const userHZ = 100

// Proc is a snapshot of one process, read from /proc/<pid>.
type Proc struct {
	PID        int
	Comm       string   // process name from stat, up to 15 chars
	Cmdline    []string // argv; nil for kernel threads
	CWD        string   // resolved working directory; "" if unreadable
	Exe        string   // resolved executable path; "" if unreadable
	UID        int      // real uid from status; -1 if unreadable
	StartTicks uint64   // starttime: clock ticks between boot and exec
}

// StartedAt converts the process start ticks to wall-clock time given the
// boot time from BootTime.
func (p *Proc) StartedAt(bootUnix int64) time.Time {
	sec := bootUnix + int64(p.StartTicks/userHZ)
	nsec := int64(p.StartTicks%userHZ) * (int64(time.Second) / userHZ)
	return time.Unix(sec, nsec)
}

// Table is a full process snapshot plus the socket-inode → pid index that
// lets a listening socket be joined back to its owner.
type Table struct {
	Procs     map[int]*Proc
	SocketPID map[uint64]int
}

// Snapshot walks every numeric directory under the proc root. Processes
// that vanish mid-walk or whose fd table is unreadable (other users,
// without root) are skipped or partially read — never fatal.
func Snapshot(root string) (*Table, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	t := &Table{Procs: map[int]*Proc{}, SocketPID: map[uint64]int{}}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		p, inodes, err := readProc(root, pid)
		if err != nil {
			continue // raced with process exit
		}
		t.Procs[pid] = p
		for _, ino := range inodes {
			t.SocketPID[ino] = pid
		}
	}
	return t, nil
}

// readProc reads one process directory. Only stat is essential; cwd, exe,
// status, and fd degrade to empty values when unreadable.
func readProc(root string, pid int) (*Proc, []uint64, error) {
	dir := filepath.Join(root, strconv.Itoa(pid))
	statRaw, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return nil, nil, err
	}
	comm, startTicks, err := parseStat(string(statRaw))
	if err != nil {
		return nil, nil, err
	}
	p := &Proc{PID: pid, Comm: comm, StartTicks: startTicks, UID: -1}
	if raw, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		p.Cmdline = splitCmdline(raw)
	}
	if target, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
		p.CWD = target
	}
	if target, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
		p.Exe = target
	}
	if uid, err := readUID(filepath.Join(dir, "status")); err == nil {
		p.UID = uid
	}
	return p, socketInodes(filepath.Join(dir, "fd")), nil
}

// parseStat extracts comm and starttime from a stat line. comm is wrapped
// in parentheses and may itself contain spaces and parentheses, so the
// fields after it are located from the LAST ')' in the line.
func parseStat(data string) (string, uint64, error) {
	open := strings.IndexByte(data, '(')
	close := strings.LastIndexByte(data, ')')
	if open < 0 || close < open {
		return "", 0, fmt.Errorf("malformed stat line")
	}
	comm := data[open+1 : close]
	fields := strings.Fields(data[close+1:])
	// fields[0] is the state (field 3 overall); starttime is field 22
	// overall, hence fields[19].
	if len(fields) < 20 {
		return "", 0, fmt.Errorf("stat line has %d fields after comm, want >= 20", len(fields))
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("bad starttime: %w", err)
	}
	return comm, ticks, nil
}

// splitCmdline splits the NUL-separated argv. Kernel threads have an empty
// cmdline and yield nil.
func splitCmdline(raw []byte) []string {
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

// readUID pulls the real uid (first value on the Uid: line) from status.
func readUID(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return -1, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "Uid:"); ok {
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				break
			}
			return strconv.Atoi(fields[0])
		}
	}
	return -1, fmt.Errorf("no Uid line in %s", path)
}

// socketInodes lists the socket inodes held open by one process. fd
// entries are symlinks shaped "socket:[12345]"; anything else is ignored.
func socketInodes(fdDir string) []uint64 {
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil // permission denied for other users' processes
	}
	var out []uint64
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue
		}
		rest, ok := strings.CutPrefix(target, "socket:[")
		if !ok || !strings.HasSuffix(rest, "]") {
			continue
		}
		ino, err := strconv.ParseUint(strings.TrimSuffix(rest, "]"), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, ino)
	}
	return out
}

// BootTime reads the epoch boot time (btime) from the top-level stat file;
// starttime ticks are relative to it.
func BootTime(root string) (int64, error) {
	f, err := os.Open(filepath.Join(root, "stat"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "btime "); ok {
			return strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		}
	}
	return 0, fmt.Errorf("no btime in %s/stat", root)
}
