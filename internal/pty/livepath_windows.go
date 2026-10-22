//go:build windows

package pty

import "golang.org/x/sys/windows/registry"

// livePath reads the current machine + user PATH straight from the Windows
// registry so the embedded terminal reflects tools installed after LaraEnv
// started (the process env is only a snapshot from launch time). System PATH
// comes first, then user PATH — the order Windows uses when composing PATH for
// a freshly launched shell.
func livePath() string {
	out := ""
	join := func(v string) {
		if v == "" {
			return
		}
		if out != "" {
			out += ";"
		}
		out += v
	}
	join(readRegPath(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`))
	join(readRegPath(registry.CURRENT_USER, `Environment`))
	return out
}

// readRegPath returns the (expanded) "Path" value of a registry key, or "" on
// any error. Handles both REG_SZ and REG_EXPAND_SZ (e.g. %SystemRoot%\...).
func readRegPath(root registry.Key, path string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return ""
	}
	if exp, err := registry.ExpandString(v); err == nil {
		v = exp
	}
	return v
}
