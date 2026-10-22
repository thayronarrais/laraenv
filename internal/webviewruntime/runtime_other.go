//go:build !windows

package webviewruntime

// RecoveryBrowserFolder is unnecessary on platforms that do not use WebView2.
func RecoveryBrowserFolder() (string, error) { return "", nil }
