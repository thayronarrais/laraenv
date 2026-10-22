package vhost

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lara4ever/internal/paths"
)

// A newly created CA must name its origin: two portable copies (or a
// portable copy alongside an installed one) on the same machine used to
// produce two certs both called "Lara4Ever Local CA", indistinguishable in
// certmgr.msc. caCommonName is the pure function behind that — no
// filesystem, no paths.Root() — so this is a straight input/output check.
func TestCACommonNameContainsInstallRootAndNotTheBareConstant(t *testing.T) {
	root := `D:\My Dev\LaraEnv`
	got := caCommonName(root)

	if !strings.Contains(got, root) {
		t.Fatalf("caCommonName(%q) = %q, want it to contain the install root", root, got)
	}
	if got == caCommon {
		t.Fatalf("caCommonName(%q) = %q, want it distinct from the shared legacy constant %q", root, got, caCommon)
	}
}

// RFC 5280's ub-common-name is 64 characters. "Lara4Ever Local CA (" + root
// + ")" overruns it for any root longer than ~43 characters, and Go/Windows
// both tolerate that — but a stricter validator elsewhere might not. The
// truncated name must stay within budget and keep the *tail* of the root
// (its distinguishing part), not the head (the drive letter every install
// under that drive shares).
func TestCACommonNameTruncatesLongInstallRootKeepingTheTail(t *testing.T) {
	root := `D:\Users\developer\really\deeply\nested\portable\install\folder\LaraEnv`
	got := caCommonName(root)

	if n := len([]rune(got)); n > caCommonNameMaxLen {
		t.Fatalf("caCommonName(%q) = %q (%d runes), want <= %d", root, got, n, caCommonNameMaxLen)
	}
	if !strings.HasSuffix(got, `LaraEnv)`) {
		t.Fatalf("caCommonName(%q) = %q, want it to keep the distinguishing tail of the root", root, got)
	}
	if strings.Contains(got, `D:\Users`) {
		t.Fatalf("caCommonName(%q) = %q, want the head of the root dropped when truncating, not kept", root, got)
	}
}

// A short root must not be truncated at all — only the over-budget case
// should ever lose characters.
func TestCACommonNameLeavesAShortInstallRootWhole(t *testing.T) {
	root := `C:\laraenv`
	got := caCommonName(root)
	want := caCommon + " (" + root + ")"
	if got != want {
		t.Fatalf("caCommonName(%q) = %q, want %q (untruncated)", root, got, want)
	}
}

// ensureRootCA's creation path (no CA files on disk yet) must use
// caCommonName(paths.Root()), not the bare caCommon constant — otherwise two
// installs on one machine are back to being indistinguishable.
func TestEnsureRootCACreatesWithInstallRootName(t *testing.T) {
	dir := t.TempDir()

	cert, _, err := ensureRootCA(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cert.Subject.CommonName == caCommon {
		t.Fatalf("newly created CA common name = %q, want it to embed the install root, not the bare constant", cert.Subject.CommonName)
	}
	// Long portable roots use the bounded, tail-preserving name covered above.
	if want := caCommonName(paths.Root()); cert.Subject.CommonName != want {
		t.Fatalf("newly created CA common name = %q, want the install root name %q", cert.Subject.CommonName, want)
	}
}

// This is the regression that would hurt every existing user: ensureRootCA
// must load a CA already on disk exactly as-is — same key, same cert bytes,
// same (old) common name — never regenerate it, even though its common name
// predates caCommonName and is the bare "Lara4Ever Local CA".
func TestEnsureRootCALoadsExistingCAUnchanged(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)
	writeLegacyCA(t, dir)

	certBefore, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	cert, key, err := ensureRootCA(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cert.Subject.CommonName != caCommonLegacy {
		t.Fatalf("loaded CA common name = %q, want unchanged legacy %q — an existing install must not be renamed or regenerated", cert.Subject.CommonName, caCommonLegacy)
	}
	if key == nil || key.N == nil {
		t.Fatal("loaded CA key is nil")
	}

	certAfter, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(certBefore, certAfter) {
		t.Fatal("CA cert file on disk changed — ensureRootCA regenerated instead of loading")
	}
	if !bytes.Equal(keyBefore, keyAfter) {
		t.Fatal("CA key file on disk changed — ensureRootCA regenerated instead of loading")
	}
}

// A missing/invalid half of an existing CA is a recovery problem, never
// permission to replace the trusted certificate with a different identity.
func TestEnsureRootCARefusesToRotatePartialOrCorruptMaterial(t *testing.T) {
	for _, state := range []string{"missing-key", "corrupt-key", "missing-cert", "mismatched-key"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			writeLegacyCA(t, dir)
			certPath, keyPath := filepath.Join(dir, caCertFile), filepath.Join(dir, caKeyFile)
			switch state {
			case "missing-key":
				if err := os.Remove(keyPath); err != nil {
					t.Fatal(err)
				}
			case "corrupt-key":
				if err := os.WriteFile(keyPath, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-cert":
				if err := os.Remove(certPath); err != nil {
					t.Fatal(err)
				}
			case "mismatched-key":
				other := t.TempDir()
				writeLegacyCA(t, other)
				key, err := os.ReadFile(filepath.Join(other, caKeyFile))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(keyPath, key, 0600); err != nil {
					t.Fatal(err)
				}
			}
			certBefore, _ := os.ReadFile(certPath)
			keyBefore, _ := os.ReadFile(keyPath)
			if _, _, err := ensureRootCA(dir); err == nil {
				t.Fatal("invalid existing CA was silently accepted or regenerated")
			}
			certAfter, _ := os.ReadFile(certPath)
			keyAfter, _ := os.ReadFile(keyPath)
			if !bytes.Equal(certBefore, certAfter) || !bytes.Equal(keyBefore, keyAfter) {
				t.Fatal("existing CA identity/material was changed")
			}
		})
	}
}

// autoTrust=false must not shell out to certutil at all, and must still
// produce a usable host cert and key signed by the CA.
func TestEnsureSelfSignedCertAutoTrustFalseDoesNotShellOut(t *testing.T) {
	dir := t.TempDir()
	logPath := stubCertutil(t, 0)

	certPath, keyPath, err := EnsureSelfSignedCert(dir, "example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	if log := certutilLog(t, logPath); log != "" {
		t.Fatalf("EnsureSelfSignedCert(autoTrust=false) shelled out to certutil: %s", log)
	}

	assertUsableHostCert(t, dir, certPath, keyPath, "example.test")
}

// autoTrust=true keeps today's automatic behaviour: it does call out to
// install the CA into the trust store (best-effort — this only checks that
// certutil was invoked with -addstore, never that it succeeded on this
// machine).
func TestEnsureSelfSignedCertAutoTrustTrueShellsOut(t *testing.T) {
	dir := t.TempDir()
	logPath := stubCertutil(t, 0)

	certPath, keyPath, err := EnsureSelfSignedCert(dir, "example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	if log := certutilLog(t, logPath); !strings.Contains(log, "-addstore") {
		t.Fatalf("EnsureSelfSignedCert(autoTrust=true) did not shell out to certutil -addstore; log: %q", log)
	}

	assertUsableHostCert(t, dir, certPath, keyPath, "example.test")
}

// The whole point of making LocalCAStatus load-only: opening a Settings
// screen must never create a CA (a 4096-bit private key written to disk,
// possibly on removable media, for a user who was only curious) and must
// never shell out to certutil -addstore — a status read must not be able to
// install anything.
func TestLocalCAStatusDoesNotCreateACAOrInstallAnything(t *testing.T) {
	dir := t.TempDir()
	logPath := stubCertutil(t, 0)

	status, err := LocalCAStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatal("LocalCAStatus reported Exists=true for an empty certsDir")
	}
	if _, err := os.Stat(filepath.Join(dir, caCertFile)); err == nil {
		t.Fatal("LocalCAStatus created a CA cert file as a side effect")
	}
	if _, err := os.Stat(filepath.Join(dir, caKeyFile)); err == nil {
		t.Fatal("LocalCAStatus created a CA key file as a side effect")
	}
	if log := certutilLog(t, logPath); log != "" {
		t.Fatalf("LocalCAStatus shelled out to certutil on an empty certsDir: %s", log)
	}
}

// LocalCAStatus must report this copy's portable/installed kind even before
// any CA exists — the Projects page needs it (alongside Exists/Trusted) to
// decide whether enabling SSL should offer the in-place Trust prompt, and
// that decision can't wait for a CA to have been created first.
func TestLocalCAStatusReportsPortableRegardlessOfWhetherACAExists(t *testing.T) {
	dir := t.TempDir()

	status, err := LocalCAStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatal("expected no CA yet in a fresh certsDir")
	}
	if status.Portable != paths.IsPortable() {
		t.Fatalf("LocalCAStatus(no CA).Portable = %v, want %v (paths.IsPortable())", status.Portable, paths.IsPortable())
	}

	if _, _, err := ensureRootCA(dir); err != nil {
		t.Fatal(err)
	}
	status, err = LocalCAStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status.Portable != paths.IsPortable() {
		t.Fatalf("LocalCAStatus(CA exists).Portable = %v, want %v (paths.IsPortable())", status.Portable, paths.IsPortable())
	}
}

// Once a CA does exist, LocalCAStatus must report it (Exists, CommonName,
// Thumbprint) and may check trust (certutil -store — see
// TestCATrustedUsesPresenceOnlyCheck), but must never call -addstore: a
// status read is not the place anything gets installed into the trust
// store, no matter how many times it's called.
func TestLocalCAStatusReportsExistingCAWithoutInvokingAddStore(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := ensureRootCA(dir); err != nil { // creates, but not via certutil
		t.Fatal(err)
	}
	logPath := stubCertutil(t, 0)

	status, err := LocalCAStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("LocalCAStatus reported Exists=false for a CA that's on disk")
	}
	if status.Thumbprint == "" {
		t.Fatal("LocalCAStatus returned an empty thumbprint for an existing CA")
	}
	if log := certutilLog(t, logPath); strings.Contains(log, "-addstore") {
		t.Fatalf("LocalCAStatus invoked certutil -addstore — a status read must never install anything: %s", log)
	}
}

// The behaviour this whole change exists for: TrustLocalCA is the only path
// that may install into the trust store, and when that install fails, the
// user — who explicitly clicked Trust — must get the real error back, not
// a silently swallowed best-effort failure.
func TestTrustLocalCAReturnsErrorWhenCertutilFails(t *testing.T) {
	dir := t.TempDir()
	stubCertutil(t, 1) // simulates certutil -addstore failing (not elevated, policy, etc)

	err := TrustLocalCA(dir)
	if err == nil {
		t.Fatal("TrustLocalCA returned nil error when certutil failed — the user asked and must be told")
	}
}

func TestTrustLocalCASucceedsAndInvokesAddStore(t *testing.T) {
	dir := t.TempDir()
	logPath := stubCertutil(t, 0)

	if err := TrustLocalCA(dir); err != nil {
		t.Fatal(err)
	}
	if log := certutilLog(t, logPath); !strings.Contains(log, "-addstore") {
		t.Fatalf("TrustLocalCA did not invoke certutil -addstore; log: %q", log)
	}
}

// caTrusted must distinguish a real, checked "not trusted" (certutil ran
// and reported absence via its exit code) from a check that could not run
// at all (certutil unresolvable). Folding the second case into "not
// trusted" would tell the user to click a button that can't fix a check
// that never happened.
func TestCATrustedDistinguishesNotTrustedFromACheckThatDidNotRun(t *testing.T) {
	stubCertutil(t, 1) // certutil ran, reported "not found"
	trusted, err := caTrusted("DEADBEEF")
	if err != nil {
		t.Fatalf("caTrusted returned an error for a checked absence: %v", err)
	}
	if trusted {
		t.Fatal("caTrusted reported trusted=true for a stub that exits non-zero")
	}
}

func TestCATrustedReportsAnErrorWhenCertutilCannotBeFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty: certutil is not resolvable at all

	_, err := caTrusted("DEADBEEF")
	if err == nil {
		t.Fatal("caTrusted returned no error when certutil could not be found — this must not read as a checked 'not trusted'")
	}
}

// -store (presence), not -verifystore (chain validation): confirms the
// stub actually gets invoked with the flag this package now depends on, so
// a future edit that reverts to -verifystore is caught here rather than
// only in behaviour nobody's asserting on.
func TestCATrustedUsesPresenceOnlyCheck(t *testing.T) {
	logPath := stubCertutil(t, 0)
	if _, err := caTrusted("DEADBEEF"); err != nil {
		t.Fatal(err)
	}
	log := certutilLog(t, logPath)
	if !strings.Contains(log, "-store") {
		t.Fatalf("caTrusted did not invoke certutil -store; log: %q", log)
	}
	if strings.Contains(log, "-verifystore") {
		t.Fatalf("caTrusted used -verifystore (chain validation) instead of -store (presence): %q", log)
	}
}

// assertUsableHostCert confirms the host cert/key EnsureSelfSignedCert wrote
// actually work: the cert parses, is signed by the CA on disk, and its key
// parses too.
func assertUsableHostCert(t *testing.T, certsDir, certPath, keyPath, host string) {
	t.Helper()

	// EnsureSelfSignedCert returns forward-slash paths; the files are real
	// filesystem paths regardless of separator on Windows.
	if certPath == "" || keyPath == "" {
		t.Fatal("EnsureSelfSignedCert returned empty paths")
	}

	caCert, _, err := ensureRootCA(certsDir)
	if err != nil {
		t.Fatal(err)
	}
	hostCertFile := filepath.Join(certsDir, host+".crt")
	if !signedByCA(hostCertFile, caCert) {
		t.Fatalf("host cert %s is not signed by the local CA", hostCertFile)
	}

	keyBytes, err := os.ReadFile(filepath.Join(certsDir, host+".key"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		t.Fatal("host key file did not contain a PEM block")
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		t.Fatalf("host key does not parse: %v", err)
	}
}

// writeLegacyCA drops a CA on disk with the pre-caCommonName common name,
// simulating what every real install had before this change.
func writeLegacyCA(t *testing.T, dir string) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048) // smaller than production's 4096: test speed only
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonLegacy, Organization: []string{"Lara4Ever"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePEM(filepath.Join(dir, caCertFile), "CERTIFICATE", der, 0644); err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePEM(filepath.Join(dir, caKeyFile), "PRIVATE KEY", keyBytes, 0600); err != nil {
		t.Fatal(err)
	}
}

// stubCertutil puts a fake certutil.bat at the front of PATH that appends
// each invocation's arguments to the returned log file and exits with
// exitCode. It never touches the real Windows trust store, so tests can
// prove exactly what was — and wasn't — shelled out to, without depending
// on (or mutating) this machine's actual certificate state.
func stubCertutil(t *testing.T, exitCode int) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "certutil-invocations.log")
	script := "@echo off\r\necho %* >> \"" + logPath + "\"\r\nexit /b " + strconv.Itoa(exitCode) + "\r\n"
	if err := os.WriteFile(filepath.Join(binDir, "certutil.bat"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// certutilLog reads back what stubCertutil recorded. An empty string means
// certutil was never invoked at all — the log file doesn't exist yet.
func certutilLog(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
