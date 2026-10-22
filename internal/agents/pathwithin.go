package agents

import (
	"path/filepath"
	"strings"
)

// PathWithin reports whether candidate is root itself or a descendant of it.
// Both paths are normalised the same way as project keys, so drive-letter case
// and slash direction never matter on Windows.
func PathWithin(root, candidate string) bool {
	root = NormalizeProjectPath(root)
	candidate = NormalizeProjectPath(candidate)
	if root == "" || candidate == "" {
		return false
	}
	if root == candidate {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
