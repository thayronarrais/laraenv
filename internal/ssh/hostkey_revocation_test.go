package ssh

import (
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestGoSSHRejectsCertificatesWithRevokedAuthorityOrLeaf(t *testing.T) {
	for _, mode := range []string{"dial", "connect", "jump"} {
		for _, revoked := range []string{"authority", "leaf", "certificate", "unrelated"} {
			t.Run(mode+"/"+revoked, func(t *testing.T) {
				manager := fixtureManager(t)
				authority, leaf := fixtureSigner(t), fixtureSigner(t)
				certificate := fixtureHostCertificate(t, leaf, authority, "127.0.0.1", false)
				host := fixtureSSH(t, manager, certificate)
				address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
				var revokedKey xssh.PublicKey
				switch revoked {
				case "authority":
					revokedKey = authority.PublicKey()
				case "leaf":
					revokedKey = leaf.PublicKey()
				case "certificate":
					revokedKey = certificate.PublicKey()
				default:
					revokedKey = fixtureSigner(t).PublicKey()
				}
				original := "@cert-authority " + knownhosts.Line([]string{address}, authority.PublicKey()) + "\n" +
					"@revoked " + knownhosts.Line([]string{"*"}, revokedKey) + "\n"
				if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				switch mode {
				case "connect":
					var session *Session
					session, err = manager.Connect(host, 80, 24, nil)
					if session != nil {
						_ = session.Close()
					}
				case "jump":
					var client *xssh.Client
					client, err = manager.dialThroughProxy(host, time.Second)
					if client != nil {
						_ = client.Close()
					}
				default:
					var client *xssh.Client
					client, err = manager.Dial(host)
					if client != nil {
						_ = client.Close()
					}
				}
				if revoked == "unrelated" {
					if err != nil {
						t.Fatalf("unrelated revocation rejected a valid trusted certificate: %v", err)
					}
				} else if err == nil {
					t.Fatalf("accepted a host certificate with revoked %s", revoked)
				}
				got, err := os.ReadFile(manager.knownHosts)
				if err != nil || string(got) != original {
					t.Fatal("certificate verification modified known_hosts")
				}
			})
		}
	}
}
