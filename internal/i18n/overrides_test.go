package i18n

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOverrideJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// A partial override file — one that names only a handful of keys — must
// change only those keys. Every sibling key, in the same locale, keeps
// rendering the embedded value.
func TestMergeOverrides_PartialFileChangesOnlyItsKeys(t *testing.T) {
	base := load(t)
	dir := t.TempDir()
	writeOverrideJSON(t, dir, "ru.json", map[string]any{
		"projects": map[string]any{"title": "Проекты (правка)"},
	})

	merged, fragments, results := MergeOverrides(base, os.DirFS(dir), ".", nil)

	if got := merged.T("ru", "projects.title", nil); got != "Проекты (правка)" {
		t.Fatalf("overridden key = %q", got)
	}
	if got := merged.T("ru", "projects.created", nil); got != "Проект {{name}} создан" {
		t.Fatalf("untouched sibling key changed: %q", got)
	}
	if got := merged.T("ru", "projects.count", map[string]any{"count": 2}); got != "2 проекта" {
		t.Fatalf("untouched plural key changed: %q", got)
	}
	if len(fragments["ru"]) == 0 {
		t.Fatal("expected a ru fragment for the bridge")
	}
	if len(results) != 1 || results[0].Locale != "ru" || results[0].Applied != 1 || len(results[0].Rejected) != 0 {
		t.Fatalf("results = %+v", results)
	}

	// base itself must never be mutated — it may already be a published
	// Global() Set, which is treated as immutable after Load.
	if got := base.T("ru", "projects.title", nil); got != "Проекты" {
		t.Fatalf("base was mutated: %q", got)
	}
}

// A locale that exists only on disk must become selectable, built from just
// what the file supplies — everything else falls back to English exactly
// the way a shipped, incomplete locale would.
func TestMergeOverrides_DiskOnlyLocaleAppearsInSelector(t *testing.T) {
	base := load(t)
	dir := t.TempDir()
	writeOverrideJSON(t, dir, "de.json", map[string]any{
		"$meta":    map[string]any{"code": "de", "name": "German", "nativeName": "Deutsch"},
		"projects": map[string]any{"title": "Projekte"},
	})

	merged, _, results := MergeOverrides(base, os.DirFS(dir), ".", nil)

	var found *Meta
	for _, m := range merged.Locales() {
		if m.Code == "de" {
			mCopy := m
			found = &mCopy
		}
	}
	if found == nil {
		t.Fatal("de is not in Locales() — a disk-only locale must appear in the selector")
	}
	if found.NativeName != "Deutsch" {
		t.Fatalf("NativeName = %q, want Deutsch", found.NativeName)
	}
	if got := merged.T("de", "projects.title", nil); got != "Projekte" {
		t.Fatalf("de.projects.title = %q", got)
	}
	// A key the disk-only file never supplied still falls back to English.
	if got := merged.T("de", "projects.created", map[string]any{"name": "shop"}); got != "Project shop created" {
		t.Fatalf("fallback for an unsupplied key = %q", got)
	}
	if len(results) != 1 || results[0].Locale != "de" || results[0].Applied != 1 {
		t.Fatalf("results = %+v", results)
	}
}

// An override that violates the do-not-translate manifest is rejected for
// that key alone: the embedded value is used instead, and the rest of the
// file is still applied.
func TestMergeOverrides_ManifestViolationRejectsOnlyThatKey(t *testing.T) {
	base := load(t)
	dir := t.TempDir()
	writeOverrideJSON(t, dir, "ru.json", map[string]any{
		"only":     map[string]any{"inEnglish": "translated, dropping the required text"},
		"projects": map[string]any{"title": "Проекты (правка)"},
	})
	manifest := map[string]ProtectedKey{
		"only.inEnglish": {Substrings: []string{"Fallback"}, Reason: "test fixture"},
	}

	merged, _, results := MergeOverrides(base, os.DirFS(dir), ".", manifest)

	// The protected key's override is discarded; the key still resolves
	// through the ordinary fallback chain (ru has no value of its own, so
	// this lands on the embedded English text) rather than the rejected
	// override.
	if got := merged.T("ru", "only.inEnglish", nil); got != "Fallback me" {
		t.Fatalf("protected key = %q, want the embedded fallback, not the rejected override", got)
	}
	// An unrelated key in the same file is untouched by the rejection.
	if got := merged.T("ru", "projects.title", nil); got != "Проекты (правка)" {
		t.Fatalf("unrelated key = %q", got)
	}

	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	r := results[0]
	if r.Applied != 1 {
		t.Fatalf("Applied = %d, want 1 (the unrelated key)", r.Applied)
	}
	if len(r.Rejected) != 1 || r.Rejected[0].Key != "only.inEnglish" {
		t.Fatalf("Rejected = %+v", r.Rejected)
	}
	if r.Rejected[0].Reason == "" {
		t.Fatal("Rejected reason must not be empty — the translator needs to know why")
	}
}

// A missing overrides folder is the normal case for almost everyone and must
// be silent: no error, no change to the catalogue set, nothing to report.
func TestMergeOverrides_MissingDirIsSilent(t *testing.T) {
	base := load(t)
	dir := t.TempDir() // exists, but has no "langs" subdirectory

	merged, fragments, results := MergeOverrides(base, os.DirFS(dir), "langs", nil)

	if merged != base {
		t.Fatal("expected the same *Set back unchanged when the overrides dir is missing")
	}
	if fragments != nil {
		t.Fatalf("expected nil fragments, got %+v", fragments)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %+v", results)
	}
}

// A malformed override file (the everyday case: a trailing comma) must not
// take the whole app down — it is skipped, every other file in the same
// folder still applies normally, and the malformed file is REPORTED, not
// silently dropped: a translator who edits and sees no effect needs to find
// out why, and a syntax error is the failure they will actually hit, far
// more often than a manifest violation.
func TestMergeOverrides_UnreadableFileIsSkippedButReported(t *testing.T) {
	base := load(t)
	dir := t.TempDir()
	// A trailing comma — the classic editor slip, not a contrived error.
	if err := os.WriteFile(filepath.Join(dir, "ru.json"), []byte(`{"projects": {"title": "X",}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeOverrideJSON(t, dir, "de.json", map[string]any{
		"$meta":    map[string]any{"code": "de", "name": "German", "nativeName": "Deutsch"},
		"projects": map[string]any{"title": "Projekte"},
	})

	merged, _, results := MergeOverrides(base, os.DirFS(dir), ".", nil)

	// The broken file changes nothing for its own locale...
	if got := merged.T("ru", "projects.title", nil); got != "Проекты" {
		t.Fatalf("ru was disturbed by its own unreadable override: %q", got)
	}
	// ...and does not prevent the other, valid file in the same folder from
	// applying.
	if got := merged.T("de", "projects.title", nil); got != "Projekte" {
		t.Fatalf("de.projects.title = %q, want the valid file to still apply", got)
	}

	var ru *OverrideResult
	for i := range results {
		if results[i].Locale == "ru" {
			ru = &results[i]
		}
	}
	if ru == nil {
		t.Fatal("expected an OverrideResult for the unreadable ru.json — silence defeats the point of reporting")
	}
	if ru.Unreadable == "" {
		t.Fatal("Unreadable must name why the file could not be read")
	}
	if ru.Applied != 0 || len(ru.Rejected) != 0 {
		t.Fatalf("an unreadable file should apply nothing: %+v", ru)
	}
}

// Valid JSON that isn't an object (a bare array, a bare string, ...) is the
// neighbouring case to a syntax error: it must be skipped and reported the
// same way, not treated as some other special case.
func TestMergeOverrides_NonObjectJSONIsSkippedButReported(t *testing.T) {
	base := load(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"array", `["not", "an", "object"]`},
		{"string", `"hello"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "ru.json"), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			merged, _, results := MergeOverrides(base, os.DirFS(dir), ".", nil)
			if got := merged.T("ru", "projects.title", nil); got != "Проекты" {
				t.Fatalf("ru was disturbed by non-object JSON: %q", got)
			}
			if len(results) != 1 || results[0].Locale != "ru" || results[0].Unreadable == "" {
				t.Fatalf("results = %+v, want one reported, unreadable ru entry", results)
			}
		})
	}
}

// A filename that isn't a plausible locale code must not become a
// selectable phantom locale — but a real, unusually-shaped BCP-47 tag must
// still be accepted rather than falsely rejected, since discarding a
// translator's real work silently is worse than a phantom locale.
func TestMergeOverrides_LocaleCodeValidation(t *testing.T) {
	base := load(t)

	t.Run("an unusual but valid tag is accepted", func(t *testing.T) {
		for _, code := range []string{"zh-Hant", "sr-Latn-RS", "pt_BR"} {
			t.Run(code, func(t *testing.T) {
				dir := t.TempDir()
				writeOverrideJSON(t, dir, code+".json", map[string]any{
					"$meta":    map[string]any{"code": code, "name": code, "nativeName": code},
					"projects": map[string]any{"title": "X"},
				})
				merged, _, results := MergeOverrides(base, os.DirFS(dir), ".", nil)
				for _, r := range results {
					if r.Unreadable != "" {
						t.Fatalf("valid tag %q was rejected: %+v", code, r)
					}
				}
				found := false
				for _, m := range merged.Locales() {
					// language.Parse normalizes "pt_BR" to "pt-BR" — the
					// locale still needs to be selectable under some code,
					// just not necessarily byte-identical to the filename.
					if strings.EqualFold(m.Code, code) || strings.EqualFold(m.Code, strings.ReplaceAll(code, "_", "-")) {
						found = true
					}
				}
				if !found {
					t.Fatalf("valid tag %q did not become selectable: %+v", code, merged.Locales())
				}
			})
		}
	})

	t.Run("a clearly-non-locale name is skipped and reported, not made selectable", func(t *testing.T) {
		dir := t.TempDir()
		writeOverrideJSON(t, dir, "README-notes-2024.json", map[string]any{
			"projects": map[string]any{"title": "should never apply"},
		})

		merged, fragments, results := MergeOverrides(base, os.DirFS(dir), ".", nil)

		for _, m := range merged.Locales() {
			if m.Code == "README-notes-2024" {
				t.Fatal("a non-locale filename must not become a selectable phantom locale")
			}
		}
		if fragments["README-notes-2024"] != nil {
			t.Fatal("a non-locale filename must not produce a bridge fragment")
		}
		if len(results) != 1 || results[0].Locale != "README-notes-2024" || results[0].Unreadable == "" {
			t.Fatalf("results = %+v, want one reported, unreadable entry naming why", results)
		}
	})
}

// fakeDirEntry and unreadableFS exist to exercise the fs.ReadFile failure
// branch specifically (as opposed to json.Unmarshal failing on content that
// was read fine) — a real permissions error is unreliable to provoke
// portably, so this fakes the listing succeeding while the read itself
// fails, the way it would for a file the OS refuses to open.
type fakeDirEntry struct{ name string }

func (e fakeDirEntry) Name() string             { return e.name }
func (fakeDirEntry) IsDir() bool                { return false }
func (fakeDirEntry) Type() fs.FileMode          { return 0 }
func (fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

type unreadableFS struct{ name string }

func (f unreadableFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

func (f unreadableFS) ReadDir(string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{fakeDirEntry{name: f.name}}, nil
}

func (f unreadableFS) ReadFile(string) ([]byte, error) {
	return nil, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrPermission}
}

// A file fs.ReadFile itself fails on (not just a parse failure) is reported
// the same way as a JSON error.
func TestMergeOverrides_UnreadableFileReadErrorIsReported(t *testing.T) {
	base := load(t)
	_, _, results := MergeOverrides(base, unreadableFS{name: "ru.json"}, ".", nil)
	if len(results) != 1 || results[0].Locale != "ru" || results[0].Unreadable == "" {
		t.Fatalf("results = %+v, want one reported, unreadable ru entry", results)
	}
}

// This is the seam that shipped broken: a clean override with zero rejected
// keys is the ordinary, successful case, and append never runs for it, so
// OverrideResult.Rejected stays a nil slice. A nil slice is indistinguishable
// from a populated one in Go — every check above this line reads
// len(results[0].Rejected) or range's over it, and both treat nil and []
// identically — so no amount of testing MergeOverrides' return value in Go
// alone can catch what actually broke: nil marshals through encoding/json as
// the JSON literal `null`, not `[]`, and the frontend
// (LanguageSelector.tsx) does `activeOverride.rejected.length`, which throws
// on null. A test that builds an OverrideResult by hand and inspects the
// field never crosses encoding/json, so it can't see this — only marshalling
// the real return value can.
func TestMergeOverrides_CleanOverrideMarshalsRejectedAsEmptyArrayNotNull(t *testing.T) {
	base := load(t)
	dir := t.TempDir()
	writeOverrideJSON(t, dir, "ru.json", map[string]any{
		"projects": map[string]any{"title": "Проекты (правка)"},
	})

	_, _, results := MergeOverrides(base, os.DirFS(dir), ".", nil)
	if len(results) != 1 || results[0].Applied == 0 || len(results[0].Rejected) != 0 {
		t.Fatalf("results = %+v, want one clean, zero-rejection result", results)
	}

	b, err := json.Marshal(results[0])
	if err != nil {
		t.Fatalf("marshal OverrideResult: %v", err)
	}
	got := string(b)
	if strings.Contains(got, `"rejected":null`) {
		t.Fatalf("OverrideResult JSON has rejected:null, want rejected:[] — got %s", got)
	}
	if !strings.Contains(got, `"rejected":[]`) {
		t.Fatalf("OverrideResult JSON missing rejected:[] — got %s", got)
	}
}
