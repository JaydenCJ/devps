// Package procfs reads the Linux proc filesystem: listening TCP sockets
// and the processes that own them. Every entry point takes an explicit
// proc root so tests (and hosts inspecting a container's proc mount) can
// point at any tree instead of the live /proc.
package procfs

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Socket is one listening TCP socket parsed from net/tcp or net/tcp6.
type Socket struct {
	Proto string // "tcp" or "tcp6"
	Addr  string // literal listen address: "127.0.0.1", "0.0.0.0", "::1", "::"
	Port  int
	UID   int    // socket owner as reported by the kernel
	Inode uint64 // joins the socket to a process via /proc/<pid>/fd
}

// tcpStateListen is the hex socket state the kernel prints for LISTEN.
const tcpStateListen = "0A"

// ListenTCP returns every LISTEN socket under the given proc root, reading
// net/tcp and net/tcp6. A missing table (e.g. IPv6 disabled) is tolerated;
// both missing means root is not a proc filesystem and is an error.
func ListenTCP(root string) ([]Socket, error) {
	var out []Socket
	opened := false
	for _, name := range []string{"tcp", "tcp6"} {
		path := filepath.Join(root, "net", name)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		opened = true
		socks, err := parseSocketTable(f, name)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, socks...)
	}
	if !opened {
		return nil, fmt.Errorf("%s: no net/tcp or net/tcp6 table (is this a Linux proc filesystem?)", root)
	}
	return out, nil
}

// parseSocketTable reads one kernel socket table. Lines it cannot parse are
// skipped rather than fatal: the table format is stable, but a truncated
// read mid-line must never take the whole listing down.
func parseSocketTable(f *os.File, proto string) ([]Socket, error) {
	var out []Socket
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		// Layout: sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode …
		if len(fields) < 10 || !strings.HasSuffix(fields[0], ":") {
			continue // header line or junk
		}
		if fields[3] != tcpStateListen {
			continue
		}
		addr, port, err := parseHexAddr(fields[1])
		if err != nil {
			continue
		}
		uid, err := strconv.Atoi(fields[7])
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, Socket{Proto: proto, Addr: addr, Port: port, UID: uid, Inode: inode})
	}
	return out, sc.Err()
}

// parseHexAddr decodes the kernel's "HEXADDR:HEXPORT" notation. The address
// is a sequence of 32-bit words, each printed in host (little-endian) byte
// order, so every 4-byte group must be reversed to recover network order.
func parseHexAddr(s string) (string, int, error) {
	hexAddr, hexPort, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, fmt.Errorf("no port separator in %q", s)
	}
	port64, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return "", 0, fmt.Errorf("bad port in %q: %w", s, err)
	}
	raw, err := hex.DecodeString(hexAddr)
	if err != nil {
		return "", 0, fmt.Errorf("bad address in %q: %w", s, err)
	}
	if len(raw)%4 != 0 {
		return "", 0, fmt.Errorf("address in %q is not word-aligned", s)
	}
	for i := 0; i < len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	var addr netip.Addr
	switch len(raw) {
	case 4:
		addr = netip.AddrFrom4([4]byte(raw))
	case 16:
		// Unmap so a v4-mapped listener ("::ffff:127.0.0.1") prints as
		// the dotted quad users actually typed into their tooling.
		addr = netip.AddrFrom16([16]byte(raw)).Unmap()
	default:
		return "", 0, fmt.Errorf("address in %q has %d bytes", s, len(raw))
	}
	return addr.String(), int(port64), nil
}
