package localtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentPreservesCustomSettingsCaseInsensitively(t *testing.T) {
	ca, p := tlsFixture(t)
	env := []string{"Path=tools", "node_extra_ca_certs=D:/company.pem", "Vite_Dev_Server_Key=custom.key", "VITE_DEV_SERVER_CERT=custom.crt"}
	got, err := Environment(env, ca, &p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "\n") != strings.Join(env, "\n") {
		t.Fatalf("custom environment changed: %v", got)
	}
	if env[0] != "Path=tools" {
		t.Fatal("input environment was mutated")
	}
}

func TestEnvironmentAddsPublicCAAndCompatibleLaravelVitePaths(t *testing.T) {
	ca, p := tlsFixture(t)
	got, err := Environment([]string{"KEEP=yes"}, ca, &p)
	if err != nil {
		t.Fatal(err)
	}
	assertEnv(t, got, "NODE_EXTRA_CA_CERTS", ca)
	assertEnv(t, got, "VITE_DEV_SERVER_CERT", p.CertPath)
	assertEnv(t, got, "VITE_DEV_SERVER_KEY", p.KeyPath)
	assertEnv(t, got, "APP_URL", "https://shop.test")
	if strings.Contains(strings.Join(got, "\n"), "BEGIN") {
		t.Fatal("certificate or private-key bytes leaked into environment")
	}
	again, err := Environment(got, ca, &p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(again, "\n") != strings.Join(got, "\n") {
		t.Fatal("environment is not idempotent")
	}
}

func TestEnvironmentHonorsProjectViteEnvironmentAndPluginSupport(t *testing.T) {
	for _, scenario := range []string{"project-key", "project-empty-cert", "custom-mode-key", "custom-mode-host", "different-host", "unsupported-plugin", "no-plugin", "missing-cert", "unrelated-cert"} {
		t.Run(scenario, func(t *testing.T) {
			ca, p := tlsFixture(t)
			switch scenario {
			case "project-key":
				writeFixture(t, filepath.Join(p.Path, ".env.local"), "export VITE_DEV_SERVER_KEY='custom.key'\n")
			case "project-empty-cert":
				writeFixture(t, filepath.Join(p.Path, ".env.development"), "VITE_DEV_SERVER_CERT=\n")
			case "custom-mode-key":
				writeFixture(t, filepath.Join(p.Path, ".env.staging"), "VITE_DEV_SERVER_KEY=custom.key\n")
			case "custom-mode-host":
				writeFixture(t, filepath.Join(p.Path, ".env"), "APP_URL=https://shop.test\n")
				writeFixture(t, filepath.Join(p.Path, ".env.staging"), "APP_URL=https://other.test\n")
			case "different-host":
				writeFixture(t, filepath.Join(p.Path, ".env"), "APP_URL=https://other.test\n")
			case "unsupported-plugin":
				writeFixture(t, filepath.Join(p.Path, "node_modules/laravel-vite-plugin/dist/index.js"), "no environment TLS support")
			case "no-plugin":
				if err := os.RemoveAll(filepath.Join(p.Path, "node_modules")); err != nil {
					t.Fatal(err)
				}
			case "missing-cert":
				if err := os.Remove(p.CertPath); err != nil {
					t.Fatal(err)
				}
			case "unrelated-cert":
				_, other := tlsFixture(t)
				cert, err := os.ReadFile(other.CertPath)
				if err != nil {
					t.Fatal(err)
				}
				key, err := os.ReadFile(other.KeyPath)
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, p.CertPath, string(cert))
				writeFixture(t, p.KeyPath, string(key))
			}
			got, err := Environment(nil, ca, &p)
			if err != nil {
				t.Fatal(err)
			}
			assertEnv(t, got, "NODE_EXTRA_CA_CERTS", ca)
			if value, ok := envValue(got, "VITE_DEV_SERVER_KEY"); ok {
				t.Fatalf("Vite settings were overridden/guessed: %q", value)
			}
		})
	}
}

// This executes the real Node TLS client against a loopback-only TLS server.
// Node snapshots NODE_EXTRA_CA_CERTS at process startup, so testing only a Go
// CertPool would miss whether our emitted environment actually works in Node.
func TestEnvironmentTrustsRealNodeTLS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable")
	}
	ca, p := tlsFixture(t)
	pair, err := tls.LoadX509KeyPair(p.CertPath, p.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_ = conn.(*tls.Conn).Handshake()
		}
	}()
	var base []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "NODE_EXTRA_CA_CERTS") {
			base = append(base, entry)
		}
	}
	env, err := Environment(base, ca, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", `const tls=require('node:tls');const s=tls.connect({host:'127.0.0.1',port:Number(process.argv[1]),servername:'shop.test'},()=>{if(!s.authorized)process.exit(2);s.end();});s.on('error',e=>{console.error(e.code,e.message);process.exit(3)});setTimeout(()=>process.exit(4),5000).unref();`, strings.Split(listener.Addr().String(), ":")[1])
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Node did not trust local CA: %v %s", err, output)
	}
	<-done
}

func TestEnvironmentRejectsPrivateKeyInPublicCA(t *testing.T) {
	ca, p := tlsFixture(t)
	key, err := os.ReadFile(p.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := os.ReadFile(ca)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca, append(pub, key...), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Environment(nil, ca, nil); err == nil || strings.Contains(err.Error(), "BEGIN") {
		t.Fatal("private material must be rejected without exposing it")
	}
}

func tlsFixture(t *testing.T) (string, Project) {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test local CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"shop.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	caPath, certPath, keyPath := filepath.Join(dir, "root.crt"), filepath.Join(dir, "shop.crt"), filepath.Join(dir, "shop.key")
	writeFixture(t, caPath, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})))
	writeFixture(t, certPath, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})))
	writeFixture(t, keyPath, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})))
	project := filepath.Join(dir, "project")
	writeFixture(t, filepath.Join(project, "artisan"), "<?php")
	writeFixture(t, filepath.Join(project, "node_modules/laravel-vite-plugin/dist/index.js"), "env.VITE_DEV_SERVER_KEY; env.VITE_DEV_SERVER_CERT")
	return caPath, Project{Path: project, Hostname: "shop.test", CertPath: certPath, KeyPath: keyPath}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func assertEnv(t *testing.T, env []string, key, want string) {
	t.Helper()
	if got, ok := envValue(env, key); !ok || got != want {
		t.Fatalf("%s=%q (present=%v), want %q", key, got, ok, want)
	}
}
