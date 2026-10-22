package workspacefiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func validRelative(name string, allowRoot bool) bool {
	if name == "" {
		return allowRoot
	}
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	clean := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(clean, "/") {
		return false
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
			return false
		}
		upper := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" {
			return false
		}
		if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9' {
			return false
		}
	}
	return true
}

func inspectPath(root *os.Root, relative string, wantDirectory bool) (os.FileInfo, Result) {
	if !validRelative(relative, wantDirectory) {
		return nil, problem("invalid_path", errors.New("invalid relative path"))
	}
	if relative == "" {
		relative = "."
	}
	parts := strings.Split(relative, "/")
	current := ""
	var info os.FileInfo
	for i, part := range parts {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, problem("unavailable", err)
		}
		if linked(info) {
			return nil, problem("invalid_path", errors.New("linked path is blocked"))
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, problem("invalid_path", errors.New("parent is not a directory"))
		}
	}
	if info == nil {
		var err error
		info, err = root.Stat(".")
		if err != nil {
			return nil, problem("unavailable", err)
		}
	}
	if wantDirectory && !info.IsDir() {
		return nil, problem("invalid_path", errors.New("not a directory"))
	}
	if !wantDirectory && !info.Mode().IsRegular() {
		return nil, problem("unsupported_format", errors.New("not a regular file"))
	}
	return info, Result{}
}

func canonicalFile(info *RootInfo, relative string) string {
	path := filepath.Join(info.CanonicalRoot, filepath.FromSlash(relative))
	return canonicalCase(path)
}
