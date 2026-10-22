//go:build windows

// Package netports maps listening TCP ports to their owning process and can
// kill the process holding a given port. This is what `taskkill` cannot do:
// dev tools like `npx concurrently` → `php artisan serve` / `vite` re-parent
// their children, so a PID-tree kill of the launcher leaves the real listener
// alive and the port bound. We enumerate the port→PID table directly via the
// Windows IP Helper API (GetExtendedTcpTable) — no shelling out to netstat.
package netports

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"lara4ever/internal/i18n"
)

// Listener is one LISTENING TCP socket and the process that owns it.
type Listener struct {
	Port int
	PID  uint32
	Addr string // local address, e.g. "127.0.0.1", "0.0.0.0", "::1"
}

const (
	// TCP_TABLE_OWNER_PID_ALL returns every connection (incl. LISTEN) with the
	// owning PID. We filter to LISTEN ourselves.
	tcpTableOwnerPIDAll = 5
	// MIB_TCP_STATE_LISTEN — the state value for a listening socket.
	mibTCPStateListen = 2
)

var (
	modiphlpapi             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCPTable = modiphlpapi.NewProc("GetExtendedTcpTable")
)

// mibTCPRowOwnerPID mirrors the Win32 MIB_TCPROW_OWNER_PID (IPv4) struct.
type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// mibTCP6RowOwnerPID mirrors MIB_TCP6ROW_OWNER_PID (IPv6).
type mibTCP6RowOwnerPID struct {
	LocalAddr     [16]byte
	LocalScopeID  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeID uint32
	RemotePort    uint32
	State         uint32
	OwningPID     uint32
}

// ntohsPort converts the network-byte-order LocalPort field (port in the low
// two bytes, big-endian) into a host int.
func ntohsPort(p uint32) int {
	return int(((p & 0xff) << 8) | ((p >> 8) & 0xff))
}

// Listeners returns every LISTENING TCP socket (IPv4 + IPv6) with its port and
// owning PID.
func Listeners() ([]Listener, error) {
	v4, err := listenersForFamily(windows.AF_INET)
	if err != nil {
		return nil, err
	}
	v6, err := listenersForFamily(windows.AF_INET6)
	if err != nil {
		// IPv6 enumeration is best-effort; return what we have from IPv4.
		return v4, nil
	}
	return append(v4, v6...), nil
}

func listenersForFamily(af int) ([]Listener, error) {
	var size uint32
	// First call sizes the buffer.
	r0, _, _ := procGetExtendedTCPTable.Call(
		0, uintptr(unsafe.Pointer(&size)), 0,
		uintptr(af), uintptr(tcpTableOwnerPIDAll), 0,
	)
	// 122 == ERROR_INSUFFICIENT_BUFFER, expected on the sizing call.
	if r0 != 0 && r0 != uintptr(syscall.ERROR_INSUFFICIENT_BUFFER) {
		return nil, i18n.Errorf("errors.netports.getExtendedTcpTableSizeFailed", map[string]any{"code": r0})
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	r0, _, _ = procGetExtendedTCPTable.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0,
		uintptr(af), uintptr(tcpTableOwnerPIDAll), 0,
	)
	if r0 != 0 {
		return nil, i18n.Errorf("errors.netports.getExtendedTcpTableFailed", map[string]any{"code": r0})
	}
	return parseTable(buf, af), nil
}

// parseTable decodes a DWORD entry count followed by a packed array of row
// structs. Split out so it can be unit-tested with a synthetic buffer.
func parseTable(buf []byte, af int) []Listener {
	if len(buf) < 4 {
		return nil
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	out := make([]Listener, 0, n)
	rows := buf[4:]
	if af == windows.AF_INET {
		sz := int(unsafe.Sizeof(mibTCPRowOwnerPID{}))
		for i := 0; i < int(n); i++ {
			off := i * sz
			if off+sz > len(rows) {
				break
			}
			row := (*mibTCPRowOwnerPID)(unsafe.Pointer(&rows[off]))
			if row.State != mibTCPStateListen {
				continue
			}
			a := row.LocalAddr
			out = append(out, Listener{
				Port: ntohsPort(row.LocalPort),
				PID:  row.OwningPID,
				Addr: fmt.Sprintf("%d.%d.%d.%d", a&0xff, (a>>8)&0xff, (a>>16)&0xff, (a>>24)&0xff),
			})
		}
		return out
	}
	sz := int(unsafe.Sizeof(mibTCP6RowOwnerPID{}))
	for i := 0; i < int(n); i++ {
		off := i * sz
		if off+sz > len(rows) {
			break
		}
		row := (*mibTCP6RowOwnerPID)(unsafe.Pointer(&rows[off]))
		if row.State != mibTCPStateListen {
			continue
		}
		out = append(out, Listener{
			Port: ntohsPort(row.LocalPort),
			PID:  row.OwningPID,
			Addr: "::", // exact v6 address isn't needed for our loopback checks
		})
	}
	return out
}

// ProcessName returns the executable base name (e.g. "php.exe") for a PID, or
// "" if it can't be resolved.
func ProcessName(pid uint32) string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return ""
	}
	for {
		if entry.ProcessID == pid {
			return windows.UTF16ToString(entry.ExeFile[:])
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return ""
		}
	}
}

// snapshotParents returns a child→parent PID map and the full list of live PIDs
// from a single process snapshot.
func snapshotParents() (parent map[uint32]uint32, all []uint32) {
	parent = map[uint32]uint32{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return parent, all
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if windows.Process32First(snap, &e) != nil {
		return parent, all
	}
	for {
		parent[e.ProcessID] = e.ParentProcessID
		all = append(all, e.ProcessID)
		if windows.Process32Next(snap, &e) != nil {
			break
		}
	}
	return parent, all
}

// TreeOwners maps every live PID that is one of roots — or a descendant of one
// — to its owning root PID. Lets callers attribute an actual listener (often a
// grandchild like `php -S`) back to the dev server / service that spawned it.
func TreeOwners(roots []int) map[int]int {
	parent, all := snapshotParents()
	rootSet := map[uint32]bool{}
	for _, r := range roots {
		rootSet[uint32(r)] = true
	}
	owner := map[int]int{}
	for _, pid := range all {
		// Walk up the parent chain (bounded) until we hit a root.
		cur := pid
		for hops := 0; hops < 64; hops++ {
			if rootSet[cur] {
				owner[int(pid)] = int(cur)
				break
			}
			p, ok := parent[cur]
			if !ok || p == 0 || p == cur {
				break
			}
			cur = p
		}
	}
	return owner
}

// PortsForTree returns the listening ports owned by rootPID or any of its live
// descendants. Used at Stop time to free every port the dev server's process
// tree was holding, even when a child detached from the launcher.
func PortsForTree(rootPID int) []int {
	owner := TreeOwners([]int{rootPID})
	ls, err := Listeners()
	if err != nil {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	for _, l := range ls {
		if _, ok := owner[int(l.PID)]; ok && isLoopbackOrAny(l.Addr) && !seen[l.Port] {
			seen[l.Port] = true
			out = append(out, l.Port)
		}
	}
	return out
}

// isLoopbackOrAny reports whether addr is a local listening address we care
// about for our own services (loopback or wildcard).
func isLoopbackOrAny(addr string) bool {
	switch addr {
	case "127.0.0.1", "0.0.0.0", "::", "::1":
		return true
	}
	return strings.HasPrefix(addr, "127.")
}

// PIDsOnPort returns the PIDs of processes LISTENING on port (loopback/any).
func PIDsOnPort(port int) []uint32 {
	ls, err := Listeners()
	if err != nil {
		return nil
	}
	seen := map[uint32]bool{}
	var out []uint32
	for _, l := range ls {
		if l.Port == port && isLoopbackOrAny(l.Addr) && !seen[l.PID] {
			seen[l.PID] = true
			out = append(out, l.PID)
		}
	}
	return out
}

// KillPort kills the process(es) LISTENING on port. When allowExes is
// non-empty, only PIDs whose executable base name (case-insensitive) is in the
// set are killed — a safety guard so we never nuke an unrelated app that
// happens to hold the port. Returns the PIDs actually killed.
func KillPort(port int, allowExes ...string) (killed []int, err error) {
	allow := map[string]bool{}
	for _, e := range allowExes {
		allow[strings.ToLower(e)] = true
	}
	for _, pid := range PIDsOnPort(port) {
		if pid == 0 {
			continue
		}
		if len(allow) > 0 {
			name := strings.ToLower(ProcessName(pid))
			if !allow[name] {
				continue
			}
		}
		if e := killTree(pid); e != nil {
			err = e
			continue
		}
		killed = append(killed, int(pid))
	}
	return killed, err
}

// killTree terminates a PID and its child tree via taskkill, matching the
// pattern used elsewhere in the app.
func killTree(pid uint32) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(int(pid)))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, e := cmd.CombinedOutput(); e != nil {
		// taskkill exits non-zero if the PID already died — treat "not found"
		// as success.
		s := strings.ToLower(string(out))
		if strings.Contains(s, "not found") || strings.Contains(s, "no running") {
			return nil
		}
		return i18n.Wrapf(e, "errors.netports.taskkillFailed", map[string]any{"pid": pid, "output": strings.TrimSpace(string(out))})
	}
	return nil
}
