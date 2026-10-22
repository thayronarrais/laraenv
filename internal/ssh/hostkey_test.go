package ssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"lara4ever/internal/config"
)

func fixtureSigner(t *testing.T) xssh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// This disposable in-memory transport implements only the SSH protocol needed
// by these tests. It never connects to a user's host or starts a real shell.
func fixtureSSH(t *testing.T, m *Manager, signers ...xssh.Signer) config.SSHHost {
	t.Helper()
	var mu sync.Mutex
	var clients []net.Conn
	var wg sync.WaitGroup
	m.dialTimeout = func(_ string, address string, _ time.Duration) (net.Conn, error) {
		client, raw := net.Pipe()
		conn := bufferedFixtureConn(raw)
		mu.Lock()
		clients = append(clients, conn)
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			cfg := &xssh.ServerConfig{NoClientAuth: true}
			for _, signer := range signers {
				cfg.AddHostKey(signer)
			}
			server, chans, reqs, err := xssh.NewServerConn(conn, cfg)
			if err != nil {
				return
			}
			defer server.Close()
			go xssh.DiscardRequests(reqs)
			for ch := range chans {
				if ch.ChannelType() != "session" {
					_ = ch.Reject(xssh.Prohibited, "fixture has no forwarding")
					continue
				}
				channel, requests, err := ch.Accept()
				if err != nil {
					return
				}
				go func() {
					defer channel.Close()
					for req := range requests {
						_ = req.Reply(req.Type == "pty-req" || req.Type == "shell", nil)
					}
				}()
			}
		}()
		return &fixtureClientConn{Conn: bufferedFixtureConn(client), remote: fixtureAddr(address)}, nil
	}
	t.Cleanup(func() {
		mu.Lock()
		for _, conn := range clients {
			_ = conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return config.SSHHost{Name: "fixture", Address: "127.0.0.1", Port: 2222, User: "fixture", AuthMode: "password", Password: "fixture-only"}
}

type fixtureAddr string

func (a fixtureAddr) Network() string { return "tcp" }
func (a fixtureAddr) String() string  { return string(a) }

type fixtureClientConn struct {
	net.Conn
	remote net.Addr
}

func (c *fixtureClientConn) RemoteAddr() net.Addr { return c.remote }

// A small bounded write queue mirrors a socket's kernel buffer. net.Pipe's
// unbuffered writes otherwise deadlock the SSH version exchange.
type fixtureBufferedConn struct {
	net.Conn
	writes chan []byte
	done   chan struct{}
	once   sync.Once
}

func bufferedFixtureConn(conn net.Conn) *fixtureBufferedConn {
	c := &fixtureBufferedConn{Conn: conn, writes: make(chan []byte, 64), done: make(chan struct{})}
	go func() {
		for {
			select {
			case p := <-c.writes:
				if _, err := conn.Write(p); err != nil {
					return
				}
			case <-c.done:
				return
			}
		}
	}()
	return c
}
func (c *fixtureBufferedConn) Write(p []byte) (int, error) {
	select {
	case <-c.done:
		return 0, net.ErrClosed
	default:
	}
	select {
	case c.writes <- append([]byte(nil), p...):
		return len(p), nil
	case <-c.done:
		return 0, net.ErrClosed
	}
}
func (c *fixtureBufferedConn) Close() error {
	var err error
	c.once.Do(func() { close(c.done); err = c.Conn.Close() })
	return err
}

func fixtureManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	return NewManager(dir, filepath.Join(dir, "known_hosts"), func(string) string { return dir })
}

func TestGoSSHNegotiatesAnExistingTrustedKeyOnEveryConnectionPath(t *testing.T) {
	for _, mode := range []string{"dial", "connect", "jump"} {
		t.Run(mode, func(t *testing.T) {
			manager := fixtureManager(t)
			trusted := fixtureSigner(t)
			private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			other, err := xssh.NewSignerFromKey(private)
			if err != nil {
				t.Fatal(err)
			}
			host := fixtureSSH(t, manager, other, trusted)
			address := net.JoinHostPort(host.Address, strconv.Itoa(host.Port))
			original := knownhosts.Line([]string{knownhosts.HashHostname(knownhosts.Normalize(address))}, trusted.PublicKey()) + "\n"
			if err := os.WriteFile(manager.knownHosts, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "connect":
				session, err := manager.Connect(host, 80, 24, nil)
				if err != nil {
					t.Fatalf("trusted key was offered but not negotiated: %v", err)
				}
				_ = session.Close()
			case "jump":
				client, err := manager.dialThroughProxy(host, time.Second)
				if err != nil {
					t.Fatalf("trusted jump key was offered but not negotiated: %v", err)
				}
				_ = client.Close()
			default:
				client, err := manager.Dial(host)
				if err != nil {
					t.Fatalf("trusted key was offered but not negotiated: %v", err)
				}
				_ = client.Close()
			}
			got, err := os.ReadFile(manager.knownHosts)
			if err != nil || string(got) != original {
				t.Fatal("negotiation changed existing trust")
			}
		})
	}
}

func TestGoSSHRejectsChangedHostKeyOnEveryConnectionPath(t *testing.T) {
	for _, mode := range []string{"dial", "connect", "jump"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureManager(t)
			h := fixtureSSH(t, m, fixtureSigner(t))
			address := net.JoinHostPort(h.Address, strconv.Itoa(h.Port))
			original := []byte(knownhosts.Line([]string{address}, fixtureSigner(t).PublicKey()) + "\n")
			if err := os.WriteFile(m.knownHosts, original, 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "connect":
				var session *Session
				session, err = m.Connect(h, 80, 24, nil)
				if session != nil {
					_ = session.Close()
				}
			case "jump":
				m.SetHostLookup(func(name string) (config.SSHHost, bool) { return h, name == "bastion" })
				target := h
				target.Name, target.Address, target.Port, target.ProxyJump = "target", "target.invalid", 22, "bastion"
				var client *xssh.Client
				client, err = m.Dial(target)
				if client != nil {
					_ = client.Close()
				}
			default:
				var client *xssh.Client
				client, err = m.Dial(h)
				if client != nil {
					_ = client.Close()
				}
			}
			var mismatch *knownhosts.KeyError
			if !errors.As(err, &mismatch) || len(mismatch.Want) == 0 {
				t.Fatalf("changed key must fail verification before auth/forwarding; error type %T", err)
			}
			got, readErr := os.ReadFile(m.knownHosts)
			if readErr != nil || string(got) != string(original) {
				t.Fatal("changed key replaced trusted host data")
			}
		})
	}
}

func TestGoSSHRemembersFirstSeenKeyAndRejectsLaterChange(t *testing.T) {
	m := fixtureManager(t)
	h := fixtureSSH(t, m, fixtureSigner(t))
	client, err := m.Dial(h)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	check, err := knownhosts.New(m.knownHosts)
	if err != nil {
		t.Fatalf("first connection did not persist known_hosts: %v", err)
	}
	addr := net.JoinHostPort(h.Address, strconv.Itoa(h.Port))
	remote := &net.TCPAddr{IP: net.ParseIP(h.Address), Port: h.Port}
	if err := check(addr, remote, fixtureSigner(t).PublicKey()); err == nil {
		t.Fatal("persisted trust accepted a different key")
	}
	client, err = m.Dial(h)
	if err != nil {
		t.Fatalf("matching key was rejected: %v", err)
	}
	_ = client.Close()
}

func TestAcceptNewSerializesConcurrentFirstConnections(t *testing.T) {
	for _, conflicting := range []bool{false, true} {
		t.Run(strconv.FormatBool(conflicting), func(t *testing.T) {
			m := fixtureManager(t)
			first, second := fixtureSigner(t).PublicKey(), fixtureSigner(t).PublicKey()
			start := make(chan struct{})
			results := make(chan error, 16)
			for i := 0; i < 16; i++ {
				key := first
				if conflicting && i%2 != 0 {
					key = second
				}
				// Different managers model detached processes using one trust file.
				other := NewManager(m.keysRoot, m.knownHosts, m.hostDirFunc)
				go func() {
					<-start
					results <- other.hostKeyCallback()("example.test:22", fixtureAddr("198.51.100.2:22"), key)
				}()
			}
			close(start)
			failed := 0
			for i := 0; i < 16; i++ {
				if err := <-results; err != nil {
					var mismatch *knownhosts.KeyError
					if !errors.As(err, &mismatch) {
						t.Fatal(err)
					}
					failed++
				}
			}
			wantFailed := 0
			if conflicting {
				wantFailed = 8
			}
			if failed != wantFailed {
				t.Fatalf("accepted conflicting first-seen keys: failures %d, want %d", failed, wantFailed)
			}
			data, err := os.ReadFile(m.knownHosts)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(strings.TrimSpace(string(data)), "\n") != 0 {
				t.Fatal("concurrent checks persisted duplicate or contradictory keys")
			}
		})
	}
}

func TestHostKeyAddressesAndExistingOpenSSHFormats(t *testing.T) {
	for _, address := range []string{"example.test:22", "example.test:2222", "[2001:db8::1]:22", "[2001:db8::1]:2222"} {
		t.Run(address, func(t *testing.T) {
			m := fixtureManager(t)
			key := fixtureSigner(t).PublicKey()
			check := m.hostKeyCallback()
			if err := check(address, procAddr{}, key); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(m.knownHosts)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(data), knownhosts.Normalize(address)+" ") {
				t.Fatal("host or port persisted in incompatible OpenSSH format")
			}
			// OpenSSH can hash the same identity; it must remain trusted.
			hashed := knownhosts.HashHostname(knownhosts.Normalize(address))
			if err := os.WriteFile(m.knownHosts, []byte(knownhosts.Line([]string{hashed}, key)+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := check(address, procAddr{}, key); err != nil {
				t.Fatal("hashed OpenSSH host was not trusted", err)
			}
		})
	}
}

func TestInvalidOrRevokedKnownHostsFailsClosed(t *testing.T) {
	for _, mode := range []string{"malformed", "revoked", "unwritable", "no-path", "address-injection"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureManager(t)
			key := fixtureSigner(t).PublicKey()
			original := []byte("invalid known_hosts data\n")
			address := "example.test:22"
			switch mode {
			case "revoked":
				original = []byte("@revoked " + knownhosts.Line([]string{address}, key) + "\n")
			case "unwritable":
				m.knownHosts = filepath.Join(m.keysRoot, "directory")
				if err := os.Mkdir(m.knownHosts, 0700); err != nil {
					t.Fatal(err)
				}
			case "no-path":
				m.knownHosts = ""
			case "address-injection":
				address = "example.test\nevil.test:22"
			}
			if mode == "malformed" || mode == "revoked" {
				if err := os.WriteFile(m.knownHosts, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.hostKeyCallback()(address, procAddr{}, key); err == nil {
				t.Fatal("unsafe/unpersistable trust must not be accepted")
			}
			if mode == "malformed" || mode == "revoked" {
				got, err := os.ReadFile(m.knownHosts)
				if err != nil || string(got) != string(original) {
					t.Fatal("failed trust verification modified known_hosts")
				}
			}
		})
	}
}

func TestDirectIPv6UsesIPv6TransportAndNormalizesBrackets(t *testing.T) {
	for _, host := range []string{"2001:db8::1", "[2001:db8::1]"} {
		t.Run(host, func(t *testing.T) {
			m := fixtureManager(t)
			m.dialTimeout = func(network, address string, _ time.Duration) (net.Conn, error) {
				if network != "tcp6" || address != "[2001:db8::1]:2222" {
					t.Fatalf("IPv6 dial = %s %s", network, address)
				}
				return nil, errors.New("fixture transport completed")
			}
			_, _ = m.proxyDial(config.SSHHost{Address: host, Port: 2222}, time.Second)
		})
	}
}

func TestSavedIPv6ProxyJumpUsesOpenSSHAddressForm(t *testing.T) {
	m := fixtureManager(t)
	m.SetHostLookup(func(string) (config.SSHHost, bool) {
		return config.SSHHost{Address: "2001:db8::1", Port: 2222, User: "fixture"}, true
	})
	got, err := m.proxyJumpArg("bastion")
	if err != nil || got != "fixture@[2001:db8::1]:2222" {
		t.Fatalf("invalid IPv6 jump argument: %q", got)
	}
}

func TestUnavailablePasswordGivesRecoveryInstruction(t *testing.T) {
	_, err := buildAuth(t.TempDir(), config.SSHHost{AuthMode: "password", PasswordUnavailable: true})
	if err == nil || !strings.Contains(err.Error(), "re-enter") {
		t.Fatal("unavailable password must explain recovery")
	}
}

func TestUnavailablePasswordDoesNotBlockConfiguredKeyAuthentication(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generate(dir, "fixture-key", KindED25519, "fixture-only"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"key", "both"} {
		methods, err := buildAuth(dir, config.SSHHost{AuthMode: mode, KeyName: "fixture-key", PasswordUnavailable: true})
		if err != nil || len(methods) != 1 {
			t.Fatalf("unavailable unused password blocked %s key authentication", mode)
		}
	}
}
