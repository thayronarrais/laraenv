package sitedoctor

import (
	"os"
	"path/filepath"
)

func storageFinding(root string, repairReady bool) Finding {
	path := filepath.Join(root, "public", "storage")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		f := finding("storage_link", "optional", "storage_link_missing")
		if repairReady {
			f.FixID = f.ID
		}
		return f
	} else if err != nil {
		return finding("storage_link", "optional", "storage_link_unreadable")
	}

	// Readlink recognizes both symbolic links and Windows junctions. Junctions
	// are not ModeSymlink in Go 1.23+, and an ordinary directory is never a link.
	if _, err := os.Readlink(path); err != nil {
		if os.IsPermission(err) {
			return finding("storage_link", "optional", "storage_link_unreadable")
		}
		return finding("storage_link", "optional", "storage_link_conflict")
	}

	// Stat follows junctions; EvalSymlinks does not reliably resolve them.
	// Compare directory identity rather than spelling or reparse-point flags.
	actual, actualErr := os.Stat(path)
	expected, expectedErr := os.Stat(filepath.Join(root, "storage", "app", "public"))
	if os.IsPermission(actualErr) || os.IsPermission(expectedErr) {
		return finding("storage_link", "optional", "storage_link_unreadable")
	}
	if actualErr != nil || expectedErr != nil || !actual.IsDir() || !expected.IsDir() || !os.SameFile(actual, expected) {
		return finding("storage_link", "optional", "storage_link_conflict")
	}
	return finding("storage_link", "pass", "storage_link_ok")
}
