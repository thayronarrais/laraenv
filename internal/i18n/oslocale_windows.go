//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

// OSLocale asks Windows for the user's UI language as a BCP-47 tag
// ("pt-BR", "ru-RU"). It returns "" when the call fails, which the caller
// treats the same as "no preference".
func OSLocale() string {
	const localeNameMaxLength = 85
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetUserDefaultLocaleName")
	buf := make([]uint16, localeNameMaxLength)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n-1])
}
