package installer

import (
	"strings"
	"testing"

	"lara4ever/internal/paths"
	"lara4ever/internal/vcruntime"
)

func TestCatalogOffersCaddyInstalledUnderTheRoot(t *testing.T) {
	var found bool
	for _, src := range Catalog() {
		if src.Kind != KindCaddy {
			continue
		}
		found = true
		if src.InstallTo != paths.CaddyRoot() {
			t.Errorf("InstallTo = %q, want %q", src.InstallTo, paths.CaddyRoot())
		}
		if !strings.HasSuffix(src.URL, "_windows_amd64.zip") {
			t.Errorf("URL = %q, want the windows/amd64 archive", src.URL)
		}
		// The archive has caddy.exe at its top level, so flattening would
		// look for a wrapper directory that is not there.
		if src.Flatten {
			t.Error("Flatten is on: the Caddy archive has no wrapper directory")
		}
	}
	if !found {
		t.Fatal("Catalog() offers no Caddy source")
	}
}

// TestPHP86IsMarkedPrerelease guards the UI contract: PHP 8.6 has no stable
// release yet (only qa/ betas), so its catalog entry must carry
// Prerelease=true. Without this flag the Downloads page has no way to warn
// the user before they install a beta.
func TestPHP86IsMarkedPrerelease(t *testing.T) {
	var found bool
	for _, src := range Catalog() {
		if src.Kind != KindPHP || !strings.HasPrefix(src.Version, "8.6") {
			continue
		}
		found = true
		if !src.Prerelease {
			t.Errorf("PHP %s: Prerelease = false, want true (no stable 8.6 release exists yet)", src.Version)
		}
	}
	if !found {
		t.Fatal("Catalog() offers no PHP 8.6 source")
	}
}

// TestPHP86DeclaresVS18Redist guards the bug this whole change exists to
// prevent: PHP 8.6 is a vs18 build while vcruntime's default RedistURL is
// the vs17 redistributable. Its catalog entry must declare the vs18
// redistributable explicitly rather than silently falling back to the
// (wrong) default.
func TestPHP86DeclaresVS18Redist(t *testing.T) {
	var found bool
	for _, src := range Catalog() {
		if src.Kind != KindPHP || !strings.HasPrefix(src.Version, "8.6") {
			continue
		}
		found = true
		if src.RedistURL != vcruntime.RedistURLVS18 {
			t.Errorf("PHP %s: RedistURL = %q, want the declared vs18 URL %q (not the default)", src.Version, src.RedistURL, vcruntime.RedistURLVS18)
		}
	}
	if !found {
		t.Fatal("Catalog() offers no PHP 8.6 source")
	}
}

// TestRedistURLForPHPSeriesSurfacesDeclaredOverDefault checks the lookup
// helper the service/composer call sites use: a series with a declared
// RedistURL (8.6 → vs18) must surface it, while an ordinary series (8.5,
// vs17-and-under) must resolve to "" — the vcruntime default — rather than
// some stale/guessed value.
func TestRedistURLForPHPSeriesSurfacesDeclaredOverDefault(t *testing.T) {
	if got := RedistURLForPHPSeries("8.6"); got != vcruntime.RedistURLVS18 {
		t.Errorf(`RedistURLForPHPSeries("8.6") = %q, want %q`, got, vcruntime.RedistURLVS18)
	}
	if got := RedistURLForPHPSeries("8.5"); got != "" {
		t.Errorf(`RedistURLForPHPSeries("8.5") = %q, want "" (default)`, got)
	}
	if got := RedistURLForPHPSeries(""); got != "" {
		t.Errorf(`RedistURLForPHPSeries("") = %q, want ""`, got)
	}
	if got := RedistURLForPHPSeries("99.9"); got != "" {
		t.Errorf(`RedistURLForPHPSeries("99.9") = %q, want "" (unknown series)`, got)
	}
}
