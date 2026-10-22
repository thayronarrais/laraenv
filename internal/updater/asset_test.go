package updater

import "testing"

func fullRelease() Release {
	return Release{
		Version:      "0.4.35",
		MsiName:      "LaraEnv-0.4.35.msi",
		MsiURL:       "https://example.test/LaraEnv-0.4.35.msi",
		Sha256URL:    "https://example.test/LaraEnv-0.4.35.msi.sha256",
		ExeName:      "LaraEnv.exe",
		ExeURL:       "https://example.test/LaraEnv.exe",
		ExeSha256URL: "https://example.test/LaraEnv.exe.sha256",
	}
}

func TestAssetForInstalledPicksTheInstaller(t *testing.T) {
	got, err := fullRelease().assetFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if got.name != "LaraEnv-0.4.35.msi" || got.url != "https://example.test/LaraEnv-0.4.35.msi" {
		t.Fatalf("got %+v", got)
	}
}

func TestAssetForPortablePicksTheExecutable(t *testing.T) {
	got, err := fullRelease().assetFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if got.name != "LaraEnv.exe" || got.url != "https://example.test/LaraEnv.exe" {
		t.Fatalf("got %+v", got)
	}
}

// A release with no executable attached must fail for a portable install
// rather than silently handing it the installer, which would create a second
// install in Program Files pointing at an empty default root.
func TestAssetForPortableFailsWithoutAnExecutable(t *testing.T) {
	r := fullRelease()
	r.ExeURL = ""

	if _, err := r.assetFor(true); err == nil {
		t.Fatal("want an error when the release has no .exe asset")
	}
}

// Replacing the running executable is not something to do against an
// unverified download, so a missing sidecar is fatal here — unlike the
// installer path, where msiexec has its own integrity checks and existing
// releases are tolerated without one.
func TestAssetForPortableFailsWithoutAChecksum(t *testing.T) {
	r := fullRelease()
	r.ExeSha256URL = ""

	if _, err := r.assetFor(true); err == nil {
		t.Fatal("want an error when the .exe has no sha256 sidecar")
	}
}

func TestAssetForInstalledToleratesAMissingChecksum(t *testing.T) {
	r := fullRelease()
	r.Sha256URL = ""

	if _, err := r.assetFor(false); err != nil {
		t.Fatalf("existing releases predate the sidecar rule: %v", err)
	}
}
