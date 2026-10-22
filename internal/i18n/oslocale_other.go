//go:build !windows

package i18n

// OSLocale returns the operating system's UI language as a BCP-47 tag.
// Only Windows is implemented; the Linux deploy TUI never renders the app UI.
func OSLocale() string { return "" }
