//go:build !windows

package credentials

import "testing"

func TestUnsupportedPlatformDoesNotSavePlaintext(t *testing.T) {
	if value, err := (System{}).Protect("fixture-only"); err == nil || value != "" {
		t.Fatal("unsupported platform must not fall back to plaintext")
	}
	if value, err := (System{}).Unprotect("dpapi:v1:AA=="); err == nil || value != "" {
		t.Fatal("unsupported platform must not expose protected bytes as a password")
	}
}
