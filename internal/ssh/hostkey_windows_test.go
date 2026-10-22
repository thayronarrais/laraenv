//go:build windows

package ssh

import (
	"net"
	"os"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestAcceptNewDoesNotRaceAnExternalKnownHostsWriter(t *testing.T) {
	m := fixtureManager(t)
	if err := os.WriteFile(m.knownHosts, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// OpenSSH does not participate in LaraEnv's sidecar lock. A writable
	// handle models an external first connection in its trust-update window.
	writer, err := os.OpenFile(m.knownHosts, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := m.hostKeyCallback()("example.test:22", procAddr{}, fixtureSigner(t).PublicKey()); err == nil {
		t.Fatal("accepted first-seen key while another writer could change trust")
	}
	data, err := os.ReadFile(m.knownHosts)
	if err != nil || len(data) != 0 {
		t.Fatal("trust was modified despite an active external writer")
	}
}

func TestReadOnlyKnownHostsAcceptsMatchingKeyWithoutUpdatingTrust(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		t.Run(strconv.FormatBool(trusted), func(t *testing.T) {
			manager := fixtureManager(t)
			signer := fixtureSigner(t)
			host := fixtureSSH(t, manager, signer)
			address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
			if !trusted {
				address = "other.invalid:22"
			}
			original := knownhosts.Line([]string{address}, signer.PublicKey()) + "\n"
			if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(manager.knownHosts, 0400); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(manager.knownHosts, 0600)
			client, err := manager.Dial(host)
			if client != nil {
				_ = client.Close()
			}
			if trusted && err != nil {
				t.Fatalf("matching key in read-only known_hosts was rejected: %v", err)
			}
			if !trusted && err == nil {
				t.Fatal("accepted a first-seen key without being able to persist it")
			}
			got, err := os.ReadFile(manager.knownHosts)
			if err != nil || string(got) != original {
				t.Fatal("verification changed read-only known_hosts")
			}
		})
	}
}
