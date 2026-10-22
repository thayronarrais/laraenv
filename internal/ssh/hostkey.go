package ssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostKeyCallback applies OpenSSH's accept-new policy to the same file used
// by BuildCommandLine. Reload under an interprocess lock for every check:
// managers, detached windows and concurrent first connections must agree on
// the first persisted key. Existing, revoked or malformed trust is never reset.
func (m *Manager) hostKeyCallback() xssh.HostKeyCallback {
	return func(address string, _ net.Addr, key xssh.PublicKey) error {
		if strings.TrimSpace(m.knownHosts) == "" {
			return errors.New("SSH known_hosts path is not configured")
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || strings.ContainsAny(host, "\r\n\x00 \t,|*?!") {
			return errors.New("invalid SSH host address for host-key verification")
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("invalid SSH port for host-key verification")
		}
		address = net.JoinHostPort(strings.ToLower(host), strconv.Itoa(n))
		if err := os.MkdirAll(filepath.Dir(m.knownHosts), 0700); err != nil {
			return fmt.Errorf("prepare SSH known_hosts: %w", err)
		}
		lock, err := os.OpenFile(m.knownHosts+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return fmt.Errorf("open SSH known_hosts lock: %w", err)
		}
		defer lock.Close()
		if err := lockKnownHosts(lock); err != nil {
			return fmt.Errorf("lock SSH known_hosts: %w", err)
		}
		defer unlockKnownHosts(lock)

		file, err := openKnownHosts(m.knownHosts)
		if err != nil {
			return fmt.Errorf("open SSH known_hosts: %w", err)
		}
		defer file.Close()
		check, err := knownhosts.New(m.knownHosts)
		if err != nil {
			return fmt.Errorf("read SSH known_hosts: %w", err)
		}
		// A proxy's RemoteAddr may describe a jump endpoint or process stdio.
		// Verify only the requested logical host and port, as OpenSSH does.
		err = check(address, hostKeyAddress(address), key)
		if certificate, ok := key.(*xssh.Certificate); ok && err == nil {
			// knownhosts checks an exact certificate revocation, but does not
			// apply revoked raw keys to the certificate's leaf or authority.
			// Reuse the same locked trust snapshot for both revocation checks;
			// ordinary key mismatches must not replace certificate validation.
			for _, certifiedKey := range []xssh.PublicKey{certificate.Key, certificate.SignatureKey} {
				var revoked *knownhosts.RevokedError
				if candidateErr := check(address, hostKeyAddress(address), certifiedKey); errors.As(candidateErr, &revoked) {
					err = candidateErr
					break
				}
			}
		}
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) || len(keyErr.Want) != 0 {
			return fmt.Errorf("SSH host key verification failed for %s; verify the server fingerprint before editing %s: %w", address, m.knownHosts, err)
		}
		data, err := os.ReadFile(m.knownHosts)
		if err != nil {
			return fmt.Errorf("read SSH known_hosts: %w", err)
		}
		line := knownhosts.Line([]string{address}, key) + "\n"
		if len(data) > 0 && data[len(data)-1] != '\n' {
			line = "\n" + line
		}
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("append SSH host key: %w", err)
		}
		if _, err := file.WriteString(line); err != nil {
			return fmt.Errorf("save SSH host key: %w", err)
		}
		if err := file.Sync(); err != nil {
			return fmt.Errorf("persist SSH host key: %w", err)
		}
		return nil
	}
}

type hostKeyAddress string

func (a hostKeyAddress) Network() string { return "tcp" }
func (a hostKeyAddress) String() string  { return string(a) }
