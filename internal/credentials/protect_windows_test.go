//go:build windows

package credentials

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestDPAPIPreservesExactPasswordAndRejectsTampering(t *testing.T) {
	protector := System{}
	const password = "fixture-only: senha ç🔐 with trailing space "
	protected, err := protector.Protect(password)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(protected, "dpapi:v1:") || strings.Contains(protected, password) {
		t.Fatal("password was not protected with the versioned envelope")
	}
	got, err := protector.Unprotect(protected)
	if err != nil || got != password {
		t.Fatal("Windows protection changed the password")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(protected, "dpapi:v1:"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(password)) {
		t.Fatal("protected bytes contain plaintext")
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	if value, err := protector.Unprotect("dpapi:v1:" + base64.StdEncoding.EncodeToString(ciphertext)); err == nil || value != "" {
		t.Fatal("tampered credential must fail without a plaintext fallback")
	}
}

func TestDPAPIRejectsUnsupportedAndMalformedEnvelopes(t *testing.T) {
	for _, value := range []string{"", "plaintext-fixture", "dpapi:v2:AA==", "dpapi:v1:", "dpapi:v1:%%%", "dpapi:v1:AA=="} {
		if password, err := (System{}).Unprotect(value); err == nil || password != "" {
			t.Fatal("invalid protected credential did not fail closed")
		}
	}
}
