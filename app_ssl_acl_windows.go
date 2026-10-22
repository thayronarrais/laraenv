//go:build windows

package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"lara4ever/internal/atomicfile"
)

// DACL-only SDDL owns its memory and preserves ACE ordering, inherited flags,
// protection, and the distinction between null and empty DACLs.
type sslFilePermissions struct{ dacl string }

func (p sslFilePermissions) equal(other sslFilePermissions) bool { return p == other }

func readSSLFilePermissions(file *os.File) (sslFilePermissions, error) {
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return sslFilePermissions{}, err
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return sslFilePermissions{}, err
	}
	if control&windows.SE_DACL_PRESENT == 0 {
		return sslFilePermissions{}, errors.New("cannot snapshot an absent SSL file DACL")
	}
	sddl, err := atomicfile.DACLString(descriptor)
	if err != nil {
		return sslFilePermissions{}, err
	}
	return sslFilePermissions{dacl: sddl}, nil
}

func sslOpenRelative(parent *os.File, name string, access, disposition uint32) (*os.File, error) {
	return sslOpenRelativeSecurity(parent, name, access, disposition, nil)
}

func sslOpenRelativeSecurity(parent *os.File, name string, access, disposition uint32, descriptor *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	ntName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory: windows.Handle(parent.Fd()), ObjectName: ntName, Attributes: windows.OBJ_DONT_REPARSE,
		SecurityDescriptor: descriptor,
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var status windows.IO_STATUS_BLOCK
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE, &attributes, &status, nil,
		windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		disposition, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		var ntStatus windows.NTStatus
		if errors.As(err, &ntStatus) {
			err = ntStatus.Errno()
		}
		return nil, &os.PathError{Op: "open SSL file", Path: name, Err: err}
	}
	return os.NewFile(uintptr(handle), name), nil
}

func newSSLFileStage(parent *os.File, name string, permissions sslFilePermissions) (*os.File, error) {
	if permissions.dacl == "" {
		return nil, errors.New("SSL rollback has no captured DACL")
	}
	descriptor, err := windows.SecurityDescriptorFromString(permissions.dacl)
	if err != nil {
		return nil, err
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return nil, err
	}
	if control&windows.SE_DACL_PRESENT == 0 {
		return nil, errors.New("SSL rollback DACL is absent")
	}
	// Protect at creation so no principal can open an empty, broadly inherited
	// stage and retain a read handle after we apply the captured permissions.
	// The original inheritance state is restored and checked before writing.
	if err := descriptor.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
		return nil, err
	}
	stage, err := sslOpenRelativeSecurity(parent, name,
		windows.FILE_GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
		windows.FILE_CREATE, descriptor)
	runtime.KeepAlive(descriptor)
	return stage, err
}

// Pin the parent and reject a final directory junction. Child opens are native
// relative opens with OBJ_DONT_REPARSE, including while deleting new files.
func sslOpenParent(path string) (*os.File, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	leaf := filepath.Base(absolute)
	if leaf == "." || leaf == ".." || strings.ContainsAny(leaf, ":\\/\x00") {
		return nil, "", errors.New("invalid SSL file name")
	}
	name, err := windows.UTF16PtrFromString(filepath.Dir(absolute))
	if err != nil {
		return nil, "", err
	}
	handle, err := windows.CreateFile(name, windows.FILE_GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, "", &os.PathError{Op: "open SSL directory", Path: filepath.Dir(absolute), Err: err}
	}
	parent := os.NewFile(uintptr(handle), filepath.Dir(absolute))
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		parent.Close()
		return nil, "", err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		parent.Close()
		return nil, "", errors.New("SSL parent must be a directory without a reparse point")
	}
	return parent, leaf, nil
}

func snapshotSSLFilePlatform(path string) (sslFileSnapshot, error) {
	snapshot := sslFileSnapshot{routeFileSnapshot: routeFileSnapshot{path: path}}
	parent, leaf, err := sslOpenParent(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	defer parent.Close()
	file, err := sslOpenRelative(parent, leaf, windows.FILE_GENERIC_READ|windows.READ_CONTROL, windows.FILE_OPEN)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return snapshot, err
	}
	if !info.Mode().IsRegular() {
		return snapshot, errors.New("SSL snapshot requires a regular file")
	}
	snapshot.permissions, err = readSSLFilePermissions(file)
	if err != nil {
		return snapshot, err
	}
	snapshot.data, err = io.ReadAll(file)
	if err != nil {
		return snapshot, err
	}
	after, err := readSSLFilePermissions(file)
	if err != nil {
		return snapshot, err
	}
	if !snapshot.permissions.equal(after) {
		return snapshot, errors.New("SSL file DACL changed during snapshot")
	}
	snapshot.mode, snapshot.exists = info.Mode(), true
	return snapshot, nil
}

func setSSLFilePermissions(file *os.File, permissions sslFilePermissions) error {
	if permissions.dacl == "" {
		return errors.New("SSL rollback has no captured DACL")
	}
	descriptor, err := windows.SecurityDescriptorFromString(permissions.dacl)
	if err != nil {
		return err
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PRESENT == 0 {
		return errors.New("SSL rollback DACL is absent")
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	err = windows.SetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, flags, nil, nil, acl, nil)
	runtime.KeepAlive(descriptor)
	return err
}

func verifySSLFilePermissions(file *os.File, snapshot sslFileSnapshot) error {
	current, err := readSSLFilePermissions(file)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	same, err := atomicfile.SameStagedDACL(snapshot.permissions.dacl, current.dacl)
	if err != nil {
		return err
	}
	if !same || info.Mode().Perm() != snapshot.mode.Perm() {
		return errors.New("staged SSL permissions differ from captured permissions")
	}
	return nil
}

func sslDeleteByHandle(file *os.File) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	if err := windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags))); err == nil {
		return nil
	}
	if err := file.Chmod(0600); err != nil {
		return err
	}
	delete := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo, &delete, 1)
}

func restoreSSLFilePlatform(snapshot sslFileSnapshot) (err error) {
	parent, leaf, err := sslOpenParent(snapshot.path)
	if errors.Is(err, os.ErrNotExist) && !snapshot.exists {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	if !snapshot.exists {
		file, err := sslOpenRelative(parent, leaf, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES, windows.FILE_OPEN)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer file.Close()
		return sslDeleteByHandle(file)
	}
	root, err := os.OpenRoot(filepath.Dir(snapshot.path))
	if err != nil {
		return err
	}
	defer root.Close()
	if err := sslSameParent(parent, root); err != nil {
		return err
	}
	temp := ".laraenv-ssl-" + rand.Text() + ".tmp"
	stage, err := newSSLFileStage(parent, temp, snapshot.permissions)
	if err != nil {
		return err
	}
	replaced := false
	defer func() {
		if !replaced {
			// The handle was granted DELETE before applying a possibly restrictive
			// DACL. Fresh opens must not be needed to remove private staging data.
			err = errors.Join(err, sslDeleteByHandle(stage))
		}
		err = errors.Join(err, stage.Close())
	}()
	if err := setSSLFilePermissions(stage, snapshot.permissions); err != nil {
		return err
	}
	if err := stage.Chmod(snapshot.mode.Perm()); err != nil {
		return err
	}
	// No private key bytes may be written before the original DACL and mode
	// have been applied and verified. Inheritance differences fail closed.
	if err := verifySSLFilePermissions(stage, snapshot); err != nil {
		return err
	}
	n, err := stage.Write(snapshot.data)
	if err != nil {
		return err
	}
	if n != len(snapshot.data) {
		return io.ErrShortWrite
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := verifySSLFilePermissions(stage, snapshot); err != nil {
		return err
	}
	// Check that the named parent still identifies our pinned directory and
	// that the replacement target has not become a link or special file.
	currentParent, _, err := sslOpenParent(snapshot.path)
	if err != nil {
		return err
	}
	defer currentParent.Close()
	before, err := parent.Stat()
	if err != nil {
		return err
	}
	current, err := currentParent.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, current) {
		return errors.New("SSL parent changed during rollback")
	}
	target, err := sslOpenRelative(parent, leaf, windows.FILE_READ_ATTRIBUTES, windows.FILE_OPEN)
	if err == nil {
		target.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(temp, leaf); err != nil {
		return err
	}
	replaced = true
	return nil
}

func sslSameParent(parent *os.File, root *os.Root) error {
	left, err := parent.Stat()
	if err != nil {
		return err
	}
	right, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(left, right) {
		return fmt.Errorf("SSL directory changed while opening rollback root")
	}
	return nil
}
