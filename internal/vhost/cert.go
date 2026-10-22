package vhost

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"lara4ever/internal/i18n"
	"lara4ever/internal/paths"
)

var rootCAMu sync.Mutex

// LocalCACertPath is the public certificate only; callers must never publish
// or pass the neighbouring root private key as runtime trust material.
func LocalCACertPath(certsDir string) string { return filepath.Join(certsDir, caCertFile) }

const (
	caCertFile = "lara4ever-rootCA.crt"
	caKeyFile  = "lara4ever-rootCA.key"
	// caCommonLegacy is the common name carried by every CA created before
	// this scheme existed, under the product's former spelling. Nothing
	// compares against it — ensureRootCA returns whatever is on disk, so a
	// CA named this way keeps working untouched. It is recorded here so the
	// name is explicable when it turns up in certmgr.msc.
	caCommonLegacy = "Lara4Ever Local CA"

	// caCommon prefixes a newly created CA's common name. This string is
	// user-visible in the Windows certificate store, so it carries the
	// product's actual name; the old spelling survives only in CAs already
	// on disk. The full name adds the install root via caCommonName, so two
	// copies on one machine are distinguishable.
	caCommon = "LaraEnv Local CA"
)

// EnsureSelfSignedCert creates (if missing) a local Root CA and a host cert
// signed by it. Returns forward-slash paths suitable for nginx.
//
// autoTrust controls whether the CA is (best-effort) installed into
// Windows' Trusted Root store. The caller decides — not this package —
// because that decision depends on the install kind: an MSI install trusts
// automatically (today's behaviour, unchanged); a portable copy must not
// silently plant a root certificate on a machine it doesn't own, so trust
// there is left to an explicit call to TrustLocalCA.
func EnsureSelfSignedCert(certsDir, host string, autoTrust bool) (certPath, keyPath string, err error) {
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return "", "", err
	}
	caCert, caKey, err := ensureRootCA(certsDir)
	if err != nil {
		return "", "", i18n.Wrapf(err, "errors.vhost.rootCAFailed", nil)
	}
	if autoTrust {
		// Best-effort: install CA into Windows Trust Store. Idempotent —
		// certutil silently no-ops if it's already there.
		_ = trustRootCA(filepath.Join(certsDir, caCertFile))
	}

	certPath = filepath.Join(certsDir, host+".crt")
	keyPath = filepath.Join(certsDir, host+".key")

	if signedByCA(certPath, caCert) {
		if _, err2 := os.Stat(keyPath); err2 == nil {
			return toFwd(certPath), toFwd(keyPath), nil
		}
	}

	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"LaraEnv"}},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host, "*." + host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &hostKey.PublicKey, caKey)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0644); err != nil {
		return "", "", err
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(hostKey)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", keyBytes, 0600); err != nil {
		return "", "", err
	}
	return toFwd(certPath), toFwd(keyPath), nil
}

// RemoveCert deletes the cert + key for a host (no error if missing).
func RemoveCert(certsDir, host string) {
	_ = os.Remove(filepath.Join(certsDir, host+".crt"))
	_ = os.Remove(filepath.Join(certsDir, host+".key"))
}

// ensureRootCA loads or creates the local Root CA used to sign host certs.
// Creating is the expensive, side-effecting half (a 4096-bit key, written to
// disk) — callers that must not have that side effect (LocalCAStatus) use
// loadRootCA directly instead.
func ensureRootCA(certsDir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	rootCAMu.Lock()
	defer rootCAMu.Unlock()
	if cert, key, exists := loadRootCA(certsDir); exists {
		return cert, key, nil
	}

	certPath := filepath.Join(certsDir, caCertFile)
	keyPath := filepath.Join(certsDir, caKeyFile)
	for _, path := range []string{certPath, keyPath} {
		if _, err := os.Lstat(path); err == nil {
			return nil, nil, errors.New("existing local CA is incomplete, invalid, or has a mismatched key; restore the matching certificate and key before retrying; no files were replaced")
		} else if !os.IsNotExist(err) {
			return nil, nil, err
		}
	}

	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName(paths.Root()), Organization: []string{"LaraEnv"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0644); err != nil {
		return nil, nil, err
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		return nil, nil, err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", keyBytes, 0600); err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, caKey, nil
}

// loadRootCA reads the local Root CA from certsDir without ever creating
// one. exists is false — with a nil cert/key and no error — when the files
// are simply not there yet, or don't parse; that is the ordinary state
// before any project has turned SSL on, not a failure. Never touches the
// filesystem beyond reading.
func loadRootCA(certsDir string) (cert *x509.Certificate, key *rsa.PrivateKey, exists bool) {
	certPath := filepath.Join(certsDir, caCertFile)
	keyPath := filepath.Join(certsDir, caKeyFile)

	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, false
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, false
	}
	cb, _ := pem.Decode(certBytes)
	kb, _ := pem.Decode(keyBytes)
	if cb == nil || kb == nil {
		return nil, nil, false
	}
	parsedCert, err1 := x509.ParseCertificate(cb.Bytes)
	k, err2 := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err1 != nil || err2 != nil {
		return nil, nil, false
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, false
	}
	publicKey, ok := parsedCert.PublicKey.(*rsa.PublicKey)
	if !ok || publicKey.N.Cmp(rsaKey.N) != 0 || publicKey.E != rsaKey.E || !parsedCert.IsCA || parsedCert.CheckSignatureFrom(parsedCert) != nil {
		return nil, nil, false
	}
	return parsedCert, rsaKey, true
}

// trustRootCA installs the CA into Windows' Trusted Root store. Requires admin
// privileges (the LaraEnv process already runs elevated for the hosts file).
// certutil -addstore is idempotent.
func trustRootCA(certPath string) error {
	cmd := exec.Command("certutil", "-addstore", "-f", "ROOT", certPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// caCommonNameMaxLen is RFC 5280's ub-common-name bound. Go's x509 doesn't
// enforce it and Windows CryptoAPI tolerates going over, but a strict
// validator elsewhere might not, so a newly created CA stays under it.
const caCommonNameMaxLen = 64

// caCommonName is the common name a newly created local CA gets: the shared
// "LaraEnv Local CA" prefix plus the install root it was created under, so a
// portable copy's CA is distinguishable — in certmgr.msc, and from any other
// copy's CA on the same machine — from every other one. Pure so it's
// testable without touching paths.Root() or the filesystem; ensureRootCA is
// the only caller and always passes paths.Root().
//
// When the full name would overrun caCommonNameMaxLen, the root is
// shortened to its tail rather than its head: the tail is the distinguishing
// part of a path (".../My Dev/LaraEnv" vs a drive letter every install
// under the same root shares), and an ellipsis marks the cut.
func caCommonName(installRoot string) string {
	full := fmt.Sprintf("%s (%s)", caCommon, installRoot)
	if len([]rune(full)) <= caCommonNameMaxLen {
		return full
	}
	fixed := len([]rune(caCommon)) + len([]rune(" (…)"))
	budget := caCommonNameMaxLen - fixed
	if budget < 0 {
		budget = 0
	}
	rootRunes := []rune(installRoot)
	if budget > len(rootRunes) {
		budget = len(rootRunes)
	}
	tail := string(rootRunes[len(rootRunes)-budget:])
	return fmt.Sprintf("%s (…%s)", caCommon, tail)
}

// CAStatus is what the UI needs to render the local CA's identity and trust
// state — see (*App).LocalCAStatus.
type CAStatus struct {
	// Exists is false when no CA has been created yet on this copy (no
	// project has ever turned SSL on). Every other identity/trust field
	// below is the zero value in that case — Portable is the exception: it
	// describes this copy, not the CA, so it is always populated.
	Exists     bool   `json:"exists"`
	CommonName string `json:"commonName"`
	Thumbprint string `json:"thumbprint"` // SHA-1, hex, upper-case — the form certmgr.msc/certutil show
	Trusted    bool   `json:"trusted"`
	// CheckError is set when whether the CA is trusted could not be
	// determined at all (e.g. certutil isn't resolvable) — distinct from a
	// checked, genuine "not trusted". Trusted is meaningless (false) when
	// this is set; the UI must not report it as "not trusted" the user can
	// act on, since the check never actually ran.
	CheckError string `json:"checkError,omitempty"`
	// Portable reports whether this copy is a portable distribution rather
	// than an MSI install — see paths.IsPortable. The UI needs this typed
	// fact (not inferred from, say, the absence of auto-trust) to decide
	// whether enabling SSL should offer the in-place Trust prompt at all:
	// an installed copy trusts its CA automatically and must never see it.
	Portable bool `json:"portable"`
}

// LocalCAStatus reports the local root CA's identity and whether Windows'
// Trusted Root store currently has it. Load-only: it never creates a CA and
// never installs anything into the trust store (that is TrustLocalCA's
// job), so rendering a Settings screen — including on a portable copy that
// has never turned SSL on — has no side effect of its own. Before any CA
// exists, this returns CAStatus{Exists: false, Portable: paths.IsPortable()}
// rather than an error.
func LocalCAStatus(certsDir string) (CAStatus, error) {
	portable := paths.IsPortable()
	cert, _, exists := loadRootCA(certsDir)
	if !exists {
		return CAStatus{Portable: portable}, nil
	}
	thumbprint := sha1Thumbprint(cert)
	status := CAStatus{Exists: true, CommonName: cert.Subject.CommonName, Thumbprint: thumbprint, Portable: portable}
	trusted, err := caTrusted(thumbprint)
	if err != nil {
		status.CheckError = err.Error()
		return status, nil
	}
	status.Trusted = trusted
	return status, nil
}

// TrustLocalCA installs the local root CA into Windows' Trusted Root store,
// creating the CA first if this copy has never had one (e.g. the user opens
// Settings and clicks Trust before any project has turned SSL on). Unlike
// the best-effort install EnsureSelfSignedCert performs when autoTrust is
// true, this is called only in response to an explicit user action (the
// "Trust" button in Settings), so its error is real and must reach the
// caller — the user asked, and deserves to know if it failed.
func TrustLocalCA(certsDir string) error {
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return err
	}
	if _, _, err := ensureRootCA(certsDir); err != nil {
		return i18n.Wrapf(err, "errors.vhost.rootCAFailed", nil)
	}
	if err := trustRootCA(filepath.Join(certsDir, caCertFile)); err != nil {
		return i18n.Wrapf(err, "errors.vhost.installRootCertFailed", nil)
	}
	return nil
}

// caTrusted reports whether Windows' ROOT store currently holds a
// certificate with this thumbprint, and whether that could be determined at
// all.
//
// -store (not -verifystore) is deliberate: -verifystore also validates the
// chain (expiry, machine policy, and so on), so a CA that IS installed but
// fails validation would read as absent — the user clicks Trust, -addstore
// (idempotently) succeeds, and the status still says "Not trusted": a
// button that visibly does nothing. -store only checks presence, which is
// what trustRootCA actually changes.
//
// A non-nil error means the check itself could not be run (certutil not on
// PATH, etc) — the caller must not fold that into "not trusted": that would
// tell the user to click a button that cannot fix a check that never ran.
func caTrusted(thumbprint string) (bool, error) {
	cmd := exec.Command("certutil", "-store", "ROOT", thumbprint)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// certutil ran and reported "not found" via its exit code — a real,
		// checked "not trusted", not a failed check.
		return false, nil
	}
	return false, i18n.Wrapf(err, "errors.vhost.checkLocalCATrustFailed", nil)
}

// sha1Thumbprint is the certificate fingerprint certmgr.msc and certutil
// call "Thumbprint" — SHA-1 over the raw DER, hex-encoded, upper-case.
func sha1Thumbprint(cert *x509.Certificate) string {
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

// signedByCA returns true if certPath exists, parses, and was issued by ca.
// Used to skip regen when the existing host cert is already trusted, and to
// force regen for old self-signed certs from a previous Lara4Ever version.
func signedByCA(certPath string, ca *x509.Certificate) bool {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	return cert.CheckSignatureFrom(ca) == nil
}

func toFwd(p string) string { return strings.ReplaceAll(p, `\`, `/`) }
