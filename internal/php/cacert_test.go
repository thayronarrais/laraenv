package php

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureCaCertPreservesCustomBundleAndIni(t *testing.T) {
	denyExternalCAFetch(t)
	dir := t.TempDir()
	bundle := filepath.Join(dir, "extras", "ssl", "cacert.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0755); err != nil {
		t.Fatal(err)
	}
	custom, _, _ := testCA(t, "custom")
	if err := os.WriteFile(bundle, custom, 0644); err != nil {
		t.Fatal(err)
	}
	ini := "curl.cainfo = \"D:/company/roots.pem\"\r\nopenssl.cafile = \"D:/company/other.pem\"\r\n"
	if err := os.WriteFile(filepath.Join(dir, "php.ini"), []byte(ini), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCaCert(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(bundle)
	if !bytes.Equal(got, custom) {
		t.Fatal("existing certificate bundle was replaced")
	}
	got, _ = os.ReadFile(filepath.Join(dir, "php.ini"))
	if string(got) != ini {
		t.Fatal("custom php.ini CA paths were overwritten")
	}
}

func TestPatchCaCertIniPreservesLaterCustomDirective(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "php.ini")
	before := "curl.cainfo = \"\"\ncurl.cainfo = \"D:/company.pem\"\nopenssl.cafile = \"D:/other.pem\"\n"
	if err := os.WriteFile(iniPath, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	if err := patchCaCertIni(dir, filepath.Join(dir, "extras/ssl/cacert.pem")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(iniPath)
	if string(after) != before {
		t.Fatal("a later active custom directive was overwritten")
	}
}

func TestRebaseCaCertIniMovesOnlyTheKnownManagedBundle(t *testing.T) {
	dir := t.TempDir()
	previous := `D:\old\php\8.4`
	ini := "curl.cainfo = \"D:/old/php/8.4/extras/ssl/cacert.pem\" ; managed before moving\nopenssl.cafile = \"D:/company.pem\"\n"
	path := filepath.Join(dir, "php.ini")
	if err := os.WriteFile(path, []byte(ini), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RebaseCaCertIni(dir, previous); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), strings.ReplaceAll(filepath.Join(dir, "extras/ssl/cacert.pem"), `\`, `/`)) {
		t.Fatal("known managed bundle was not rebased")
	}
	if !strings.Contains(string(got), `openssl.cafile = "D:/company.pem"`) {
		t.Fatal("custom trust choice was changed during relocation")
	}
	if !strings.Contains(string(got), "; managed before moving") {
		t.Fatal("inline annotation was lost")
	}
}

// Removing the local root append makes the real TLS handshake below fail;
// replacing the bundle drops the independently generated custom root.
func TestEnsureCaCertTrustsLocalTLSAndIsIdempotent(t *testing.T) {
	denyExternalCAFetch(t)
	dir := t.TempDir()
	bundle := filepath.Join(dir, "extras", "ssl", "cacert.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0755); err != nil {
		t.Fatal(err)
	}
	custom, _, _ := testCA(t, "custom")
	local, ca, caKey := testCA(t, "local")
	if err := os.WriteFile(bundle, custom, 0644); err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "local.crt")
	if err := os.WriteFile(caPath, local, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "php.ini"), []byte(";curl.cainfo =\n;openssl.cafile =\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCaCert(dir, caPath); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(bundle)
	if !bytes.HasPrefix(first, custom) {
		t.Fatal("existing trust roots were not preserved")
	}
	if bytes.Contains(first, []byte("PRIVATE KEY")) {
		t.Fatal("private key leaked into public trust bundle")
	}
	if err := EnsureCaCert(dir, caPath); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(bundle)
	if !bytes.Equal(first, again) {
		t.Fatal("repeated setup duplicated the local CA")
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("verified")) }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: leafKey}}}
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(first)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}, Timeout: 5 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("local HTTPS is not trusted by PHP bundle: %v", err)
	}
	response.Body.Close()
	ini, _ := os.ReadFile(filepath.Join(dir, "php.ini"))
	if !strings.Contains(string(ini), strings.ReplaceAll(bundle, `\`, `/`)) {
		t.Fatal("default PHP CA paths were not configured")
	}
}

func TestEnsureCaCertRejectsMalformedLocalCAWithoutChangingBundle(t *testing.T) {
	denyExternalCAFetch(t)
	dir := t.TempDir()
	bundle := filepath.Join(dir, "extras", "ssl", "cacert.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0755); err != nil {
		t.Fatal(err)
	}
	custom, _, _ := testCA(t, "custom")
	if err := os.WriteFile(bundle, custom, 0644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.crt")
	if err := os.WriteFile(bad, []byte("-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCaCert(dir, bad); err == nil {
		t.Fatal("invalid public CA was accepted")
	}
	got, _ := os.ReadFile(bundle)
	if !bytes.Equal(got, custom) {
		t.Fatal("invalid input changed the existing bundle")
	}
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func denyExternalCAFetch(t *testing.T) {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = testRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected external CA download") })
	t.Cleanup(func() { http.DefaultTransport = original })
}

func testCA(t *testing.T, name string) ([]byte, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), parsed, key
}
