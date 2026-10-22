package workspacefiles

import "os"

type DirectoryIdentity struct{ CanonicalPath, PhysicalID string }

// OpenDirectoryIdentity is for backend-discovered paths. The caller owns root.
func OpenDirectoryIdentity(path string) (*os.Root, DirectoryIdentity, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, DirectoryIdentity{}, err
	}
	canonical, id, err := directoryIdentity(root, path)
	if err != nil {
		root.Close()
		return nil, DirectoryIdentity{}, err
	}
	return root, DirectoryIdentity{CanonicalPath: canonical, PhysicalID: id}, nil
}

// OpenedFileIdentity returns the identity of the retained handle, never a stat
// of a newly resolved path. Some Unix platforms do not return a canonical path.
func OpenedFileIdentity(file *os.File) (canonical, id string, err error) { return handleIdentity(file) }
func ValidRelativePath(name string, allowRoot bool) bool                 { return validRelative(name, allowRoot) }
