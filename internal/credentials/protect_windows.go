//go:build windows

package credentials

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Deliberately omit CRYPTPROTECT_LOCAL_MACHINE: a portable directory must
// not make a saved password readable by another Windows account.
func protect(data []byte) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windows.DataBlob
	err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	runtime.KeepAlive(data)
	if err != nil {
		return nil, err
	}
	return copyAndFreeBlob(output), nil
}

func unprotect(data []byte) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windows.DataBlob
	err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	runtime.KeepAlive(data)
	if err != nil {
		return nil, err
	}
	return copyAndFreeBlob(output), nil
}

func copyAndFreeBlob(blob windows.DataBlob) []byte {
	if blob.Data == nil {
		return nil
	}
	native := unsafe.Slice(blob.Data, int(blob.Size))
	result := append([]byte(nil), native...)
	clear(native)
	_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(blob.Data))))
	return result
}
