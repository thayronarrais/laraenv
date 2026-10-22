package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentActivityRootFollowsInstallRoot(t *testing.T) {
	previous := root
	root = filepath.Join(t.TempDir(), "portable-install")
	t.Cleanup(func() { root = previous })
	if got, want := AgentActivityRoot(), filepath.Join(root, "state", "agent-activity"); got != want {
		t.Fatalf("AgentActivityRoot() = %q, want %q", got, want)
	}
}

// An existing install has the exe in %ProgramFiles%\LaraEnv and no marker.
// It must keep resolving to C:\laraenv exactly as before — updating must not
// move anyone's projects, databases or config.
func TestResolveKeepsLegacyRootWhenNoMarkerAndNoEnv(t *testing.T) {
	exeDir := t.TempDir()

	got := resolve(exeDir, "")

	if got != `C:\laraenv` {
		t.Fatalf("resolve() = %q, want %q — this breaks every existing install", got, `C:\laraenv`)
	}
}

// The portable ZIP ships a marker next to the exe: the folder holding the exe
// is the whole environment.
func TestResolveUsesExeDirWhenPortableMarkerPresent(t *testing.T) {
	exeDir := t.TempDir()
	writeMarker(t, exeDir)

	got := resolve(exeDir, "")

	if got != exeDir {
		t.Fatalf("resolve() = %q, want %q", got, exeDir)
	}
}

// LARAENV_ROOT is the escape hatch: it wins over everything, including a
// marker, so a test or a power user can point the app anywhere.
func TestResolveEnvOverrideWinsOverMarker(t *testing.T) {
	exeDir := t.TempDir()
	writeMarker(t, exeDir)
	want := filepath.Join(t.TempDir(), "elsewhere")

	got := resolve(exeDir, want)

	if got != want {
		t.Fatalf("resolve() = %q, want %q", got, want)
	}
}

func writeMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, portableMarker), nil, 0644); err != nil {
		t.Fatal(err)
	}
}

// The exact on-disk layout an existing install already has. Pinned as
// literals on purpose: comparing against the very functions under test would
// let both sides drift together and still pass. If this test fails, an
// update is about to relocate somebody's projects and databases.
func TestExistingInstallLayoutIsUnchanged(t *testing.T) {
	if os.Getenv(EnvRoot) != "" {
		t.Skipf("%s is set, this test describes the default install", EnvRoot)
	}

	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"root", Root(), `C:\laraenv`},
		{"projects", ProjectsRoot(), `C:\laraenv\www`},
		{"config", ConfigFile(), `C:\laraenv\config.json`},
		{"state", StateRoot(), `C:\laraenv\state`},
		{"php", PHPRoot(), `C:\laraenv\php`},
		{"mysql data", MySQLData(), `C:\laraenv\mysql\data`},
		{"postgres data", PostgresData(), `C:\laraenv\postgres\data`},
		{"ssh", SSHRoot(), `C:\laraenv\ssh`},
		{"nginx sites", NginxSites(), `C:\laraenv\nginx\conf\sites`},
		{"apache sites", ApacheSites(), `C:\laraenv\apache\conf\sites`},
		{"bin", Bin(), `C:\laraenv\bin`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// The updater needs to know whether swapping the executable in place keeps
// the app pointing at the same environment. That is true exactly when the
// marker put us in portable mode — an environment variable override is a
// support escape hatch, not a distribution mode, and still wants the MSI.
func TestIsPortableFollowsTheMarkerNotTheRoot(t *testing.T) {
	withMarker := t.TempDir()
	writeMarker(t, withMarker)
	withoutMarker := t.TempDir()

	if !IsPortableDir(withMarker) {
		t.Error("marker present: want portable")
	}
	if IsPortableDir(withoutMarker) {
		t.Error("no marker: want not portable")
	}
}

// The marker's name is a contract between this package and whatever builds
// the portable ZIP. The packaging step cannot read a Go constant, so it ships
// a file committed to the repo — and this asserts the two still agree. Rename
// portableMarker without renaming that file and every portable build silently
// stops being portable.
func TestPackagedMarkerMatchesTheConstant(t *testing.T) {
	packaged := filepath.Join("..", "..", "build", "windows", "portable", portableMarker)

	if _, err := os.Stat(packaged); err != nil {
		t.Fatalf("the portable payload has no %s next to it: %v", portableMarker, err)
	}
}

func TestCaddyPathsSitUnderTheInstallRoot(t *testing.T) {
	if os.Getenv(EnvRoot) != "" {
		t.Skipf("%s is set, this test describes the default install", EnvRoot)
	}

	for _, tc := range []struct{ name, got, want string }{
		{"root", CaddyRoot(), `C:\laraenv\caddy`},
		{"exe", CaddyExe(), `C:\laraenv\caddy\caddy.exe`},
		{"caddyfile", CaddyFile(), `C:\laraenv\caddy\Caddyfile`},
		{"sites", CaddySites(), `C:\laraenv\caddy\sites`},
		{"logs", CaddyLogs(), `C:\laraenv\caddy\logs`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
