// Package scan joins the three views procfs offers — listening sockets,
// processes, and their working directories — into one answer per port:
// who is listening, from which project, on which branch, for how long.
package scan

import (
	"os/user"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JaydenCJ/devps/internal/classify"
	"github.com/JaydenCJ/devps/internal/gitinfo"
	"github.com/JaydenCJ/devps/internal/procfs"
)

// Listener is one process × port row, after joining and enrichment.
type Listener struct {
	Port      int
	Addresses []string // distinct listen addresses, IPv4 first
	PID       int
	UID       int
	User      string
	Command   string // short classified label, e.g. "vite"
	Argv      string // full command line
	Kind      classify.Kind
	Dir       string // working directory
	Project   string // basename of the git root (or of Dir)
	GitRoot   string
	Branch    string
	Detached  bool
	StartedAt time.Time
	Age       time.Duration
}

// Options configures a scan.
type Options struct {
	ProcRoot string // proc filesystem root, normally /proc
	All      bool   // include infra and non-project listeners
	Ports    []int  // restrict to these ports; empty = all
	NoGit    bool   // skip repository lookup
	Now      time.Time
	// LookupUser resolves a uid to a display name; nil uses os/user.
	// Injectable so tests are independent of the host's passwd database.
	LookupUser func(uid int) string
}

// Result is what a scan found.
type Result struct {
	Listeners []Listener // visible rows, sorted by port then pid
	Hidden    int        // rows filtered out by the default view
	Unowned   int        // listening ports whose owner could not be mapped
}

// Scan reads the proc tree once and returns the joined listener table.
func Scan(opts Options) (Result, error) {
	sockets, err := procfs.ListenTCP(opts.ProcRoot)
	if err != nil {
		return Result{}, err
	}
	table, err := procfs.Snapshot(opts.ProcRoot)
	if err != nil {
		return Result{}, err
	}
	boot, err := procfs.BootTime(opts.ProcRoot)
	if err != nil {
		return Result{}, err
	}
	lookupUser := opts.LookupUser
	if lookupUser == nil {
		lookupUser = systemUser
	}

	type key struct{ pid, port int }
	groups := map[key]*Listener{}
	ownedPorts := map[int]bool{}
	unownedPorts := map[int]bool{}
	gitCache := map[string]*gitinfo.Info{}

	for _, s := range sockets {
		pid, ok := table.SocketPID[s.Inode]
		if !ok {
			unownedPorts[s.Port] = true
			continue
		}
		ownedPorts[s.Port] = true
		k := key{pid, s.Port}
		if l, ok := groups[k]; ok {
			l.Addresses = appendAddr(l.Addresses, s.Addr)
			continue
		}
		p := table.Procs[pid]
		l := &Listener{
			Port:      s.Port,
			Addresses: []string{s.Addr},
			PID:       pid,
			UID:       p.UID,
			Dir:       p.CWD,
			StartedAt: p.StartedAt(boot),
		}
		if l.UID < 0 {
			l.UID = s.UID // socket table still knows the owner
		}
		l.User = lookupUser(l.UID)
		l.Argv = strings.Join(p.Cmdline, " ")
		if l.Argv == "" {
			l.Argv = "[" + p.Comm + "]"
		}
		l.Command, l.Kind = classify.Classify(p.Comm, p.Cmdline, p.Exe)
		l.Age = opts.Now.Sub(l.StartedAt)
		if l.Age < 0 {
			l.Age = 0
		}
		if !opts.NoGit && p.CWD != "" {
			info, ok := gitCache[p.CWD]
			if info == nil && !ok {
				if gi, found := gitinfo.Lookup(p.CWD); found {
					info = &gi
				}
				gitCache[p.CWD] = info
			}
			if info != nil {
				l.GitRoot = info.Root
				l.Branch = info.Branch
				l.Detached = info.Detached
			}
		}
		switch {
		case l.GitRoot != "":
			l.Project = pathBase(l.GitRoot)
		case l.Dir != "":
			l.Project = pathBase(l.Dir)
		}
		groups[k] = l
	}

	res := Result{}
	for port := range unownedPorts {
		if !ownedPorts[port] && wantPort(opts.Ports, port) {
			res.Unowned++
		}
	}
	for _, l := range groups {
		if !wantPort(opts.Ports, l.Port) {
			continue
		}
		if opts.All || visible(l) {
			sortAddrs(l.Addresses)
			res.Listeners = append(res.Listeners, *l)
		} else {
			res.Hidden++
		}
	}
	sort.Slice(res.Listeners, func(i, j int) bool {
		a, b := res.Listeners[i], res.Listeners[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.PID < b.PID
	})
	return res, nil
}

// visible implements the default view: recognized dev servers always show;
// unrecognized processes show when they run inside a git repository, which
// is the working definition of "a dev server you left running".
func visible(l *Listener) bool {
	if l.Kind == classify.Dev {
		return true
	}
	return l.Kind == classify.Other && l.GitRoot != ""
}

func wantPort(ports []int, port int) bool {
	if len(ports) == 0 {
		return true
	}
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}

// appendAddr adds addr if not already present.
func appendAddr(addrs []string, addr string) []string {
	for _, a := range addrs {
		if a == addr {
			return addrs
		}
	}
	return append(addrs, addr)
}

// sortAddrs orders IPv4 before IPv6, then lexically, so output is stable
// and the address people bound ("127.0.0.1") leads.
func sortAddrs(addrs []string) {
	sort.Slice(addrs, func(i, j int) bool {
		v4i := strings.Contains(addrs[i], ".")
		v4j := strings.Contains(addrs[j], ".")
		if v4i != v4j {
			return v4i
		}
		return addrs[i] < addrs[j]
	})
}

// pathBase is filepath.Base without importing path/filepath twice; it also
// guards the degenerate "/" case to keep the column readable.
func pathBase(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// systemUser resolves a uid through the host account database, falling
// back to the bare number when the uid has no passwd entry.
func systemUser(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil && u.Username != "" {
		return u.Username
	}
	return strconv.Itoa(uid)
}
