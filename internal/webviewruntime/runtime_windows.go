//go:build windows

package webviewruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// RecoveryBrowserFolder returns an explicitly selected machine WebView2 runtime
// only when the ordinary loader cannot find an installed runtime. An empty
// result preserves Wails' ordinary discovery and bootstrap behavior.
//
// Pass a nonempty result to windows.Options.WebviewBrowserPath before wails.Run.
// That option uses the loader's explicit-folder (fixed runtime) semantics for
// this process. The files remain in Microsoft's managed installation, and the
// selection is repeated on every launch so a later launch adopts updater changes.
// No version, environment override, or registry change is persisted.
func RecoveryBrowserFolder() (string, error) {
	return recoveryBrowserFolder(func() (string, error) {
		return webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	}, machineRuntimeRoots, func(root, folder string) error {
		return validateRuntimeCandidate(root, folder, runtime.GOARCH)
	})
}

type browserVersion [4]uint16

// This is Wails v2.12's minimum supported runtime (SDK 1.0.992.28).
var minimumBrowserVersion = browserVersion{94, 0, 992, 31}

type runtimeCandidate struct {
	root, folder string
	version      browserVersion
}

func recoveryBrowserFolder(normal func() (string, error), roots func() ([]string, error), validate func(string, string) error) (string, error) {
	version, err := normal()
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return "", fmt.Errorf("query installed WebView2 runtime: %w", err)
	}
	if err == nil && version != "" {
		return "", nil
	}
	installedRoots, err := roots()
	if err != nil {
		return "", fmt.Errorf("locate machine WebView2 installation: %w", err)
	}
	var candidates []runtimeCandidate
	for _, root := range installedRoots {
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read machine WebView2 installation %q: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			version, valid := parseBrowserVersion(entry.Name())
			if !valid || compareVersions(version, minimumBrowserVersion) < 0 {
				continue
			}
			candidates = append(candidates, runtimeCandidate{root, filepath.Join(root, entry.Name()), version})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return compareVersions(candidates[i].version, candidates[j].version) > 0 })
	var rejected []error
	for _, candidate := range candidates {
		if err := validate(candidate.root, candidate.folder); err != nil {
			rejected = append(rejected, fmt.Errorf("reject WebView2 runtime %q: %w", candidate.folder, err))
			continue
		}
		return candidate.folder, nil
	}
	return "", errors.Join(rejected...)
}

func machineRuntimeRoots() ([]string, error) {
	var roots []string
	var failures []error
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_ProgramFilesX86, windows.FOLDERID_ProgramFiles} {
		folder, err := windows.KnownFolderPath(id, windows.KF_FLAG_DONT_VERIFY)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		root := filepath.Join(folder, "Microsoft", "EdgeWebView", "Application")
		duplicate := false
		for _, existing := range roots {
			duplicate = duplicate || strings.EqualFold(root, existing)
		}
		if !duplicate {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, errors.Join(failures...)
	}
	return roots, nil
}

func parseBrowserVersion(value string) (browserVersion, bool) {
	var version browserVersion
	parts := strings.Split(value, ".")
	if len(parts) != len(version) {
		return version, false
	}
	for i, part := range parts {
		if part == "" {
			return version, false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return version, false
			}
		}
		number, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return version, false
		}
		version[i] = uint16(number)
	}
	return version, true
}

func compareVersions(left, right browserVersion) int {
	for i, part := range left {
		if part < right[i] {
			return -1
		}
		if part > right[i] {
			return 1
		}
	}
	return 0
}

func containedWithoutReparse(root, path string) error {
	root, path = filepath.Clean(root), filepath.Clean(path)
	volume := filepath.VolumeName(path)
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) || len(volume) != 2 || volume[1] != ':' {
		return errors.New("runtime paths must be absolute local drive paths")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("runtime path escapes its machine installation root")
	}
	// Check every ancestor, including the Known Folder itself: a junction above
	// the apparent installation root must not redirect DLL loading elsewhere.
	current := volume + string(filepath.Separator)
	components := strings.Split(strings.TrimPrefix(path[len(volume):], string(filepath.Separator)), string(filepath.Separator))
	for _, component := range append([]string{""}, components...) {
		if component != "" {
			current = filepath.Join(current, component)
		}
		pointer, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attributes, err := windows.GetFileAttributes(pointer)
		if err != nil {
			return err
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("runtime path contains a reparse point: %q", current)
		}
	}
	return nil
}
