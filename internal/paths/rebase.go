package paths

import (
	"path/filepath"
	"strings"
)

// SameRoot reports whether two paths name the same install root.
//
// Windows ignores letter case, so C:\LaraEnv and C:\laraenv are one and the
// same directory. Comparing them as plain strings would make an environment
// look relocated onto itself and rewrite every stored path for nothing.
func SameRoot(a, b string) bool {
	return normalizeRoot(a) == normalizeRoot(b)
}

// Rebase moves p from oldRoot to newRoot, reporting whether it applied.
//
// Only paths that actually live under oldRoot are moved: a project the user
// keeps somewhere else is not part of the environment and must survive the
// move untouched. Anything else is returned unchanged.
//
// The replacement copies the case style of what it replaced. Some stored
// values are lowercased keys that other data is looked up by, and handing
// them a mixed-case root would orphan every record pointing at them.
func Rebase(oldRoot, newRoot, p string) (string, bool) {
	if p == "" {
		return p, false
	}
	oldNorm := normalizeRoot(oldRoot)
	pNorm := normalizeRoot(p)

	if pNorm != oldNorm && !strings.HasPrefix(pNorm, oldNorm+"/") {
		return p, false
	}

	// Length is measured on the normalized form, which preserves the original
	// rune count — only separators and case change.
	rest := p[len(oldRoot):]
	if strings.HasSuffix(oldRoot, string(filepath.Separator)) || strings.HasSuffix(oldRoot, "/") {
		rest = string(filepath.Separator) + rest
	}

	head := p[:len(oldRoot)]
	if head == strings.ToLower(head) {
		newRoot = strings.ToLower(newRoot)
	}
	return newRoot + rest, true
}

// normalizeRoot lowercases, unifies separators and drops any trailing one, so
// two spellings of the same directory compare equal.
func normalizeRoot(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimRight(p, "/")
	return strings.ToLower(p)
}
