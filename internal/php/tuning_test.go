package php

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePhpTuningSetsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "php.ini")
	// Seed a php.ini resembling the bundled 8.1 defaults: tiny limits, some
	// commented, varied spacing.
	seed := "[PHP]\n" +
		"memory_limit = 128M\n" +
		"max_execution_time = 30\n" +
		";max_input_time = 60\n" +
		"post_max_size = 8M\n" +
		"upload_max_filesize = 2M\n"
	if err := os.WriteFile(ini, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsurePhpTuning(dir, "400M", "400M", "1G", 600, 600); err != nil {
		t.Fatalf("EnsurePhpTuning: %v", err)
	}
	got, _ := os.ReadFile(ini)
	for _, want := range []string{
		"upload_max_filesize = 400M",
		"post_max_size = 400M",
		"memory_limit = 1G",
		"max_execution_time = 600",
		"max_input_time = 600", // was commented; should be set active
	} {
		if !contains(string(got), want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	// Second identical call must not rewrite the file (byte-stable).
	before, _ := os.ReadFile(ini)
	infoBefore, _ := os.Stat(ini)
	if err := EnsurePhpTuning(dir, "400M", "400M", "1G", 600, 600); err != nil {
		t.Fatalf("EnsurePhpTuning 2: %v", err)
	}
	after, _ := os.ReadFile(ini)
	infoAfter, _ := os.Stat(ini)
	if string(before) != string(after) {
		t.Errorf("second call changed content:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Errorf("second call rewrote the file (mtime changed) — not idempotent")
	}
}

func TestEnsurePhpTuningDefaultsOnEmpty(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "php.ini")
	if err := os.WriteFile(ini, []byte("[PHP]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePhpTuning(dir, "", "", "", 0, 0); err != nil {
		t.Fatalf("EnsurePhpTuning: %v", err)
	}
	got, _ := os.ReadFile(ini)
	if !contains(string(got), "upload_max_filesize = 512M") {
		t.Errorf("expected default 512M applied, got:\n%s", got)
	}
}

func TestUpstreamHelpers(t *testing.T) {
	if got := UpstreamName("8.1"); got != "php_8_1" {
		t.Errorf("UpstreamName(8.1) = %q", got)
	}
	if got := UpstreamName("8.4.20"); got != "php_8_4" {
		t.Errorf("UpstreamName(8.4.20) = %q", got)
	}
	if got := ReplicaPort("8.1", 0); got != 9081 {
		t.Errorf("ReplicaPort(8.1,0) = %d", got)
	}
	if got := ReplicaPort("8.1", 3); got != 9381 {
		t.Errorf("ReplicaPort(8.1,3) = %d", got)
	}
	if ReplicaPort("8.4", 1) == ReplicaPort("8.1", 1) {
		t.Errorf("replica ports collide across versions")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
