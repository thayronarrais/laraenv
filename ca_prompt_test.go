package main

import (
	"testing"

	"lara4ever/internal/vhost"
)

// TestShouldPromptTrustLocalCA pins down exactly when the Projects page may
// offer the in-place Trust prompt: only a portable copy, and only for a CA
// that genuinely exists, was checked, and came back not trusted. Every other
// combination — including every installed-copy case — must come back false,
// since an installed copy trusts automatically and must never gain this
// friction (see shouldPromptTrustLocalCA's doc comment).
func TestShouldPromptTrustLocalCA(t *testing.T) {
	for _, tc := range []struct {
		name     string
		portable bool
		status   vhost.CAStatus
		want     bool
	}{
		{
			name:     "portable, existing CA, checked, not trusted: prompt",
			portable: true,
			status:   vhost.CAStatus{Exists: true, Trusted: false},
			want:     true,
		},
		{
			name:     "installed, existing CA, checked, not trusted: unchanged, no prompt",
			portable: false,
			status:   vhost.CAStatus{Exists: true, Trusted: false},
			want:     false,
		},
		{
			name:     "installed, existing CA, already trusted: no prompt",
			portable: false,
			status:   vhost.CAStatus{Exists: true, Trusted: true},
			want:     false,
		},
		{
			name:     "portable, but CA already trusted: no prompt",
			portable: true,
			status:   vhost.CAStatus{Exists: true, Trusted: true},
			want:     false,
		},
		{
			name:     "portable, no CA created yet: no prompt",
			portable: true,
			status:   vhost.CAStatus{Exists: false},
			want:     false,
		},
		{
			name:     "installed, no CA created yet: no prompt",
			portable: false,
			status:   vhost.CAStatus{Exists: false},
			want:     false,
		},
		{
			name:     "portable, existing CA, but the trust check itself failed: no prompt",
			portable: true,
			status:   vhost.CAStatus{Exists: true, Trusted: false, CheckError: "certutil not found"},
			want:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPromptTrustLocalCA(tc.portable, tc.status); got != tc.want {
				t.Errorf("shouldPromptTrustLocalCA(portable=%v, status=%+v) = %v, want %v", tc.portable, tc.status, got, tc.want)
			}
		})
	}
}
