// Tests for the kernel socket-table parser. The captured lines below are
// real /proc/net/tcp{,6} output, so the parser is checked against the
// kernel's actual formatting, not just against our own encoder.
package procfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JaydenCJ/devps/internal/procfstest"
)

const tcpHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

// writeTables writes raw tcp/tcp6 contents and returns the proc root.
func writeTables(t *testing.T, tcp, tcp6 string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proc")
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"tcp": tcp, "tcp6": tcp6} {
		if content == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, "net", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParsesIPv4LoopbackListenerFromRealCapturedLine(t *testing.T) {
	// 127.0.0.1:8080, LISTEN, uid 1000, inode 32921 — captured verbatim.
	line := "   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 32921 1 0000000000000000 100 0 0 10 0\n"
	socks, err := ListenTCP(writeTables(t, tcpHeader+line, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 {
		t.Fatalf("got %d sockets, want 1", len(socks))
	}
	s := socks[0]
	if s.Addr != "127.0.0.1" || s.Port != 8080 || s.UID != 1000 || s.Inode != 32921 {
		t.Fatalf("parsed %+v, want 127.0.0.1:8080 uid=1000 inode=32921", s)
	}
	if s.Proto != "tcp" {
		t.Fatalf("proto = %q, want tcp", s.Proto)
	}
}

func TestParsesIPv6AnyAddressListenerFromRealCapturedLine(t *testing.T) {
	// [::]:22 LISTEN, root-owned — captured verbatim from a stock sshd.
	line := "   0: 00000000000000000000000000000000:0016 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0\n"
	socks, err := ListenTCP(writeTables(t, tcpHeader, tcpHeader+line))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 {
		t.Fatalf("got %d sockets, want 1", len(socks))
	}
	s := socks[0]
	if s.Addr != "::" || s.Port != 22 || s.Proto != "tcp6" {
		t.Fatalf("parsed %+v, want [::]:22 on tcp6", s)
	}
}

func TestParsesIPv6LoopbackWordOrder(t *testing.T) {
	// ::1 exercises the per-word little-endian byte swap: the final word
	// prints as 01000000, and naive whole-address reversal would corrupt it.
	line := "   0: 00000000000000000000000001000000:0FA0 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 777 1 0000000000000000 100 0 0 10 0\n"
	socks, err := ListenTCP(writeTables(t, tcpHeader, tcpHeader+line))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 || socks[0].Addr != "::1" || socks[0].Port != 4000 {
		t.Fatalf("parsed %+v, want [::1]:4000", socks)
	}
}

func TestUnmapsV4MappedIPv6Address(t *testing.T) {
	// Node binds "::ffff:127.0.0.1" when given 127.0.0.1 on a dual-stack
	// host; users typed a dotted quad, so that is what devps must print.
	line := "   0: 0000000000000000FFFF00000100007F:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 888 1 0000000000000000 100 0 0 10 0\n"
	socks, err := ListenTCP(writeTables(t, tcpHeader, tcpHeader+line))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 || socks[0].Addr != "127.0.0.1" || socks[0].Port != 3000 {
		t.Fatalf("parsed %+v, want 127.0.0.1:3000 (unmapped)", socks)
	}
}

func TestSkipsSocketsInNonListenStates(t *testing.T) {
	established := procfstest.Sock{Addr: "127.0.0.1", Port: 5000, Inode: 1, State: "01"}
	timeWait := procfstest.Sock{Addr: "127.0.0.1", Port: 5001, Inode: 2, State: "06"}
	listen := procfstest.Sock{Addr: "127.0.0.1", Port: 5002, Inode: 3}
	tcp := tcpHeader +
		procfstest.EncodeSockLine(0, established) + "\n" +
		procfstest.EncodeSockLine(1, timeWait) + "\n" +
		procfstest.EncodeSockLine(2, listen) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 || socks[0].Port != 5002 {
		t.Fatalf("parsed %+v, want only the LISTEN row (port 5002)", socks)
	}
}

func TestSkipsHeaderAndMalformedLinesWithoutError(t *testing.T) {
	tcp := tcpHeader +
		"garbage line that is not a socket row\n" +
		"   0: NOTHEX:1F90 00000000:0000 0A x x x  1000 0 1 y\n" +
		procfstest.EncodeSockLine(1, procfstest.Sock{Addr: "0.0.0.0", Port: 80, Inode: 9}) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, ""))
	if err != nil {
		t.Fatalf("malformed lines must be skipped, not fatal: %v", err)
	}
	if len(socks) != 1 || socks[0].Port != 80 {
		t.Fatalf("parsed %+v, want only the valid row", socks)
	}
}

func TestWildcardIPv4AddressParses(t *testing.T) {
	tcp := tcpHeader + procfstest.EncodeSockLine(0,
		procfstest.Sock{Addr: "0.0.0.0", Port: 8000, Inode: 4}) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 || socks[0].Addr != "0.0.0.0" {
		t.Fatalf("parsed %+v, want 0.0.0.0", socks)
	}
}

func TestMissingTCP6TableIsTolerated(t *testing.T) {
	tcp := tcpHeader + procfstest.EncodeSockLine(0,
		procfstest.Sock{Addr: "127.0.0.1", Port: 3000, Inode: 5}) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, "")) // no tcp6 file at all
	if err != nil {
		t.Fatalf("an IPv6-less kernel must not be an error: %v", err)
	}
	if len(socks) != 1 {
		t.Fatalf("got %d sockets, want 1", len(socks))
	}
}

func TestMissingBothTablesIsAnError(t *testing.T) {
	if _, err := ListenTCP(t.TempDir()); err == nil {
		t.Fatal("a directory with no socket tables must error, or a typoed --proc-root would silently report an empty machine")
	}
}

func TestCombinesRowsFromBothTables(t *testing.T) {
	tcp := tcpHeader + procfstest.EncodeSockLine(0,
		procfstest.Sock{Addr: "127.0.0.1", Port: 5173, Inode: 10}) + "\n"
	tcp6 := tcpHeader + procfstest.EncodeSockLine(0,
		procfstest.Sock{Addr: "::", Port: 5173, Inode: 11}) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, tcp6))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 2 {
		t.Fatalf("got %d sockets, want 2 (one per table)", len(socks))
	}
	if socks[0].Proto != "tcp" || socks[1].Proto != "tcp6" {
		t.Fatalf("protos = %q,%q, want tcp,tcp6", socks[0].Proto, socks[1].Proto)
	}
}

func TestHighPortNumberDecodesFromHex(t *testing.T) {
	// 65535 = FFFF: the top of the range must survive the uint16 parse.
	tcp := tcpHeader + procfstest.EncodeSockLine(0,
		procfstest.Sock{Addr: "127.0.0.1", Port: 65535, Inode: 6}) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 1 || socks[0].Port != 65535 {
		t.Fatalf("parsed %+v, want port 65535", socks)
	}
}

func TestEncoderRoundTripAcrossAddressFamilies(t *testing.T) {
	// Round-trip a spread of addresses through the independent test
	// encoder; combined with the captured-line tests above this pins the
	// byte-order handling from both directions.
	cases := []procfstest.Sock{
		{Addr: "10.1.2.3", Port: 9000, UID: 7, Inode: 100},
		{Addr: "192.168.0.1", Port: 1, UID: 0, Inode: 101},
		{Addr: "::1", Port: 8443, UID: 1000, Inode: 102},
		{Addr: "fe80::1", Port: 9999, UID: 1000, Inode: 103},
	}
	tcp := tcpHeader +
		procfstest.EncodeSockLine(0, cases[0]) + "\n" +
		procfstest.EncodeSockLine(1, cases[1]) + "\n"
	tcp6 := tcpHeader +
		procfstest.EncodeSockLine(0, cases[2]) + "\n" +
		procfstest.EncodeSockLine(1, cases[3]) + "\n"
	socks, err := ListenTCP(writeTables(t, tcp, tcp6))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != len(cases) {
		t.Fatalf("got %d sockets, want %d", len(socks), len(cases))
	}
	for i, want := range cases {
		got := socks[i]
		if got.Addr != want.Addr || got.Port != want.Port || got.UID != want.UID || got.Inode != want.Inode {
			t.Errorf("case %d: got %+v, want %+v", i, got, want)
		}
	}
}
