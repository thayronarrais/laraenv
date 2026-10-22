//go:build windows

package netports

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNtohsPort(t *testing.T) {
	// htons(8000) == 0x401F as stored in MIB_TCPROW_OWNER_PID.LocalPort.
	if got := ntohsPort(0x401F); got != 8000 {
		t.Fatalf("ntohsPort(0x401F) = %d, want 8000", got)
	}
	if got := ntohsPort(0x5000); got != 80 { // htons(80) == 0x5000
		t.Fatalf("ntohsPort(0x5000) = %d, want 80", got)
	}
}

// buildIPv4Table packs a MIB_TCPTABLE_OWNER_PID-shaped buffer for tests:
// DWORD count followed by 6×uint32 rows.
func buildIPv4Row(state, localAddr, localPort, pid uint32) []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint32(b[0:], state)
	binary.LittleEndian.PutUint32(b[4:], localAddr)
	binary.LittleEndian.PutUint32(b[8:], localPort)
	binary.LittleEndian.PutUint32(b[12:], 0) // remote addr
	binary.LittleEndian.PutUint32(b[16:], 0) // remote port
	binary.LittleEndian.PutUint32(b[20:], pid)
	return b
}

func TestParseTableFiltersListen(t *testing.T) {
	count := make([]byte, 4)
	binary.LittleEndian.PutUint32(count, 2)
	// Row 1: LISTEN on 127.0.0.1:8000, pid 4242.
	// 127.0.0.1 little-endian uint32 = 0x0100007F.
	listen := buildIPv4Row(mibTCPStateListen, 0x0100007F, 0x401F, 4242)
	// Row 2: ESTABLISHED (state 5) — must be filtered out.
	estab := buildIPv4Row(5, 0x0100007F, 0x401F, 9999)

	buf := append(append(count, listen...), estab...)
	got := parseTable(buf, windows.AF_INET)

	if len(got) != 1 {
		t.Fatalf("parseTable returned %d listeners, want 1: %+v", len(got), got)
	}
	l := got[0]
	if l.Port != 8000 || l.PID != 4242 || l.Addr != "127.0.0.1" {
		t.Fatalf("listener = %+v, want {8000 4242 127.0.0.1}", l)
	}
}
