package updater

import "testing"

// The real asset list of a release, including the sidecars. The ordering of
// the suffix checks matters: "LaraEnv.exe.sha256" must land as the checksum,
// never as the executable itself.
func TestAssetsAreClassifiedBySuffix(t *testing.T) {
	var out Release
	applyAssets(&out, []ghAsset{
		{Name: "LaraEnv-Setup.msi", Size: 30, DownloadURL: "u/msi"},
		{Name: "LaraEnv-0.4.35.msi.sha256", DownloadURL: "u/msi-sha"},
		{Name: "LaraEnv.exe", Size: 20, DownloadURL: "u/exe"},
		{Name: "LaraEnv.exe.sha256", DownloadURL: "u/exe-sha"},
		{Name: "LaraEnv-0.4.35-portable-win64.zip", DownloadURL: "u/zip"},
	})

	for _, c := range []struct{ name, got, want string }{
		{"msi", out.MsiURL, "u/msi"},
		{"msi sha", out.Sha256URL, "u/msi-sha"},
		{"exe", out.ExeURL, "u/exe"},
		{"exe sha", out.ExeSha256URL, "u/exe-sha"},
		{"exe name", out.ExeName, "LaraEnv.exe"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if out.ExeSizeBytes != 20 || out.SizeBytes != 30 {
		t.Errorf("sizes crossed over: exe=%d msi=%d", out.ExeSizeBytes, out.SizeBytes)
	}
}

// Every release published so far carries only the installer. Those must keep
// upgrading an installed copy, and must be refused for a portable one.
func TestReleaseWithOnlyAnInstallerStillUpgradesAnInstalledCopy(t *testing.T) {
	var out Release
	applyAssets(&out, []ghAsset{
		{Name: "LaraEnv-Setup.msi", DownloadURL: "u/msi"},
		{Name: "LaraEnv-0.4.34.msi.sha256", DownloadURL: "u/msi-sha"},
	})

	if _, err := out.assetFor(false); err != nil {
		t.Fatalf("installed copy must still upgrade: %v", err)
	}
	if _, err := out.assetFor(true); err == nil {
		t.Fatal("portable copy must be refused, not handed the installer")
	}
}
