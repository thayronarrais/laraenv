package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/config"
)

// The Sync card promises end-to-end encryption. These tests pin the contract
// behind that promise: the server only ever receives ciphertext + version,
// and the ciphertext reveals nothing about the payload.

const secretMarker = "hunter2-ssh-password"

func newSyncTestClient(t *testing.T, serverURL string) (*Client, []byte) {
	t.Helper()
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(store)
	cl := store.Get().Cloud
	cl.BaseURL, cl.Token, cl.Plan = serverURL, "test-token", "pro"
	cl.Enabled = true
	cl.KdfSalt = hex.EncodeToString(bytes.Repeat([]byte{0xA5}, 32))
	if err := store.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	if err := client.PrimeKey("correct horse battery staple"); err != nil {
		t.Fatalf("PrimeKey: %v", err)
	}
	key, err := client.loadKey()
	if err != nil {
		t.Fatal(err)
	}
	return client, key
}

func TestPutResourceSendsOnlyCiphertext(t *testing.T) {
	var gotBody []byte
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"ssh_host","external_id":"x","version":1}`))
	}))
	defer server.Close()

	client, key := newSyncTestClient(t, server.URL)
	host := map[string]string{"name": "prod-db", "host": "10.0.0.5", "user": "root", "password": secretMarker}
	if err := client.PutResourceJSON(context.Background(), "ssh_host", "0f7e3b8c-uuid", host, 0); err != nil {
		t.Fatalf("PutResourceJSON: %v", err)
	}

	if gotPath != "/api/v1/resources/ssh_host/0f7e3b8c-uuid" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(body) != 2 || body["ciphertext"] == nil || body["version"] == nil {
		t.Fatalf("server must receive only ciphertext + version, got keys: %v", body)
	}
	for _, leak := range []string{secretMarker, "prod-db", "10.0.0.5", "root"} {
		if strings.Contains(string(gotBody), leak) {
			t.Fatalf("plaintext %q leaked into the request body", leak)
		}
	}
	var ciphertextB64 string
	if err := json.Unmarshal(body["ciphertext"], &ciphertextB64); err != nil {
		t.Fatal(err)
	}
	blob, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		t.Fatalf("ciphertext is not base64: %v", err)
	}
	if bytes.Contains(blob, []byte(secretMarker)) {
		t.Fatal("ciphertext contains the plaintext secret")
	}
	plain, err := Decrypt(key, blob)
	if err != nil {
		t.Fatalf("decrypt with the device key: %v", err)
	}
	var roundTrip map[string]string
	if err := json.Unmarshal(plain, &roundTrip); err != nil || roundTrip["password"] != secretMarker {
		t.Fatalf("round trip mismatch: %s (%v)", plain, err)
	}
}

func TestEncryptRejectsWrongKeyAndTampering(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	other := bytes.Repeat([]byte{2}, 32)
	blob, err := Encrypt(key, []byte(secretMarker))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(other, blob); err == nil {
		t.Fatal("a different key must not decrypt the blob")
	}
	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("GCM must reject a modified ciphertext")
	}
	again, _ := Encrypt(key, []byte(secretMarker))
	if bytes.Equal(blob, again) {
		t.Fatal("each encryption must use a fresh random nonce")
	}
}

func TestSyncStatusDescribesEncryption(t *testing.T) {
	client, _ := newSyncTestClient(t, "http://example.invalid")
	status := client.SyncStatusSnapshot()
	if status.Encryption.Cipher != "AES-256-GCM" || status.Encryption.KDF != "Argon2id" {
		t.Fatalf("encryption description must match crypto.go: %+v", status.Encryption)
	}
	if !status.Encryption.KeyOnDevice {
		t.Fatalf("a primed key must be reported as held on this device: %+v", status.Encryption)
	}
	cl := client.cfg.Get().Cloud
	cl.SyncKey = ""
	_ = client.cfg.SetCloud(cl)
	if client.SyncStatusSnapshot().Encryption.KeyOnDevice {
		t.Fatal("without a key, KeyOnDevice must be false")
	}
}
