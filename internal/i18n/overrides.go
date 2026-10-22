package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"golang.org/x/text/language"
)

// ProtectedKey is one entry from the do-not-translate manifest
// (langs/.do-not-translate.json): a catalogue key whose value the app's own
// code matches on literally at runtime, and the exact substring(s) that
// match must survive translation untouched.
type ProtectedKey struct {
	Substrings []string `json:"substrings"`
	Reason     string   `json:"reason"`
}

type manifestDoc struct {
	Protected map[string]ProtectedKey `json:"protected"`
}

// LoadManifest reads dir's do-not-translate manifest, mirroring
// tools/langs/check.mjs's readDoNotTranslate. A missing or malformed
// manifest means no protected keys: the manifest is optional infrastructure,
// not itself a translation file, so its absence must never fail a caller
// that is simply trying to load catalogues.
func LoadManifest(fsys fs.FS, dir string) map[string]ProtectedKey {
	raw, err := fs.ReadFile(fsys, path.Join(dir, doNotTranslateManifest))
	if err != nil {
		return map[string]ProtectedKey{}
	}
	var doc manifestDoc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Protected == nil {
		return map[string]ProtectedKey{}
	}
	return doc.Protected
}

var pluralCategorySet = map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}

// isPluralEntry mirrors tools/langs/check.mjs's isPluralEntry: a non-empty
// object whose every key is a CLDR plural category is a single translatable
// unit, not a namespace to keep walking into.
func isPluralEntry(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return false
	}
	for k := range m {
		if !pluralCategorySet[k] {
			return false
		}
	}
	return true
}

// flattenLeaves walks doc and records every leaf under its dot-joined path —
// a plain string, or a whole plural-forms object treated as one unit. This
// is the granularity "a key" means throughout override merging: a file
// naming three keys changes exactly those three, nothing coarser and nothing
// that silently drags sibling keys along with it.
func flattenLeaves(doc map[string]any, prefix string, out map[string]any) {
	for k, v := range doc {
		if k == "$meta" {
			continue
		}
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		if m, ok := v.(map[string]any); ok && !isPluralEntry(v) {
			flattenLeaves(m, p, out)
			continue
		}
		out[p] = v
	}
}

// setLeaf writes value at key's dot-separated path into doc, creating
// intermediate maps as needed — the counterpart to flattenLeaves, used to
// rebuild a nested document from the flat key/value pairs an override
// actually supplied.
func setLeaf(doc map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	m := doc
	for i, part := range parts {
		if i == len(parts)-1 {
			m[part] = value
			return
		}
		next, ok := m[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[part] = next
		}
		m = next
	}
}

// isPlausibleLocaleCode reports whether code could plausibly be a BCP-47
// locale tag — the check that keeps an unrelated file (README-notes-2024.json,
// backup-2024.json) from becoming a selectable phantom locale just because it
// sits in the folder and ends in .json.
//
// It deliberately accepts anything language.Parse accepts rather than
// demanding canonical form or a known region/script combination: "pt-BR",
// "zh-Hant", "sr-Latn-RS" and an underscore variant like "pt_BR" must all
// pass. tools/langs/check.mjs is the strict CI-side gate for what actually
// ships; this only needs to catch names no one would genuinely give a
// locale file. A false rejection here would silently discard a translator's
// real work, which is worse than a phantom locale in the selector — so when
// in doubt, this accepts.
func isPlausibleLocaleCode(code string) bool {
	_, err := language.Parse(code)
	return err == nil
}

// deepCopyDoc clones a parsed catalogue so merging into it can never mutate a
// document a published *Set already holds — Global is an atomic.Pointer[Set]
// and a published Set is treated as immutable.
func deepCopyDoc(doc map[string]any) map[string]any {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		if m, ok := v.(map[string]any); ok {
			out[k] = deepCopyDoc(m)
		} else {
			out[k] = v
		}
	}
	return out
}

// RejectedKey is one override key MergeOverrides refused to apply because
// its value violates the do-not-translate manifest, and why.
type RejectedKey struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// OverrideResult is what one locale's on-disk override file contributed.
// LoadCatalogues (app.go) records these for the bridge, which the Settings
// screen's status line reads: the app must say when an override is active,
// and name any key it refused (see the feature's rules 5 and 6).
//
// Unreadable is set instead of Applied/Rejected when the file itself could
// not be turned into a catalogue at all — invalid JSON (a trailing comma is
// the everyday case; every translator hits a syntax error eventually, far
// more often than a manifest violation), valid JSON that isn't an object (a
// bare array or string), or a filename that isn't a plausible locale code
// (README-notes-2024.json). The file is still skipped rather than failing
// startup — that risk is accepted deliberately — but silence about *why*
// nothing changed defeats the entire reason Rejected exists: a translator
// who edits and sees no effect needs to be able to find out why.
type OverrideResult struct {
	Locale     string        `json:"locale"`
	Applied    int           `json:"applied"`
	Rejected   []RejectedKey `json:"rejected"`
	Unreadable string        `json:"unreadable"`
}

// MergeOverrides looks for dir/*.json under fsys — in production,
// os.DirFS(paths.Root()) with dir "langs" — and merges each file's keys into
// base at the key level: a translator's file naming three keys changes only
// those three, and a code entirely absent from base becomes a new locale
// built from just what is on disk. base is never mutated — it may already be
// published via SetGlobal.
//
// A protected key (per manifest) whose override value does not contain its
// required substring(s) is rejected for that key alone: the embedded value
// keeps rendering through the ordinary fallback chain, and the rejection is
// recorded in the returned []OverrideResult rather than discarding the rest
// of the file.
//
// A missing dir is the normal case for almost every user and is silent: the
// returned Set is base itself, unchanged, and both other return values are
// nil. A file that fails to parse is skipped rather than treated as fatal —
// a translator's editor can leave a JSON file momentarily invalid mid-edit,
// and that must not be able to take the whole app down; it will simply keep
// rendering the embedded catalogue for that locale until the file is valid
// again and the app restarts (rule 2: load at startup only, no watcher). It
// is not silent, though: it produces an OverrideResult naming the file and
// why it could not be read, exactly like a rejected key, because "I edited
// and nothing changed" needs an answer for a syntax error the same way it
// does for a do-not-translate violation — and a syntax error is the one a
// translator will actually hit.
//
// The second return value is, per contributing locale, a fragment document
// containing only the accepted overridden keys (plus $meta, if the file
// supplied one, or a code-based default for a brand-new locale) — small
// enough to hand across the Wails bridge and exactly what the frontend's
// I18nProvider needs to overlay onto what it bundled at build time.
func MergeOverrides(base *Set, fsys fs.FS, dir string, manifest map[string]ProtectedKey) (*Set, map[string]map[string]any, []OverrideResult) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return base, nil, nil
	}

	merged := &Set{
		catalogues: make(map[string]map[string]any, len(base.catalogues)),
		metas:      make(map[string]Meta, len(base.metas)),
	}
	for code, doc := range base.catalogues {
		merged.catalogues[code] = deepCopyDoc(doc)
	}
	for code, m := range base.metas {
		merged.metas[code] = m
	}

	var fragments map[string]map[string]any
	var results []OverrideResult

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == doNotTranslateManifest {
			continue
		}
		code := strings.TrimSuffix(e.Name(), ".json")
		if !isPlausibleLocaleCode(code) {
			results = append(results, OverrideResult{Locale: code, Unreadable: "not a language code"})
			continue
		}

		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			results = append(results, OverrideResult{Locale: code, Unreadable: err.Error()})
			continue
		}
		// json.Unmarshal into a map[string]any also catches "valid JSON but
		// not an object" (a bare array or string) with its own descriptive
		// error — no separate check needed for that case.
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			results = append(results, OverrideResult{Locale: code, Unreadable: err.Error()})
			continue
		}

		target, existed := merged.catalogues[code]
		if !existed {
			target = map[string]any{}
			merged.catalogues[code] = target
		}
		meta, hasMeta := merged.metas[code]
		if !hasMeta {
			meta = Meta{Code: code, Name: code, NativeName: code}
		}
		var metaFragment map[string]any
		if m, ok := doc["$meta"].(map[string]any); ok {
			if v, ok := m["name"].(string); ok {
				meta.Name = v
			}
			if v, ok := m["nativeName"].(string); ok {
				meta.NativeName = v
			}
			metaFragment = m
		}
		merged.metas[code] = meta

		leaves := map[string]any{}
		flattenLeaves(doc, "", leaves)

		fragment := map[string]any{}
		// Rejected starts as a non-nil empty slice, not the zero value: the
		// normal, successful case is zero rejections, and append never
		// allocates for that case, so a nil Rejected would marshal to JSON
		// `null` instead of `[]` for exactly the override that worked
		// perfectly. The frontend indexes this field unconditionally
		// (LanguageSelector reads activeOverride.rejected.length), so the
		// JSON shape must be honest about being an array even when empty.
		result := OverrideResult{Locale: code, Rejected: []RejectedKey{}}
		for key, value := range leaves {
			if spec, protected := manifest[key]; protected {
				str, isStr := value.(string)
				if !isStr || !containsAll(str, spec.Substrings) {
					result.Rejected = append(result.Rejected, RejectedKey{
						Key:    key,
						Reason: rejectionReason(spec),
					})
					continue
				}
			}
			setLeaf(target, key, value)
			setLeaf(fragment, key, value)
			result.Applied++
		}
		sort.Slice(result.Rejected, func(i, j int) bool { return result.Rejected[i].Key < result.Rejected[j].Key })

		if result.Applied == 0 && len(result.Rejected) == 0 {
			continue
		}
		if metaFragment != nil {
			fragment["$meta"] = metaFragment
		} else if !existed {
			fragment["$meta"] = map[string]any{"code": code, "name": code, "nativeName": code}
		}
		if fragments == nil {
			fragments = map[string]map[string]any{}
		}
		fragments[code] = fragment
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Locale < results[j].Locale })
	return merged, fragments, results
}

// containsAll reports whether s contains every one of substrings.
func containsAll(s string, substrings []string) bool {
	for _, sub := range substrings {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// rejectionReason renders a ProtectedKey's requirement as a short sentence
// fragment for RejectedKey.Reason, e.g. `must contain "file too large"`.
func rejectionReason(spec ProtectedKey) string {
	if len(spec.Substrings) == 1 {
		return fmt.Sprintf("must contain %q", spec.Substrings[0])
	}
	return fmt.Sprintf("must contain all of %q", spec.Substrings)
}
