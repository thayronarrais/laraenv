package ssh

import (
	"net"
	"strconv"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"

	xssh "golang.org/x/crypto/ssh"
)

// Dial establishes a TCP + SSH connection to the host without requesting a
// PTY or starting a shell. The returned *ssh.Client lets the caller open
// short-lived sessions for one-off command execution (the deploy runner
// uses client.NewSession() + session.CombinedOutput() for each detection
// probe). Caller is responsible for closing the client when done.
//
// IPv4 is forced for the same reason as Connect — Go's default resolver
// can stall on AAAA records before falling back to A, which manifests as
// a 30-60s "timeout" even for hosts that plain ssh.exe reaches instantly.
func (m *Manager) Dial(h config.SSHHost) (*xssh.Client, error) {
	if h.Address == "" {
		return nil, i18n.Errorf("errors.ssh.hostMissingAddress", map[string]any{"host": h.Name})
	}
	if h.User == "" {
		return nil, i18n.Errorf("errors.ssh.hostMissingUser", map[string]any{"host": h.Name})
	}
	port := h.Port
	if port == 0 {
		port = 22
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
		Timeout:           60 * time.Second,
	}

	addr := net.JoinHostPort(sshHostname(h.Address), strconv.Itoa(port))
	conn, err := m.proxyDial(h, cfg.Timeout)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.ssh.connectFailed", map[string]any{"addr": addr, "error": err})
	}
	_ = conn.SetDeadline(time.Now().Add(cfg.Timeout))
	c, chans, reqs, err := xssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, friendlyAuthError(addr, h, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return xssh.NewClient(c, chans, reqs), nil
}
