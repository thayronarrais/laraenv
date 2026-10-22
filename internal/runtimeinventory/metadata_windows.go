//go:build windows

package runtimeinventory

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func readExecutableMetadata(executable string) (Metadata, error) {
	size, err := windows.GetFileVersionInfoSize(executable, nil)
	if err != nil {
		return Metadata{}, err
	}
	if size == 0 || size > 16<<20 {
		return Metadata{}, errors.New("runtime version resource is unavailable")
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(executable, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return Metadata{}, err
	}
	var fixed *windows.VS_FIXEDFILEINFO
	var length uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&data[0]), `\`, unsafe.Pointer(&fixed), &length); err != nil || fixed == nil || length < uint32(unsafe.Sizeof(*fixed)) {
		return Metadata{}, errors.New("runtime version resource is invalid")
	}
	metadata := Metadata{Prerelease: fixed.FileFlags&fixed.FileFlagsMask&0x2 != 0}
	var translations *uint16
	var translationLength uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&translations), &translationLength) == nil && translations != nil && translationLength >= 4 && translationLength <= 4096 {
		pairs := unsafe.Slice(translations, int(translationLength/2))
		for i := 0; i+1 < len(pairs); i += 2 {
			block := fmt.Sprintf(`\StringFileInfo\%04x%04x\ProductVersion`, pairs[i], pairs[i+1])
			var value *uint16
			var valueLength uint32
			if windows.VerQueryValue(unsafe.Pointer(&data[0]), block, unsafe.Pointer(&value), &valueLength) == nil && value != nil && valueLength > 0 && valueLength <= 1024 {
				metadata.FullVersion = strings.TrimSpace(windows.UTF16ToString(unsafe.Slice(value, int(valueLength))))
				if metadata.FullVersion != "" {
					return metadata, nil
				}
			}
		}
	}
	if fixed.ProductVersionMS == 0 && fixed.ProductVersionLS == 0 {
		return Metadata{}, errors.New("runtime product version is unavailable")
	}
	metadata.FullVersion = fmt.Sprintf("%d.%d.%d", fixed.ProductVersionMS>>16, fixed.ProductVersionMS&0xffff, fixed.ProductVersionLS>>16)
	// Numeric resources cannot preserve the qualifier. Mark such a build as
	// prerelease explicitly so automatic stable selection cannot accept it.
	if metadata.Prerelease {
		metadata.FullVersion += "-prerelease"
	}
	return metadata, nil
}
