package main

import (
	"testing"

	"lara4ever/internal/installer"
)

// TestRecommendedSourcesSkipsPrereleases is the regression test for the
// "beta became the default PHP" defect: recommendedSources() used to take
// the first catalog entry per kind, breaking on the first match regardless
// of Prerelease. PHP 8.6.0beta3 sits above 8.5 in the catalog, so the
// recommended/bootstrap stack was installing a beta as the user's default
// PHP. A prerelease must never be what recommendedSources() returns for any
// kind — this must fail if the Prerelease filter is removed.
func TestRecommendedSourcesSkipsPrereleases(t *testing.T) {
	for _, s := range recommendedSources() {
		if s.Prerelease {
			t.Errorf("recommendedSources() returned a prerelease source: kind=%s version=%s — a prerelease must never be a recommended default", s.Kind, s.Version)
		}
	}
}

// TestRecommendedSourcesPHPIsStable pins down the specific field failure:
// the recommended PHP must resolve to a stable version, not to the 8.6
// beta that sits above it in the catalog. Confirms recommendedSources()
// picks the newest *stable* PHP rather than the newest PHP entry overall.
func TestRecommendedSourcesPHPIsStable(t *testing.T) {
	var php *installer.Source
	for i, s := range recommendedSources() {
		if s.Kind == installer.KindPHP {
			php = &recommendedSources()[i]
			break
		}
	}
	if php == nil {
		t.Fatal("recommendedSources() returned no PHP source")
	}
	if php.Prerelease {
		t.Errorf("recommended PHP = %s, Prerelease = true; want a stable PHP version", php.Version)
	}
}

// TestRecommendedSourcesPHPSeriesMatchesNewestStableInCatalog guards the
// existing-user regression IsStackBootstrapped() depends on: before this
// fix, recommendedSources() would resolve PHP to the 8.6 beta's series
// (\php\8.6), which no existing user has installed, so IsStackBootstrapped()
// returned false for everyone and Dashboard's "Start all" opened the
// bootstrap modal instead of starting their already-installed stack. With
// the Prerelease filter in place, recommendedSources() must resolve to the
// newest *stable* PHP series in the catalog (currently 8.5) — the series
// existing users actually have — so IsStackBootstrapped(), which walks the
// same recommendedSources() list, resolves correctly for them again.
func TestRecommendedSourcesPHPSeriesMatchesNewestStableInCatalog(t *testing.T) {
	var wantSeries string
	for _, s := range installer.Catalog() {
		if s.Kind == installer.KindPHP && !s.Prerelease {
			wantSeries = s.Version
			break
		}
	}
	if wantSeries == "" {
		t.Fatal("Catalog() has no stable PHP source to compare against")
	}

	var got string
	for _, s := range recommendedSources() {
		if s.Kind == installer.KindPHP {
			got = s.Version
			break
		}
	}
	if got != wantSeries {
		t.Errorf("recommendedSources() PHP = %q, want the newest stable catalog entry %q", got, wantSeries)
	}
}
