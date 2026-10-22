package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WritePreservingPermissions saves an existing regular file through the
// pinned-directory lifecycle used by document saves. Its permissions (including
// a Windows DACL) are copied and verified before any data reaches staging;
// failures never fall back to an unrestricted replacement. mode applies only
// when creating a file that does not exist yet.
func WritePreservingPermissions(path string, data []byte, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Write(path, data, mode)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot preserve permissions of a non-regular file")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return ReplaceInRoot(root, filepath.Base(path), data, nil)
}

// Write replaces path with data using a temporary file in the same directory.
// The platform-specific replace operation supports replacing an existing file.
func Write(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".laraenv-state-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replace(tempPath, path)
}
