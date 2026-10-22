//go:build !windows

package atomicfile

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func openDirectory(root *os.Root, name string) (*os.File, error) {
	current, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if name == "." {
		return current, nil
	}
	for _, part := range strings.Split(name, "/") {
		fd, err := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		current.Close()
		if err != nil {
			return nil, &os.PathError{Op: "open", Path: part, Err: err}
		}
		current = os.NewFile(uintptr(fd), part)
	}
	return current, nil
}
func openTarget(dir *os.File, leaf string) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), leaf, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: leaf, Err: err}
	}
	return os.NewFile(uintptr(fd), leaf), nil
}

type permissionState struct{ mode os.FileMode }

func readPermissions(file *os.File) (permissionState, error) {
	info, err := file.Stat()
	if err != nil {
		return permissionState{}, err
	}
	return permissionState{info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)}, nil
}
func (p permissionState) equal(other permissionState) bool { return p == other }

type rootStage struct {
	writer, retained *os.File
	parent           *os.Root
	name             string
}

func newRootStage(dir *os.File, parent *os.Root, name string, _ permissionState) (*rootStage, error) {
	writer, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	return retainRootStage(writer, parent, name, duplicateStageHandle)
}

func duplicateStageHandle(writer *os.File) (*os.File, error) {
	fd, err := unix.Dup(int(writer.Fd()))
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), writer.Name()), nil
}
func (s *rootStage) Write(p []byte) (int, error) { return s.writer.Write(p) }
func (s *rootStage) Sync() error                 { return s.writer.Sync() }
func (s *rootStage) Close() error {
	if s.writer == nil {
		return nil
	}
	err := s.writer.Close()
	s.writer = nil
	return err
}
func (s *rootStage) preserve(p permissionState) error { return s.retained.Chmod(p.mode) }
func (s *rootStage) verify(p permissionState) error {
	now, err := readPermissions(s.retained)
	if err != nil {
		return err
	}
	if !p.equal(now) {
		return errors.New("staged permissions differ from original")
	}
	return nil
}
func (s *rootStage) applyMode(p permissionState) error { return s.retained.Chmod(p.mode) }
func (s *rootStage) release() error                    { return s.retained.Close() }
func (s *rootStage) cleanup() error {
	err := s.parent.Remove(s.name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
