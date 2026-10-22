package ssh

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"

	xssh "golang.org/x/crypto/ssh"
)

// Session is a live SSH connection wrapped to satisfy the in-app PTY
// interface (io.ReadWriteCloser + Resize). It deliberately does NOT import
// our pty package — pty.Session is structurally compatible with this type.
type Session struct {
	client  *xssh.Client
	session *xssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader // multiplexed stdout+stderr
}

// Client returns the underlying *xssh.Client so other subsystems (e.g. SFTP)
// can layer on top of the already-authenticated transport.
func (s *Session) Client() *xssh.Client { return s.client }

func (s *Session) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *Session) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *Session) Resize(cols, rows int) error {
	if s.session == nil {
		return nil
	}
	return s.session.WindowChange(rows, cols)
}
func (s *Session) Close() error {
	var firstErr error
	if s.session != nil {
		if err := s.session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.client != nil {
		if err := s.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Stage labels emitted by Connect via the onStage callback. Kept in a single
// list so the frontend timeline can render them in order without the backend
// hard-coding any ordering on its side beyond the sequence of calls.
const (
	StageDial      = "dial"
	StageHandshake = "handshake"
	StageSession   = "session"
	StagePTY       = "pty"
	StageShell     = "shell"
	StageReady     = "ready"
)

// Connect opens an SSH session to h, allocates a remote PTY, and starts an
// interactive shell. Auth method is picked from h.AuthMode:
//   - "password": uses h.Password
//   - "key":      uses the private key file at <hostDir>/<h.KeyName>
//   - "both":     tries the key first, falls back to password
//
// onStage (optional) is invoked at each milestone so the UI can render a
// progress timeline. It MUST be safe to call from any goroutine — the
// caller is expected to forward to a thread-safe sink (e.g. wruntime.EventsEmit).
//
// Host-key verification shares the external client's accept-new policy and
// known_hosts file: first-seen keys are persisted, changed keys are rejected.
func (m *Manager) Connect(h config.SSHHost, cols, rows int, onStage func(stage string)) (*Session, error) {
	emit := func(s string) {
		if onStage != nil {
			onStage(s)
		}
	}
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
	via := ""
	if strings.TrimSpace(h.ProxyCommand) != "" {
		via = " via ProxyCommand"
	} else if pj := strings.TrimSpace(h.ProxyJump); pj != "" {
		via = " via " + pj
	}
	log.Printf("[ssh] dialing %s%s as %s (auth=%s)", addr, via, h.User, h.AuthMode)
	emit(StageDial)

	// proxyDial handles direct (forced-IPv4), ProxyJump (chained ssh
	// transport), and ProxyCommand (child-process stdio) uniformly.
	conn, err := m.proxyDial(h, cfg.Timeout)
	if err != nil {
		log.Printf("[ssh] dial failed: %v", err)
		return nil, i18n.Wrapf(err, "errors.ssh.connectFailed", map[string]any{"addr": addr, "error": err})
	}
	log.Printf("[ssh] tcp connected, starting handshake")
	emit(StageHandshake)
	_ = conn.SetDeadline(time.Now().Add(cfg.Timeout))
	c, chans, reqs, err := xssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		log.Printf("[ssh] handshake failed: %v", err)
		return nil, friendlyAuthError(addr, h, err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := xssh.NewClient(c, chans, reqs)
	log.Printf("[ssh] connected, opening session")
	emit(StageSession)

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		log.Printf("[ssh] open session failed: %v", err)
		return nil, i18n.Wrapf(err, "errors.ssh.openSessionFailed", nil)
	}
	log.Printf("[ssh] session open, requesting pty")
	emit(StagePTY)

	if cols <= 0 {
		cols = 100
	}
	if rows <= 0 {
		rows = 30
	}
	modes := xssh.TerminalModes{
		xssh.ECHO:          1,
		xssh.TTY_OP_ISPEED: 14400,
		xssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, i18n.Wrapf(err, "errors.ssh.requestPtyFailed", nil)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, i18n.Wrapf(err, "errors.ssh.stdinPipeFailed", nil)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, i18n.Wrapf(err, "errors.ssh.stdoutPipeFailed", nil)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, i18n.Wrapf(err, "errors.ssh.stderrPipeFailed", nil)
	}

	emit(StageShell)
	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		log.Printf("[ssh] start shell failed: %v", err)
		return nil, i18n.Wrapf(err, "errors.ssh.startShellFailed", nil)
	}
	log.Printf("[ssh] shell running on %s", addr)
	emit(StageReady)

	return &Session{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  io.MultiReader(stdout, stderr),
	}, nil
}

// friendlyAuthError translates the cryptic "ssh: handshake failed: ssh:
// unable to authenticate, attempted methods [...]" into something the user
// can act on (the raw text is appended at the end for diagnostics).
func friendlyAuthError(addr string, h config.SSHHost, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"):
		hint := ""
		switch h.AuthMode {
		case "password":
			hint = "the server rejected the password — double-check the value (watch for stray spaces from copy/paste) or whether the server allows password login (PasswordAuthentication yes / no 2FA)"
		case "key":
			hint = "the server rejected the key — make sure the public key is in the remote ~/.ssh/authorized_keys for the user, and that the file matches the one selected here"
		case "both":
			hint = "neither the key nor the password were accepted — confirm the public key is in remote ~/.ssh/authorized_keys and that the password is current"
		}
		return i18n.Errorf("errors.ssh.authFailed", map[string]any{"addr": addr, "hint": hint, "error": err})
	case strings.Contains(msg, "no supported methods remain"):
		return i18n.Errorf("errors.ssh.noSupportedAuthMethods", map[string]any{"addr": addr, "error": err})
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "deadline exceeded"):
		return i18n.Errorf("errors.ssh.handshakeTimeout", map[string]any{"addr": addr, "error": err})
	}
	return i18n.Wrapf(err, "errors.ssh.handshakeFailed", map[string]any{"addr": addr})
}

func buildAuth(hostDir string, h config.SSHHost) ([]xssh.AuthMethod, error) {
	wantKey := h.AuthMode == "key" || h.AuthMode == "both"
	wantPwd := h.AuthMode == "password" || h.AuthMode == "both"
	if !wantKey && !wantPwd {
		return nil, i18n.Errorf("errors.ssh.unknownAuthMode", map[string]any{"mode": h.AuthMode})
	}

	methods := make([]xssh.AuthMethod, 0, 3)

	// Key first when requested — public-key auth is silent and faster.
	if wantKey {
		if h.KeyName == "" {
			if h.AuthMode == "key" {
				return nil, i18n.Errorf("errors.ssh.keyAuthNoFileChosen", nil)
			}
			// "both" + no key → just skip the key part and rely on password.
		} else {
			path := filepath.Join(hostDir, h.KeyName)
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, i18n.Wrapf(err, "errors.ssh.readKeyFailed", map[string]any{"path": path})
			}
			signer, err := xssh.ParsePrivateKey(b)
			if err != nil {
				return nil, i18n.Wrapf(err, "errors.ssh.parseKeyFailed", map[string]any{"path": path})
			}
			methods = append(methods, xssh.PublicKeys(signer))
		}
	}

	if wantPwd {
		// Trim trailing newlines / spaces that occasionally sneak in via
		// clipboard paste — they make sshd reject an otherwise correct
		// password without any obvious clue to the user.
		pw := strings.TrimRight(h.Password, "\r\n\t ")
		if pw == "" {
			if h.PasswordUnavailable && len(methods) == 0 {
				return nil, errors.New("saved SSH password is unavailable for this Windows account; re-enter and save the password in SSH host settings")
			}
			if h.AuthMode == "password" {
				return nil, i18n.Errorf("errors.ssh.passwordAuthEmpty", nil)
			}
			// "both" + no password → keep going with whatever key methods we
			// gathered above.
		} else {
			// Order: keyboard-interactive FIRST, then plain password. Many
			// modern sshd configs (PAM-backed) only honor kbd-interactive
			// even though they advertise "password" — putting it first lets
			// us auth before the server ever runs out of attempts.
			kbd := xssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			})
			methods = append(methods, kbd, xssh.Password(pw))
		}
	}

	if len(methods) == 0 {
		return nil, i18n.Errorf("errors.ssh.authModeNoCredentials", map[string]any{"mode": h.AuthMode})
	}
	return methods, nil
}
