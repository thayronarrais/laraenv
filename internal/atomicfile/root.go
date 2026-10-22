package atomicfile

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// ErrChanged means an identity or permission changed while staging a save.
var ErrChanged = errors.New("file or parent changed during save")

// ReplaceInRoot replaces an existing regular file in a pinned parent directory.
// beforeReplace must check the caller's revision against that parent and leaf.
// This is not a compare-and-swap against external writers, nor a promise of
// power-loss durability. A moved open directory remains the same directory.
func ReplaceInRoot(root *os.Root, relativePath string, data []byte, beforeReplace func(parent *os.Root, leaf string) error) error {
	if root == nil || !validRootPath(relativePath) {
		return fmt.Errorf("invalid relative file path")
	}
	parentPath, leaf := path.Split(relativePath)
	parentPath = strings.TrimSuffix(parentPath, "/")
	if parentPath == "" {
		parentPath = "."
	}
	dir, err := openDirectory(root, parentPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	parent, err := root.OpenRoot(parentPath)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := sameDirectory(parent, dir); err != nil {
		return err
	}
	return replaceInParent(root, parentPath, parent, dir, leaf, data, beforeReplace)
}

// replaceInParent owns the file lifecycle after the parent's native no-link
// validation and Root identity comparison. Every file operation stays pinned.
func replaceInParent(root *os.Root, parentPath string, parent *os.Root, dir *os.File, leaf string, data []byte, beforeReplace func(*os.Root, string) error) error {
	original, err := openTarget(dir, leaf)
	if err != nil {
		return err
	}
	defer original.Close()
	originalInfo, err := original.Stat()
	if err != nil {
		return err
	}
	if !originalInfo.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if originalInfo.Mode().Perm()&0222 == 0 {
		return os.ErrPermission
	}
	permissions, err := readPermissions(original)
	if err != nil {
		return err
	}
	temp := ".laraenv-document-" + rand.Text() + ".tmp"
	stage, err := newRootStage(dir, parent, temp, permissions)
	if err != nil {
		return err
	}
	validate := func() error {
		currentDir, err := openDirectory(root, parentPath)
		if err != nil {
			return err
		}
		defer currentDir.Close()
		oldDirInfo, err := dir.Stat()
		if err != nil {
			return err
		}
		newDirInfo, err := currentDir.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(oldDirInfo, newDirInfo) {
			return ErrChanged
		}
		if err := sameDirectory(parent, dir); err != nil {
			return err
		}
		current, err := openTarget(dir, leaf)
		if err != nil {
			return err
		}
		defer current.Close()
		info, err := current.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !os.SameFile(originalInfo, info) {
			return ErrChanged
		}
		now, err := readPermissions(current)
		if err != nil {
			return err
		}
		if !permissions.equal(now) {
			return ErrChanged
		}
		return nil
	}
	return replaceStaged(parent, leaf, temp, data, permissions, stage, func() error {
		if beforeReplace != nil {
			if err := beforeReplace(parent, leaf); err != nil {
				return err
			}
		}
		return validate()
	})
}

// The lifecycle interface separates platform handle/permission handling from
// the common write, flush and replacement sequence. It has no exported knobs.
type stagedFile interface {
	io.Writer
	Sync() error
	Close() error
	preserve(permissionState) error
	verify(permissionState) error
	applyMode(permissionState) error
	cleanup() error
	release() error
}

// retainRootStage transfers exclusive creation's writer into the staged
// lifecycle after acquiring its independent cleanup handle.
func retainRootStage(writer *os.File, parent *os.Root, name string, duplicate func(*os.File) (*os.File, error)) (*rootStage, error) {
	retained, err := duplicate(writer)
	if err != nil {
		// Duplication failed, but exclusive creation already granted the writer
		// its cleanup rights. Use that handle before closing it; on Windows a
		// fresh DELETE open may now be denied by the inherited or changed DACL.
		stage := &rootStage{retained: writer, parent: parent, name: name}
		cleanupErr := stage.cleanup()
		closeErr := writer.Close()
		return nil, errors.Join(err, cleanupErr, closeErr)
	}
	return &rootStage{writer: writer, retained: retained, parent: parent, name: name}, nil
}

func replaceStaged(parent *os.Root, leaf, temp string, data []byte, permissions permissionState, stage stagedFile, before func() error) (err error) {
	replaced := false
	defer func() {
		// Closing the writer does not discard the retained cleanup handle.
		closeErr := stage.Close()
		if !replaced {
			err = errors.Join(err, closeErr, stage.cleanup())
		}
		err = errors.Join(err, stage.release())
	}()
	if err = stage.preserve(permissions); err != nil {
		return err
	}
	if err = stage.verify(permissions); err != nil {
		return err
	}
	n, err := stage.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if err = stage.Sync(); err != nil {
		return err
	}
	if err = stage.applyMode(permissions); err != nil {
		return err
	}
	if err = stage.Close(); err != nil {
		return err
	}
	if err = before(); err != nil {
		return err
	}
	if err = stage.verify(permissions); err != nil {
		return err
	}
	if err = parent.Rename(temp, leaf); err != nil {
		return err
	}
	replaced = true
	return nil
}

func sameDirectory(parent *os.Root, dir *os.File) error {
	a, err := parent.Stat(".")
	if err != nil {
		return err
	}
	b, err := dir.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return ErrChanged
	}
	return nil
}

func validRootPath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}
