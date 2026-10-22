package i18n

import (
	"os"
	"testing"
)

func load(t *testing.T) *Set {
	t.Helper()
	s, err := Load(os.DirFS("testdata"), ".")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestLookupAndInterpolation(t *testing.T) {
	s := load(t)
	cases := []struct {
		name, lang, key string
		vars            map[string]any
		want            string
	}{
		{"plain", "en", "projects.title", nil, "Projects"},
		{"translated", "ru", "projects.title", nil, "Проекты"},
		{"interpolated", "en", "projects.created", map[string]any{"name": "shop"}, "Project shop created"},
		{"falls back to en", "ru", "only.inEnglish", nil, "Fallback me"},
		{"unknown locale falls back", "de", "projects.title", nil, "Projects"},
		{"missing key returns the path", "en", "nope.missing", nil, "nope.missing"},
		{"missing var is left alone", "en", "projects.created", nil, "Project {{name}} created"},
		{"non-count numeric var is not formatted", "en", "server.port", map[string]any{"port": 3000}, "Serving on 3000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.T(tc.lang, tc.key, tc.vars); got != tc.want {
				t.Fatalf("T(%q, %q) = %q, want %q", tc.lang, tc.key, got, tc.want)
			}
		})
	}
}

func TestPluralCategories(t *testing.T) {
	s := load(t)
	cases := []struct {
		lang  string
		count int
		want  string
	}{
		{"en", 1, "1 project"},
		{"en", 0, "0 projects"},
		{"en", 5, "5 projects"},
		{"en", 1234, "1,234 projects"},
		// Russian: 1 → one, 2 → few, 5 → many, 21 → one, 11 → many.
		{"ru", 1, "1 проект"},
		{"ru", 2, "2 проекта"},
		{"ru", 5, "5 проектов"},
		{"ru", 21, "21 проект"},
		{"ru", 11, "11 проектов"},
		{"ru", 1234, "1 234 проекта"},
	}
	for _, tc := range cases {
		got := s.T(tc.lang, "projects.count", map[string]any{"count": tc.count})
		if got != tc.want {
			t.Fatalf("T(%q, count=%d) = %q, want %q", tc.lang, tc.count, got, tc.want)
		}
	}
}

func TestLocalesAndHas(t *testing.T) {
	s := load(t)
	got := s.Locales()
	if len(got) != 2 || got[0].Code != "en" || got[1].Code != "ru" {
		t.Fatalf("Locales() = %+v, want en then ru", got)
	}
	if got[1].NativeName != "Русский" {
		t.Fatalf("NativeName = %q", got[1].NativeName)
	}
	if !s.Has("en", "projects.title") {
		t.Fatal("Has(en, projects.title) = false")
	}
	if s.Has("en", "nope.missing") {
		t.Fatal("Has(en, nope.missing) = true")
	}
}
