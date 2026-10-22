package ssh

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
)

// Manager builds the ssh.exe command line for a configured host. The actual
// PTY spawn is done by app.go (so it reuses the same data/exit pipeline as
// the local terminal tabs) — this just centralises the argument logic.
type Manager struct {
	keysRoot    string // e.g. C:\laraenv\ssh
	knownHosts  string // e.g. C:\laraenv\ssh\known_hosts
	hostDirFunc func(host string) string
	// hostLookup resolves another saved host by Name — used to chain
	// ProxyJump connections through the user's saved hosts. Optional;
	// when nil, ProxyJump on a saved-host name returns an error.
	hostLookup  func(name string) (config.SSHHost, bool)
	dialTimeout func(network, address string, timeout time.Duration) (net.Conn, error)
}

func NewManager(keysRoot, knownHosts string, hostDir func(string) string) *Manager {
	return &Manager{keysRoot: keysRoot, knownHosts: knownHosts, hostDirFunc: hostDir, dialTimeout: net.DialTimeout}
}

// SetHostLookup wires a resolver for ProxyJump references. Safe to call
// after construction (the deploy-tui constructs a Manager without one).
func (m *Manager) SetHostLookup(f func(name string) (config.SSHHost, bool)) {
	m.hostLookup = f
}

func (m *Manager) HostDir(host string) string { return m.hostDirFunc(host) }

// SSHExecutable returns the absolute path to ssh.exe on Windows. Returns an
// error if the OpenSSH client is not installed (Windows 10+ ships it under
// optional features).
func SSHExecutable() (string, error) {
	if p, err := exec.LookPath("ssh.exe"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("ssh"); err == nil {
		return p, nil
	}
	return "", i18n.Errorf("errors.ssh.clientNotFound", nil)
}

// BuildCommandLine returns the full ssh.exe command line for h. When the
// host uses key auth, the absolute path to its private key is passed via -i.
// Password auth simply omits -i and lets ssh prompt the user inside the PTY.
func (m *Manager) BuildCommandLine(h config.SSHHost) (string, error) {
	exe, err := SSHExecutable()
	if err != nil {
		return "", err
	}
	if h.Address == "" {
		return "", i18n.Errorf("errors.ssh.hostMissingAddress", map[string]any{"host": h.Name})
	}
	if h.User == "" {
		return "", i18n.Errorf("errors.ssh.hostMissingUser", map[string]any{"host": h.Name})
	}
	port := h.Port
	if port == 0 {
		port = 22
	}

	var b strings.Builder
	b.WriteString(quote(exe))
	fmt.Fprintf(&b, " -p %d", port)
	fmt.Fprintf(&b, ` -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=%s`, quote(m.knownHosts))
	if h.AuthMode == "key" && h.KeyName != "" {
		key := m.hostDirFunc(h.Name) + `\` + h.KeyName
		fmt.Fprintf(&b, " -i %s", quote(key))
	}
	// ProxyCommand wins over ProxyJump if both are set, matching OpenSSH.
	if pc := strings.TrimSpace(h.ProxyCommand); pc != "" {
		fmt.Fprintf(&b, ` -o ProxyCommand=%s`, quote(pc))
	} else if pj := strings.TrimSpace(h.ProxyJump); pj != "" {
		jumpArg, err := m.proxyJumpArg(pj)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, " -J %s", quote(jumpArg))
	}
	fmt.Fprintf(&b, " %s@%s", h.User, h.Address)
	return b.String(), nil
}

// proxyJumpArg turns a ProxyJump value into the [user@]host[:port] string
// ssh.exe expects after -J. If pj matches a saved host, we expand from
// that host's user/address/port (so the user just picks a name in the UI).
// Otherwise we pass through whatever they typed.
func (m *Manager) proxyJumpArg(pj string) (string, error) {
	if m.hostLookup != nil {
		if jh, ok := m.hostLookup(pj); ok {
			port := jh.Port
			if port == 0 {
				port = 22
			}
			address := net.JoinHostPort(sshHostname(jh.Address), fmt.Sprint(port))
			if jh.User == "" {
				return address, nil
			}
			return jh.User + "@" + address, nil
		}
	}
	return pj, nil
}

func sshHostname(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// quote wraps s in double quotes if it contains spaces. ssh.exe and the
// underlying CommandLineToArgvW handle escaped quotes via double-quote
// doubling, but our paths don't contain quotes so plain wrapping is enough.
func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}
