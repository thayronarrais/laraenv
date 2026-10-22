package ssh

import (
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"

	xssh "golang.org/x/crypto/ssh"
)

// proxyDial returns a net.Conn to the target host's TCP endpoint, honoring
// ProxyCommand and ProxyJump if either is set on h. The returned conn is
// owned by the caller and must be closed.
//
// Callers should still wrap conn with xssh.NewClientConn to do the SSH
// handshake on top of it. The deadline is applied for the establishment
// step only; callers may clear it after handshake.
func (m *Manager) proxyDial(h config.SSHHost, timeout time.Duration) (net.Conn, error) {
	port := h.Port
	if port == 0 {
		port = 22
	}
	hostname := sshHostname(h.Address)
	addr := net.JoinHostPort(hostname, strconv.Itoa(port))

	// ProxyCommand wins if set: spawn the command, wrap its stdio as a Conn.
	if pc := strings.TrimSpace(h.ProxyCommand); pc != "" {
		return spawnProxyCommand(pc, h.Address, port)
	}

	// ProxyJump: dial the jump host first (recursively, so chained jumps
	// work), then ask its SSH client to open a TCP channel to our target.
	if pj := strings.TrimSpace(h.ProxyJump); pj != "" {
		if m.hostLookup == nil {
			return nil, i18n.Errorf("errors.ssh.proxyJumpNoLookup", map[string]any{"name": pj})
		}
		jh, ok := m.hostLookup(pj)
		if !ok {
			return nil, i18n.Errorf("errors.ssh.proxyJumpHostNotFound", map[string]any{"name": pj})
		}
		jumpClient, err := m.dialThroughProxy(jh, timeout)
		if err != nil {
			return nil, i18n.Wrapf(err, "errors.ssh.dialJumpHostFailed", map[string]any{"name": pj})
		}
		// xssh.Client.Dial gives us a net.Conn channeled through the SSH
		// transport. Closing it tears down just the channel; the parent
		// client is closed by the handshake wrapper below via its own conn.
		conn, err := jumpClient.Dial("tcp", addr)
		if err != nil {
			_ = jumpClient.Close()
			return nil, i18n.Wrapf(err, "errors.ssh.openChannelFailed", map[string]any{"via": pj, "addr": addr})
		}
		return &jumpConn{Conn: conn, parent: jumpClient}, nil
	}

	// Direct: same forced-IPv4 behavior as the original Dial. AAAA-first
	// resolution stalls many setups by ~30s before falling back.
	network := "tcp4"
	if strings.Contains(hostname, ":") {
		network = "tcp6"
	}
	return m.dialTimeout(network, addr, timeout)
}

// dialThroughProxy returns a fully-handshaked SSH client to h, going
// through any chained ProxyJump/ProxyCommand transitively.
func (m *Manager) dialThroughProxy(h config.SSHHost, timeout time.Duration) (*xssh.Client, error) {
	if h.User == "" {
		return nil, i18n.Errorf("errors.ssh.jumpHostMissingUser", map[string]any{"host": h.Name})
	}
	auth, err := buildAuth(m.hostDirFunc(h.Name), h)
	if err != nil {
		return nil, err
	}
	cfg := &xssh.ClientConfig{
		User:              h.User,
		Auth:              auth,
		HostKeyCallback:   m.hostKeyCallback(),
		HostKeyAlgorithms: m.hostKeyAlgorithms(h),
		Timeout:           timeout,
	}
	conn, err := m.proxyDial(h, timeout)
	if err != nil {
		return nil, err
	}
	port := h.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(sshHostname(h.Address), strconv.Itoa(port))
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, chans, reqs, err := xssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, friendlyAuthError(addr, h, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return xssh.NewClient(c, chans, reqs), nil
}

// jumpConn wraps a Conn whose lifetime is tied to a parent SSH client
// (the jump connection). Closing it also closes the parent so the jump
// transport doesn't leak when the caller is done.
type jumpConn struct {
	net.Conn
	parent io.Closer
}

func (j *jumpConn) Close() error {
	err := j.Conn.Close()
	if j.parent != nil {
		_ = j.parent.Close()
	}
	return err
}

// spawnProxyCommand starts cmd with %h/%p substituted and returns a Conn
// over its stdio. cmd is split using cmd.exe semantics — we hand the full
// string to "cmd.exe /c <cmd>" so users can use any shell construct they
// already know from their SSH config.
func spawnProxyCommand(cmd, host string, port int) (net.Conn, error) {
	expanded := strings.ReplaceAll(cmd, "%h", host)
	expanded = strings.ReplaceAll(expanded, "%p", strconv.Itoa(port))

	c := exec.Command("cmd.exe", "/c", expanded)
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.proxyCommandStdinFailed", nil)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, i18n.Wrapf(err, "errors.ssh.proxyCommandStdoutFailed", nil)
	}
	if err := c.Start(); err != nil {
		_ = stdin.Close()
		return nil, i18n.Wrapf(err, "errors.ssh.startProxyCommandFailed", nil)
	}
	return &procConn{cmd: c, stdin: stdin, stdout: stdout}, nil
}

// procConn adapts a child process's stdio into a net.Conn. Deadlines are
// no-ops (pipes don't support them); SSH's own Timeout in ClientConfig
// covers the handshake step, which is the only place we cared about a
// deadline anyway.
type procConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (p *procConn) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *procConn) Write(b []byte) (int, error) { return p.stdin.Write(b) }
func (p *procConn) Close() error {
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}
func (p *procConn) LocalAddr() net.Addr              { return procAddr{} }
func (p *procConn) RemoteAddr() net.Addr             { return procAddr{} }
func (p *procConn) SetDeadline(time.Time) error      { return nil }
func (p *procConn) SetReadDeadline(time.Time) error  { return nil }
func (p *procConn) SetWriteDeadline(time.Time) error { return nil }

type procAddr struct{}

func (procAddr) Network() string { return "proxycommand" }
func (procAddr) String() string  { return "proxycommand" }
