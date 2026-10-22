package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The portable flag travels from paths.IsPortable() through NewManager into
// every asset decision the manager makes. Releases 0.4.35 through 0.4.40
// accepted the argument and dropped it on the floor, so every portable copy
// downloaded the installer instead: msiexec created a second install in
// Program Files resolving to the default root, and the user's PHP versions,
// projects and databases were still on disk but invisible.
//
// Asserting through the constructor is the point. The tests in asset_test.go
// call Release.assetFor directly and were green throughout, because the value
// they pass is the one that never arrived.
func TestPortableManagerPicksTheExecutable(t *testing.T) {
	m := NewManager(t.TempDir(), "0.4.35", true, nil)

	got, err := fullRelease().assetFor(m.portable)
	if err != nil {
		t.Fatal(err)
	}
	if got.name != "LaraEnv.exe" {
		t.Fatalf("a portable copy must update by replacing its executable, got %q", got.name)
	}
}

func TestInstalledManagerPicksTheInstaller(t *testing.T) {
	m := NewManager(t.TempDir(), "0.4.35", false, nil)

	got, err := fullRelease().assetFor(m.portable)
	if err != nil {
		t.Fatal(err)
	}
	if got.name != "LaraEnv-0.4.35.msi" {
		t.Fatalf("an installed copy must update through the installer, got %q", got.name)
	}
}

// End to end over real HTTP: a portable manager handed a release must fetch
// the executable, verify it against the sidecar, and leave it in the cache.
// Nothing here is stubbed except the server.
func TestPortableDownloadFetchesAndVerifiesTheExecutable(t *testing.T) {
	payload := []byte("MZ this stands in for LaraEnv.exe")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/LaraEnv.exe":
			w.Write(payload)
		case "/LaraEnv.exe.sha256":
			w.Write([]byte(digest + "  LaraEnv.exe\n"))
		default:
			t.Errorf("a portable update must not request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	m := NewManager(cache, "0.4.35", true, nil)
	rel := &Release{
		Version:      "0.4.40",
		MsiName:      "LaraEnv-0.4.40.msi",
		MsiURL:       srv.URL + "/LaraEnv-0.4.40.msi",
		Sha256URL:    srv.URL + "/LaraEnv-0.4.40.msi.sha256",
		ExeName:      "LaraEnv.exe",
		ExeURL:       srv.URL + "/LaraEnv.exe",
		ExeSha256URL: srv.URL + "/LaraEnv.exe.sha256",
		ExeSizeBytes: int64(len(payload)),
	}

	got, err := m.Download(t.Context(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "LaraEnv.exe" {
		t.Fatalf("cached the wrong asset: %s", got)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Fatalf("cached file does not match what the server served")
	}
}

// The second line of defence, and the one that does not depend on anybody
// having passed the right argument: whatever the manager believes, running
// the installer over a copy carrying the portable marker is the failure this
// package exists to prevent. Refuse it rather than damage the install.
func TestApplyRefusesTheInstallerBesideThePortableMarker(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "LaraEnv.exe")
	if err := os.WriteFile(exe, []byte("running executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".laraenv-portable"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	msi := filepath.Join(t.TempDir(), "LaraEnv-0.4.40.msi")
	if err := os.WriteFile(msi, []byte("installer"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A manager that believes it is installed, running from a portable folder:
	// exactly the state 0.4.35 through 0.4.40 shipped in.
	m := NewManager(t.TempDir(), "0.4.35", false, nil)
	m.locateExe = func() (string, error) { return exe, nil }

	if err := m.Apply(msi); err == nil {
		t.Fatal("want a refusal to run the installer over a portable copy")
	}
}

// The UI needs to say which update it is about to apply, so a copy that picks
// the wrong one becomes visible before the user clicks rather than after their
// environment has moved.
func TestManagerReportsHowItWillUpdate(t *testing.T) {
	if !NewManager(t.TempDir(), "0.4.35", true, nil).IsPortable() {
		t.Error("a portable manager must report itself as portable")
	}
	if NewManager(t.TempDir(), "0.4.35", false, nil).IsPortable() {
		t.Error("an installed manager must not report itself as portable")
	}
}
