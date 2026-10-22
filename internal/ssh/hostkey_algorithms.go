package ssh

import (
	"crypto/ed25519"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"lara4ever/internal/config"
)

// Prefer keys already trusted by OpenSSH when a server offers several key
// types. This snapshot changes negotiation only: the callback still reloads
// and verifies the actual key under its lock before any authentication.
func (m *Manager) hostKeyAlgorithms(host config.SSHHost) []string {
	port := host.Port
	if port == 0 {
		port = 22
	}
	address := net.JoinHostPort(strings.ToLower(sshHostname(host.Address)), strconv.Itoa(port))
	check, err := knownhosts.New(m.knownHosts)
	if err != nil {
		return nil
	} // final verification reports missing/invalid trust
	probe, err := xssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		return nil
	}
	err = check(address, hostKeyAddress(address), probe)
	var mismatch *knownhosts.KeyError
	if !errors.As(err, &mismatch) || len(mismatch.Want) == 0 {
		return nil
	}
	contents, err := os.ReadFile(m.knownHosts)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(contents), "\n")
	defaults := append(xssh.SupportedAlgorithms().HostKeys, xssh.InsecureAlgorithms().HostKeys...)
	allowed := make(map[string]bool, len(defaults))
	for _, algorithm := range defaults {
		allowed[algorithm] = true
	}
	seen := map[string]bool{}
	var preferred []string
	add := func(algorithm string) {
		if allowed[algorithm] && !seen[algorithm] {
			preferred = append(preferred, algorithm)
			seen[algorithm] = true
		}
	}
	// A matching CA can sign a leaf with a different key type. Prefer all
	// certificate algorithms; never mistake the CA's raw key for the leaf.
	for _, known := range mismatch.Want {
		if known.Line > 0 && known.Line <= len(lines) && strings.HasPrefix(strings.TrimSpace(lines[known.Line-1]), "@cert-authority") {
			for _, algorithm := range defaults {
				if strings.Contains(algorithm, "-cert-") {
					add(algorithm)
				}
			}
		}
	}
	for _, known := range mismatch.Want {
		if known.Line <= 0 || known.Line > len(lines) {
			continue
		}
		fields := strings.Fields(lines[known.Line-1])
		if len(fields) == 0 || strings.HasPrefix(fields[0], "@") {
			continue
		}
		switch known.Key.Type() {
		case xssh.KeyAlgoRSA:
			add(xssh.KeyAlgoRSASHA512)
			add(xssh.KeyAlgoRSASHA256)
			add(xssh.KeyAlgoRSA)
		case xssh.CertAlgoRSAv01:
			add(xssh.CertAlgoRSASHA512v01)
			add(xssh.CertAlgoRSASHA256v01)
			add(xssh.CertAlgoRSAv01)
		default:
			add(known.Key.Type())
		}
	}
	if len(preferred) == 0 {
		return nil
	}
	for _, algorithm := range defaults {
		add(algorithm)
	}
	return preferred
}
