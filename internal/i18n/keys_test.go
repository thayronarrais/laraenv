package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The catalogue and the code have to agree in both directions: a key the code
// asks for must exist, and a key nobody asks for is dead weight a translator
// would otherwise translate.
//
// The two directions need different matchers, deliberately:
//
//   - "does the code ask for a key that doesn't exist" needs a NARROW
//     matcher: genuine call sites only, .T("..."), .labels("..."),
//     i18n.Errorf("...") and i18n.Wrapf(err, "..."). A false match on a
//     dot-shaped string that was never a key would fail the build for
//     nothing; missing a real call site is harmless here. Errorf/Wrapf are
//     the constructors internal/i18n's Error type exposes for translatable
//     errors (see error.go) — every errors.* key added anywhere in the
//     backend goes through one of the two, so both need to be as visible to
//     this guard as .T(...) already is.
//   - "is a key actually used" needs a BROAD matcher: does the key's exact
//     literal text appear anywhere in the source, not only at a call site.
//     internal/tray/tray.go's Relabel() passes its keys through a local
//     closure (set(item, "backend.tray.showWindow")) rather than calling
//     .T or .labels directly, so a call-site-only regex would report six of
//     the eight backend.tray.* keys as orphans on day one even though the
//     literal is right there in the file. Under-matching here breaks the
//     build for no reason; over-matching only ever spares a key.
var callRe = regexp.MustCompile(`\.(?:T|labels)\(\s*"([\w.]+)"|i18n\.(?:Errorf|Wrapf)\(\s*(?:[^,]*,\s*)?"([\w.]+)"`)
var literalRe = regexp.MustCompile(`["` + "`" + `]([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+)["` + "`" + `]`)

func catalogueKeys(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "langs", "en.json"))
	if err != nil {
		t.Fatalf("read en.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse en.json: %v", err)
	}
	keys := map[string]bool{}
	var walk func(node any, prefix string)
	plurals := map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}
	walk = func(node any, prefix string) {
		m, ok := node.(map[string]any)
		if !ok {
			keys[prefix] = true
			return
		}
		allPlural := len(m) > 0
		for k := range m {
			if !plurals[k] {
				allPlural = false
				break
			}
		}
		if allPlural {
			keys[prefix] = true
			return
		}
		for k, v := range m {
			if k == "$meta" {
				continue
			}
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			walk(v, path)
		}
	}
	walk(doc, "")
	return keys
}

// goSources walks every non-test .go file under the module root, skipping
// directories that are not this module's own Go source.
func goSources(t *testing.T) []string {
	t.Helper()
	var files []string
	root := filepath.Join("..", "..")
	// .claude holds tooling scratch state, including nested git worktrees
	// (a locked one can sit under .claude/worktrees/<name> for another
	// in-flight session) — a full copy of the repo that must not be
	// double-scanned for catalogue keys.
	skip := map[string]bool{"node_modules": true, "frontend": true, ".git": true, "vendor": true, "build": true, ".claude": true}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// goCallSites returns keys seen at a genuine .T("...") or .labels("...")
// call site — the narrow matcher, used only for the missing-key direction.
func goCallSites(t *testing.T) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	for _, path := range goSources(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range callRe.FindAllSubmatch(raw, -1) {
			// Exactly one of the two alternatives matched, so exactly one of
			// these two capture groups is non-empty.
			if len(m[1]) > 0 {
				used[string(m[1])] = true
			}
			if len(m[2]) > 0 {
				used[string(m[2])] = true
			}
		}
	}
	return used
}

// goLiteralTexts returns every dot-shaped quoted literal appearing anywhere
// in the source — the broad matcher, used only for the orphan-key direction.
func goLiteralTexts(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, path := range goSources(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range literalRe.FindAllSubmatch(raw, -1) {
			found[string(m[1])] = true
		}
	}
	return found
}

func TestEveryGoKeyExists(t *testing.T) {
	defined := catalogueKeys(t)
	var missing []string
	for key := range goCallSites(t) {
		if !defined[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("T()/labels() calls with no key in langs/en.json:\n  %s", strings.Join(missing, "\n  "))
	}
}

func TestNoOrphanBackendKeys(t *testing.T) {
	literal := goLiteralTexts(t)
	var orphans []string
	for key := range catalogueKeys(t) {
		if !strings.HasPrefix(key, "backend.") && !strings.HasPrefix(key, "errors.") {
			continue // frontend keys; the Vitest guard covers those
		}
		if !literal[key] {
			orphans = append(orphans, key)
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		t.Fatalf("keys in langs/en.json nobody uses:\n  %s", strings.Join(orphans, "\n  "))
	}
}

// placeholderRe finds {{name}} tokens in a catalogue value.
var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// extractTokens is the set of {{name}} tokens appearing in s.
func extractTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

// catalogueValueTokens maps every leaf key in en.json to the {{name}} tokens
// its English value uses. A plural entry's tokens are the union across every
// category, since any category can render depending on count.
func catalogueValueTokens(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "langs", "en.json"))
	if err != nil {
		t.Fatalf("read en.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse en.json: %v", err)
	}
	plurals := map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}
	tokens := map[string]map[string]bool{}
	var walk func(node any, prefix string)
	walk = func(node any, prefix string) {
		switch v := node.(type) {
		case string:
			tokens[prefix] = extractTokens(v)
		case map[string]any:
			allPlural := len(v) > 0
			for k := range v {
				if !plurals[k] {
					allPlural = false
					break
				}
			}
			if allPlural {
				set := map[string]bool{}
				for _, val := range v {
					if s, ok := val.(string); ok {
						for tok := range extractTokens(s) {
							set[tok] = true
						}
					}
				}
				tokens[prefix] = set
				return
			}
			for k, vv := range v {
				if k == "$meta" {
					continue
				}
				path := k
				if prefix != "" {
					path = prefix + "." + k
				}
				walk(vv, path)
			}
		}
	}
	walk(doc, "")
	return tokens
}

// errVarCall is one i18n.Errorf/i18n.Wrapf call site whose key is a string
// literal (so it can be checked statically) and whose vars argument is
// either "nil" or a map[string]... composite literal (so its keys can be
// read without a full Go parser).
type errVarCall struct {
	fn   string // "Errorf" or "Wrapf"
	key  string
	vars map[string]bool
}

var errVarCallStart = regexp.MustCompile(`i18n\.(Errorf|Wrapf)\(`)

// splitTopLevelArgs splits a Go call's argument-list text on commas that are
// not nested inside (), {}, [] or a string/raw-string literal — exactly what
// distinguishes `map[string]any{"a": f(x, y)}` (one argument) from a literal
// top-level comma.
func splitTopLevelArgs(s string) []string {
	var args []string
	var buf strings.Builder
	depth := 0
	inString := false
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			buf.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote != '`' {
				escaped = true
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '`':
			inString = true
			quote = c
			buf.WriteByte(c)
		case '(', '{', '[':
			depth++
			buf.WriteByte(c)
		case ')', '}', ']':
			depth--
			buf.WriteByte(c)
		case ',':
			if depth == 0 {
				args = append(args, buf.String())
				buf.Reset()
				continue
			}
			buf.WriteByte(c)
		default:
			buf.WriteByte(c)
		}
	}
	if strings.TrimSpace(buf.String()) != "" {
		args = append(args, buf.String())
	}
	return args
}

// extractCallArgs returns the text between the '(' at src[openParen] and its
// matching ')', tracking nested brackets and string literals the same way
// splitTopLevelArgs does, so a nested call like filepath.Base(bin) or a
// string argument containing a stray paren doesn't end the scan early.
func extractCallArgs(src []byte, openParen int) string {
	depth := 0
	inString := false
	var quote byte
	escaped := false
	start := openParen + 1
	for i := openParen; i < len(src); i++ {
		c := src[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote != '`' {
				escaped = true
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '`':
			inString = true
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return string(src[start:i])
			}
		}
	}
	return string(src[start:])
}

// mapLiteralKeyRe finds the quoted keys of a map[string]... composite
// literal, e.g. {"path": p, "name": n} -> "path", "name".
var mapLiteralKeyRe = regexp.MustCompile(`"(\w+)"\s*:`)

// errVarCallsIn scans one file's source for i18n.Errorf/i18n.Wrapf call
// sites and, for the ones whose key and vars are both literals a regex can
// read, returns the key and the set of var names actually passed.
func errVarCallsIn(raw []byte) []errVarCall {
	var calls []errVarCall
	for _, loc := range errVarCallStart.FindAllSubmatchIndex(raw, -1) {
		fn := string(raw[loc[2]:loc[3]])
		openParen := loc[1] - 1 // errVarCallStart ends in "("
		argsText := extractCallArgs(raw, openParen)
		args := splitTopLevelArgs(argsText)

		keyIdx := 0
		if fn == "Wrapf" {
			keyIdx = 1 // Wrapf(err, key, vars) — Errorf(key, vars)
		}
		varsIdx := keyIdx + 1
		if keyIdx >= len(args) || varsIdx >= len(args) {
			continue // not the 2/3-arg shape this scan understands
		}

		keyArg := strings.TrimSpace(args[keyIdx])
		if len(keyArg) < 2 || keyArg[0] != '"' || keyArg[len(keyArg)-1] != '"' {
			continue // dynamic key (a variable, a Sprintf, ...) — can't check statically
		}
		key := keyArg[1 : len(keyArg)-1]

		varsArg := strings.TrimSpace(args[varsIdx])
		var vars map[string]bool
		switch {
		case varsArg == "nil":
			vars = map[string]bool{}
		case strings.HasPrefix(varsArg, "map["):
			vars = map[string]bool{}
			for _, m := range mapLiteralKeyRe.FindAllStringSubmatch(varsArg, -1) {
				vars[m[1]] = true
			}
		default:
			continue // vars built elsewhere (a variable) — can't check statically
		}

		calls = append(calls, errVarCall{fn: fn, key: key, vars: vars})
	}
	return calls
}

// TestErrorfWrapfVarsMatchCatalogue guards the mismatch TestEveryGoKeyExists
// and TestNoOrphanBackendKeys cannot see: both only check that a key exists
// and is used, never that the vars a call site passes actually cover the
// {{tokens}} the key's English value interpolates. A key whose value holds
// {{path}}, called with {"name": ...}, renders the literal string "{{path}}"
// on the user's screen — every other guard, and every test, stays green.
func TestErrorfWrapfVarsMatchCatalogue(t *testing.T) {
	values := catalogueValueTokens(t)
	var problems []string
	for _, path := range goSources(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range errVarCallsIn(raw) {
			want, ok := values[call.key]
			if !ok {
				continue // TestEveryGoKeyExists already reports an undefined key
			}
			var missing []string
			for tok := range want {
				if !call.vars[tok] {
					missing = append(missing, tok)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				problems = append(problems, fmt.Sprintf("%s: i18n.%s(%q, ...) doesn't pass {{%s}}, which its English value uses",
					path, call.fn, call.key, strings.Join(missing, "}}, {{")))
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("call-site vars don't cover the catalogue value's placeholders:\n  %s", strings.Join(problems, "\n  "))
	}
}
