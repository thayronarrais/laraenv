//go:build windows

package atomicfile

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func nativeOpen(dir *os.File, name string, access, disposition, options uint32) (*os.File, error) {
	return nativeOpenSecurity(dir, name, access, disposition, options, nil)
}

func nativeOpenSecurity(dir *os.File, name string, access, disposition, options uint32, descriptor *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	ntName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(dir.Fd()), ObjectName: ntName, Attributes: windows.OBJ_DONT_REPARSE, SecurityDescriptor: descriptor}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var iosb windows.IO_STATUS_BLOCK
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE, &oa, &iosb, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		var status windows.NTStatus
		if errors.As(err, &status) {
			err = status.Errno()
		}
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(handle), name), nil
}

func openDirectory(root *os.Root, name string) (*os.File, error) {
	anchor, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if name == "." {
		return anchor, nil
	}
	defer anchor.Close()
	return nativeOpen(anchor, strings.ReplaceAll(name, "/", "\\"), windows.FILE_GENERIC_READ, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE)
}

func openTarget(dir *os.File, leaf string) (*os.File, error) {
	return nativeOpen(dir, leaf, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.READ_CONTROL, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
}

type permissionState struct {
	mode           os.FileMode
	descriptor     *windows.SECURITY_DESCRIPTOR
	acl            *windows.ACL
	protected      bool
	representation string
}

func readPermissions(file *os.File) (permissionState, error) {
	var p permissionState
	info, err := file.Stat()
	if err != nil {
		return p, err
	}
	p.mode = info.Mode().Perm()
	p.descriptor, err = windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return p, err
	}
	control, _, err := p.descriptor.Control()
	if err != nil {
		return p, err
	}
	if control&windows.SE_DACL_PRESENT == 0 {
		return p, errors.New("absent DACL cannot be preserved")
	}
	p.acl, _, err = p.descriptor.DACL()
	if err != nil {
		return p, err
	}
	p.protected = control&windows.SE_DACL_PROTECTED != 0
	// SDDL preserves ACE order, masks, inheritance flags and null versus empty
	// ACLs. The query includes only the DACL (not owner, group or SACL).
	p.representation, err = DACLString(p.descriptor)
	if err != nil {
		return p, err
	}
	return p, nil
}
func (p permissionState) equal(other permissionState) bool {
	return p.mode == other.mode && p.protected == other.protected && p.representation == other.representation
}

type rootStage struct {
	writer, retained *os.File
	parent           *os.Root
	name             string
}

func newRootStage(dir *os.File, parent *os.Root, name string, permissions permissionState) (*rootStage, error) {
	descriptor, err := windows.SecurityDescriptorFromString(permissions.representation)
	if err != nil {
		return nil, err
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return nil, err
	}
	if control&windows.SE_DACL_PRESENT == 0 {
		return nil, errors.New("cannot create a stage without a captured DACL")
	}
	// Exclude broader parent access from the first instant the file exists;
	// a reader must never retain a handle opened before preserve restricts it.
	if err := descriptor.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
		return nil, err
	}
	writer, err := nativeOpenSecurity(dir, name, windows.FILE_GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE, descriptor)
	runtime.KeepAlive(descriptor)
	if err != nil {
		return nil, err
	}
	return retainRootStage(writer, parent, name, duplicateStageHandle)
}

func duplicateStageHandle(writer *os.File) (*os.File, error) {
	var copy windows.Handle
	err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(writer.Fd()), windows.CurrentProcess(), &copy, 0, false, windows.DUPLICATE_SAME_ACCESS)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(copy), writer.Name()), nil
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
func (s *rootStage) preserve(p permissionState) error {
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if p.protected {
		flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	err := windows.SetSecurityInfo(windows.Handle(s.retained.Fd()), windows.SE_FILE_OBJECT, flags, nil, nil, p.acl, nil)
	runtime.KeepAlive(p.descriptor)
	return err
}
func (s *rootStage) verify(p permissionState) error {
	current, err := readPermissions(s.retained)
	if err != nil {
		return err
	}
	same, err := SameStagedDACL(p.representation, current.representation)
	if err != nil {
		return err
	}
	if current.protected != p.protected || !same {
		return errors.New("staged DACL differs from original")
	}
	return nil
}
func (s *rootStage) applyMode(p permissionState) error { return s.retained.Chmod(p.mode) }
func (s *rootStage) release() error                    { return s.retained.Close() }
func (s *rootStage) cleanup() error {
	if err := s.parent.Remove(s.name); err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// DELETE was granted at exclusive creation, before the original ACL was
	// copied. This handle survives restrictive DACLs that deny fresh opens.
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.SetFileInformationByHandle(windows.Handle(s.retained.Fd()), windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	if chmodErr := s.retained.Chmod(0600); chmodErr != nil {
		return errors.Join(err, chmodErr)
	}
	disposition := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(s.retained.Fd()), windows.FileDispositionInfo, &disposition, 1)
}
