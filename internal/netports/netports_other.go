//go:build !windows

// Package netports is Windows-only in practice (LaraEnv targets Windows). The
// non-windows build provides no-op stubs so the package still compiles.
package netports

// Listener is one LISTENING TCP socket and the process that owns it.
type Listener struct {
	Port int
	PID  uint32
	Addr string
}

// Listeners returns nothing off Windows.
func Listeners() ([]Listener, error) { return nil, nil }

// ProcessName returns "" off Windows.
func ProcessName(pid uint32) string { return "" }

// PIDsOnPort returns nothing off Windows.
func PIDsOnPort(port int) []uint32 { return nil }

// PortsForTree returns nothing off Windows.
func PortsForTree(rootPID int) []int { return nil }

// TreeOwners returns nothing off Windows.
func TreeOwners(roots []int) map[int]int { return nil }

// KillPort is a no-op off Windows.
func KillPort(port int, allowExes ...string) (killed []int, err error) { return nil, nil }
