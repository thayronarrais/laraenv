package installer

import "testing"

// TestPeclSeriesHaveCompleteVCMapping guards against the silent-empty-URL
// failure: peclVCTag returns "" for any PHP series it doesn't know, and
// peclZipURL then returns "" too — so a PECL extension pinned to a series
// missing from peclVCTag installs nothing and fails with no useful error.
// Every PHPSeries referenced anywhere in PeclCatalog() must resolve to a
// non-empty VC tag and a non-empty zip URL. This test never touches the
// network — it only checks that the local mapping is complete.
func TestPeclSeriesHaveCompleteVCMapping(t *testing.T) {
	seen := map[string]bool{}
	for _, ext := range PeclCatalog() {
		for _, b := range ext.Builds {
			if seen[b.PHPSeries] {
				continue
			}
			seen[b.PHPSeries] = true

			vc := peclVCTag(b.PHPSeries)
			if vc == "" {
				t.Errorf("peclVCTag(%q) = \"\": no VC tag mapped for a PHP series used in PeclCatalog() (extension %q)", b.PHPSeries, ext.Name)
				continue
			}

			url := peclZipURL(ext.Name, b)
			if url == "" {
				t.Errorf("peclZipURL(%q, %+v) = \"\": empty URL for a PHP series used in PeclCatalog()", ext.Name, b)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("PeclCatalog() produced no builds to check — test would pass vacuously")
	}
}

// TestCatalogPHPSeriesHaveVCMapping guards the case
// TestPeclSeriesHaveCompleteVCMapping's name suggests but doesn't actually
// cover: it only walks PeclCatalog(), so adding a new PHP series to
// Catalog() without also teaching peclVCTag() its toolchain silently slips
// through — the series just never gets a PECL build offered, with no test
// failure pointing at the gap. Every PHP series in Catalog() must resolve
// to a non-empty VC tag.
func TestCatalogPHPSeriesHaveVCMapping(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Catalog() {
		if s.Kind != KindPHP {
			continue
		}
		series := PeclSeries(s.Version)
		if seen[series] {
			continue
		}
		seen[series] = true

		if peclVCTag(series) == "" {
			t.Errorf("peclVCTag(%q) = \"\": Catalog() has a PHP %s source (version %q) with no toolchain mapping", series, series, s.Version)
		}
	}
	if len(seen) == 0 {
		t.Fatal("Catalog() has no PHP sources to check — test would pass vacuously")
	}
}
