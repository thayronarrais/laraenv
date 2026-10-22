// Package i18n loads the JSON translation catalogues shared with the public
// repo and renders keys for a language. It is the Go half of a pair: the
// frontend's frontend/src/lib/i18n.tsx implements the same semantics over the
// same files, and both take plural categories from CLDR, so they agree.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Fallback is the language every lookup falls back to. A key missing here is
// a bug, not a translation gap.
const Fallback = "en"

// doNotTranslateManifest is the shared, machine-readable list of catalogue
// keys the app's own code matches on literally at runtime (SFTP large-file
// handling, agent-restore detection, the VC++ redistributable prompt — see
// langs/.do-not-translate.json). It sits alongside the locale files but is
// not itself a locale, so Load must skip it rather than parse it as one; the
// authoritative enforcement lives in tools/langs/check.mjs, which every
// contributor's PR runs through.
const doNotTranslateManifest = ".do-not-translate.json"

// Meta is the $meta block of a locale file. It is what the language selector
// renders, so adding a language is dropping in a file — nothing carries a
// hardcoded list.
type Meta struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	// NeedsNativeReview is true when the catalogue was produced without a
	// native speaker checking it (machine translation, or a contributor
	// translating out of their own language). Absent or false means
	// reviewed. Surfaced by the language selector so a speaker who opens
	// the app in their own language can see the caveat and fix it.
	NeedsNativeReview bool `json:"needsNativeReview"`
}

// Set is every catalogue that shipped, parsed once at startup.
type Set struct {
	catalogues map[string]map[string]any
	metas      map[string]Meta
}

// pluralForms maps x/text's Form constants to the CLDR category names used as
// keys in the catalogue. The order matches the iota order in the plural
// package: Other, Zero, One, Two, Few, Many.
var pluralForms = [...]string{"other", "zero", "one", "two", "few", "many"}

// Load parses every *.json under dir. A file that does not parse is an error:
// shipping a catalogue we cannot read should fail loudly at startup, not
// render key paths at the user.
func Load(fsys fs.FS, dir string) (*Set, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	s := &Set{
		catalogues: map[string]map[string]any{},
		metas:      map[string]Meta{},
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == doNotTranslateManifest {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		code := strings.TrimSuffix(e.Name(), ".json")
		meta := Meta{Code: code, Name: code, NativeName: code}
		if m, ok := doc["$meta"].(map[string]any); ok {
			if v, ok := m["name"].(string); ok {
				meta.Name = v
			}
			if v, ok := m["nativeName"].(string); ok {
				meta.NativeName = v
			}
			if v, ok := m["needsNativeReview"].(bool); ok {
				meta.NeedsNativeReview = v
			}
		}
		s.catalogues[code] = doc
		s.metas[code] = meta
	}
	if _, ok := s.catalogues[Fallback]; !ok {
		return nil, fmt.Errorf("no %s.json in %s", Fallback, dir)
	}
	return s, nil
}

// Locales lists what shipped, English first and the rest alphabetical, which
// is the order the selector shows.
func (s *Set) Locales() []Meta {
	out := make([]Meta, 0, len(s.metas))
	for _, m := range s.metas {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code == Fallback {
			return true
		}
		if out[j].Code == Fallback {
			return false
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Has reports whether lang defines key itself, without falling back.
func (s *Set) Has(lang, key string) bool {
	_, ok := lookup(s.catalogues[lang], key)
	return ok
}

// T renders key for lang: the locale's own value, else English, else the key
// path itself so a missing string is visible rather than blank.
func (s *Set) T(lang, key string, vars map[string]any) string {
	node, ok := lookup(s.catalogues[lang], key)
	if !ok {
		node, ok = lookup(s.catalogues[Fallback], key)
		if !ok {
			return key
		}
		lang = Fallback
	}
	return interpolate(resolve(node, lang, vars), lang, vars)
}

// lookup walks a dot-separated path through the nested catalogue.
func lookup(doc map[string]any, key string) (any, bool) {
	if doc == nil {
		return nil, false
	}
	var node any = doc
	for _, part := range strings.Split(key, ".") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		node, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return node, true
}

// resolve turns a catalogue node into a string. A plain string is itself; an
// object is a plural entry and needs vars["count"] to pick a category.
func resolve(node any, lang string, vars map[string]any) string {
	if str, ok := node.(string); ok {
		return str
	}
	forms, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	category := "other"
	if n, ok := intCount(vars["count"]); ok {
		category = pluralCategory(lang, n)
	}
	if v, ok := forms[category].(string); ok {
		return v
	}
	// A translator omitted this category. "other" always exists — CI enforces
	// it — so rendering that beats rendering nothing.
	if v, ok := forms["other"].(string); ok {
		return v
	}
	return ""
}

// pluralCategory asks CLDR which form lang uses for n. The frontend asks
// Intl.PluralRules the same question and gets the same answer.
func pluralCategory(lang string, n int) string {
	tag, err := language.Parse(lang)
	if err != nil {
		tag = language.English
	}
	if n < 0 {
		n = -n
	}
	form := plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0)
	if int(form) < len(pluralForms) {
		return pluralForms[form]
	}
	return "other"
}

// intCount accepts the shapes a count arrives in: an int from Go, a float64
// when it came through JSON.
func intCount(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// interpolate replaces {{name}} with vars["name"]. {{count}} is formatted for
// the locale — 1234 is "1,234" in English and "1.234" in Portuguese — because
// a count is prose. Every other variable is inserted verbatim: a port, a PID
// or a version number must never grow a thousands separator.
func interpolate(s, lang string, vars map[string]any) string {
	if len(vars) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	for k, v := range vars {
		token := "{{" + k + "}}"
		if !strings.Contains(s, token) {
			continue
		}
		rendered := fmt.Sprint(v)
		if k == "count" {
			if n, ok := intCount(v); ok {
				tag, err := language.Parse(lang)
				if err != nil {
					tag = language.English
				}
				rendered = message.NewPrinter(tag).Sprint(number.Decimal(n))
			}
		}
		s = strings.ReplaceAll(s, token, rendered)
	}
	return s
}

// Match picks the best available catalogue for a preferred BCP-47 tag: an
// exact code first, then any catalogue sharing its base language, then
// English. "pt-PT" resolves to "pt-BR" because Brazilian Portuguese is the
// only Portuguese that ships, and it is far closer than English.
func Match(preferred string, available []Meta) string {
	if preferred == "" {
		return Fallback
	}
	lower := strings.ToLower(preferred)
	for _, m := range available {
		if strings.EqualFold(m.Code, preferred) {
			return m.Code
		}
	}
	base := lower
	if i := strings.IndexAny(base, "-_"); i > 0 {
		base = base[:i]
	}
	for _, m := range available {
		mine := strings.ToLower(m.Code)
		if i := strings.IndexAny(mine, "-_"); i > 0 {
			mine = mine[:i]
		}
		if mine == base {
			return m.Code
		}
	}
	return Fallback
}
