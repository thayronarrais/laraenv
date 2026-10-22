package ssh

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func fixtureRSASigner(t *testing.T) xssh.AlgorithmSigner {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer.(xssh.AlgorithmSigner)
}

func fixtureECDSASigner(t *testing.T) xssh.Signer {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func fixtureHostCertificate(t *testing.T, leaf, authority xssh.Signer, principal string, expired bool) xssh.Signer {
	t.Helper()
	now := time.Now()
	certificate := &xssh.Certificate{
		Key: leaf.PublicKey(), CertType: xssh.HostCert, KeyId: "fixture-only",
		ValidPrincipals: []string{principal},
		ValidAfter:      uint64(now.Add(-time.Hour).Unix()),
		ValidBefore:     uint64(now.Add(time.Hour).Unix()),
	}
	if expired {
		certificate.ValidBefore = uint64(now.Add(-time.Minute).Unix())
	}
	if err := certificate.SignCert(rand.Reader, authority); err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewCertSigner(certificate, leaf)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestGoSSHNegotiatesTrustedRSAWithOnlySHA2Signatures(t *testing.T) {
	rsaSigner := fixtureRSASigner(t)
	for _, algorithm := range []string{xssh.KeyAlgoRSASHA256, xssh.KeyAlgoRSASHA512} {
		t.Run(algorithm, func(t *testing.T) {
			manager := fixtureManager(t)
			// This signer cannot offer or emit ssh-rsa/SHA-1 signatures. A
			// successful handshake proves that the trusted RSA aliases work.
			signer, err := xssh.NewSignerWithAlgorithms(rsaSigner, []string{algorithm})
			if err != nil {
				t.Fatal(err)
			}
			host := fixtureSSH(t, manager, fixtureECDSASigner(t), signer)
			address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
			original := knownhosts.Line([]string{knownhosts.HashHostname(knownhosts.Normalize(address))}, signer.PublicKey()) + "\n"
			if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := manager.Dial(host)
			if err != nil {
				t.Fatalf("trusted RSA key with %s was not negotiated: %v", algorithm, err)
			}
			_ = client.Close()
			got, err := os.ReadFile(manager.knownHosts)
			if err != nil || string(got) != original {
				t.Fatal("RSA negotiation changed existing trust")
			}
		})
	}
}

func TestGoSSHNegotiatesHostCertificateFromTabSeparatedAuthority(t *testing.T) {
	rsaSigner, authority := fixtureRSASigner(t), fixtureSigner(t)
	for _, algorithm := range []string{xssh.KeyAlgoRSASHA256, xssh.KeyAlgoRSASHA512} {
		t.Run(algorithm, func(t *testing.T) {
			manager := fixtureManager(t)
			leaf, err := xssh.NewSignerWithAlgorithms(rsaSigner, []string{algorithm})
			if err != nil {
				t.Fatal(err)
			}
			certificate := fixtureHostCertificate(t, leaf, authority, "127.0.0.1", false)
			host := fixtureSSH(t, manager, fixtureSigner(t), certificate)
			address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
			// The authority and leaf intentionally have different key types.
			// An old raw ED25519 pin competes with the authority. Its key is no
			// longer offered, so skipping the tab marker or promoting the CA's
			// raw type would negotiate the untrusted raw ED25519 key and fail.
			original := knownhosts.Line([]string{address}, fixtureSigner(t).PublicKey()) + "\n@cert-authority\t" + knownhosts.Line([]string{address}, authority.PublicKey()) + "\n"
			if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			session, err := manager.Connect(host, 80, 24, nil)
			if err != nil {
				t.Fatalf("trusted authority did not negotiate its RSA certificate with %s: %v", algorithm, err)
			}
			_ = session.Close()
			got, err := os.ReadFile(manager.knownHosts)
			if err != nil || string(got) != original {
				t.Fatal("certificate connection changed existing CA trust")
			}
		})
	}
}

func TestGoSSHNegotiationHonorsNegatedRawKeysAndAuthorities(t *testing.T) {
	for _, marker := range []string{"", "@cert-authority\t"} {
		t.Run(marker, func(t *testing.T) {
			manager := fixtureManager(t)
			trusted, excluded := fixtureSigner(t), fixtureECDSASigner(t)
			var offered xssh.Signer = excluded
			if marker != "" {
				offered = fixtureHostCertificate(t, fixtureECDSASigner(t), excluded, "127.0.0.1", false)
			}
			host := fixtureSSH(t, manager, offered, trusted)
			address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
			// The wildcard matches, but the explicit negation must win. If
			// this line influences negotiation, the untrusted key/cert wins.
			pattern := "!" + knownhosts.Normalize(address) + ",[*]:2222"
			original := marker + knownhosts.Line([]string{pattern}, excluded.PublicKey()) + "\n" + knownhosts.Line([]string{address}, trusted.PublicKey()) + "\n"
			if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := manager.Dial(host)
			if err != nil {
				t.Fatalf("negated trust displaced the valid trusted key: %v", err)
			}
			_ = client.Close()
			got, err := os.ReadFile(manager.knownHosts)
			if err != nil || string(got) != original {
				t.Fatal("negotiation changed negated trust entries")
			}
		})
	}
}

func TestGoSSHRejectsCertificatesOutsideAuthorityPrincipalOrValidity(t *testing.T) {
	for _, scenario := range []string{"different-authority", "negated-authority", "wrong-principal", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			manager := fixtureManager(t)
			authority, signerAuthority := fixtureSigner(t), fixtureSigner(t)
			if scenario != "different-authority" {
				signerAuthority = authority
			}
			principal := "127.0.0.1"
			if scenario == "wrong-principal" {
				principal = "other.invalid"
			}
			certificate := fixtureHostCertificate(t, fixtureSigner(t), signerAuthority, principal, scenario == "expired")
			host := fixtureSSH(t, manager, certificate)
			address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
			pattern := address
			if scenario == "negated-authority" {
				pattern = "!" + knownhosts.Normalize(address) + ",[*]:2222"
			}
			original := "@cert-authority\t" + knownhosts.Line([]string{pattern}, authority.PublicKey()) + "\n"
			if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := manager.Dial(host)
			if client != nil {
				_ = client.Close()
			}
			if err == nil {
				t.Fatalf("accepted host certificate outside %s boundary", scenario)
			}
			got, err := os.ReadFile(manager.knownHosts)
			if err != nil || string(got) != original {
				t.Fatal("invalid certificate modified known_hosts")
			}
		})
	}
}
