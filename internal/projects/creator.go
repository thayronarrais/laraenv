package projects

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lara4ever/internal/paths"

	"lara4ever/internal/i18n"
	"lara4ever/internal/installer"
	"lara4ever/internal/php"
	"lara4ever/internal/vcruntime"
)

// ensurePHPIniAt is a thin wrapper that defers to php.EnsureIni so we don't
// duplicate logic. Kept here for the LookPath("php") fallback case where we
// don't have an active version reference.
func ensurePHPIniAt(versionDir string) error { return php.EnsureIni(versionDir) }

type CreateOptions struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`   // "blank" | "laravel" | "wordpress" | "git" | "vite" | "next" | "nuxt" | "astro"
	GitURL string `json:"gitUrl"` // only used when Kind == "git"
	// JsTemplate is the framework-specific sub-template (e.g. "react-ts" for vite, "" for next/nuxt/astro defaults).
	JsTemplate string `json:"jsTemplate"`
	// PackageManager overrides the user's default for create-* (npm/pnpm/yarn/bun).
	PackageManager string `json:"packageManager"`
	// LaravelVersion pins the Laravel major (e.g. "11"). Empty → composer's default.
	LaravelVersion string `json:"laravelVersion"`
	// PhpVersion pins which installed PHP to run composer with (e.g. "8.4").
	// Empty → fall back to the active PHP (legacy behavior).
	PhpVersion string `json:"phpVersion"`
}

type Creator struct {
	root           string
	cacheDir       string
	binDir         string
	phpExeFor      func(version string) string // empty version → active php.exe, or ""
	nodeDir        func() string               // returns the bin dir of active managed Node (or "")
	projectNodeDir func(name, path string) (string, error)
}

func NewCreator(root, cacheDir, binDir string, phpExeFor func(string) string) *Creator {
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "laraenv-cache")
	}
	_ = os.MkdirAll(cacheDir, 0755)
	if binDir == "" {
		binDir = paths.Bin()
	}
	_ = os.MkdirAll(binDir, 0755)
	return &Creator{root: root, cacheDir: cacheDir, binDir: binDir, phpExeFor: phpExeFor}
}

// SetNodeResolver wires a callback that returns the bin dir of the active
// managed Node install (or empty to fall back to PATH). Used by the JS
// scaffolding templates (vite/next/nuxt/astro) to find npm/pnpm/yarn/bun.
func (c *Creator) SetNodeResolver(f func() string) { c.nodeDir = f }

// SetProjectNodeResolver selects Node from the generated project's runtime
// requirements before installing dependencies. An error leaves the scaffold
// intact and prevents installation with an incompatible global Node.
func (c *Creator) SetProjectNodeResolver(f func(name, path string) (string, error)) {
	c.projectNodeDir = f
}

var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (c *Creator) Create(ctx context.Context, opts CreateOptions, emit func(line string)) error {
	if !safeName.MatchString(opts.Name) {
		return i18n.Errorf("errors.projects.invalidProjectName", map[string]any{"name": opts.Name})
	}
	target := filepath.Join(c.root, opts.Name)
	if _, err := os.Stat(target); err == nil {
		return i18n.Errorf("errors.projects.folderAlreadyExists", map[string]any{"path": target})
	}
	if err := os.MkdirAll(c.root, 0755); err != nil {
		return err
	}

	switch opts.Kind {
	case "blank":
		return c.createBlank(target, opts.Name, emit)
	case "laravel":
		return c.createLaravel(ctx, opts.Name, opts.PhpVersion, opts.LaravelVersion, emit)
	case "wordpress":
		return c.createWordPress(ctx, target, opts.Name, emit)
	case "git":
		if strings.TrimSpace(opts.GitURL) == "" {
			return i18n.Errorf("errors.projects.gitUrlRequired", nil)
		}
		return c.createGit(ctx, target, opts.GitURL, emit)
	case "vite", "next", "nuxt", "astro":
		return c.createJS(ctx, opts, emit)
	default:
		return i18n.Errorf("errors.projects.unknownProjectKind", map[string]any{"kind": opts.Kind})
	}
}

// createJS scaffolds a JS project via the framework's standard create-* CLI.
// Runs in c.root with a non-interactive flag set so the CLI doesn't prompt.
func (c *Creator) createJS(ctx context.Context, opts CreateOptions, emit func(string)) error {
	pm := opts.PackageManager
	if pm == "" {
		pm = "npm"
	}
	scaffoldNodeDir := c.nodeDirOrEmpty()
	bin, err := resolvePackageManager(pm, scaffoldNodeDir)
	if err != nil {
		return err
	}
	tpl := opts.JsTemplate
	var args []string
	switch opts.Kind {
	case "vite":
		if tpl == "" {
			tpl = "vanilla"
		}
		// npm create vite@latest <name> -- --template <tpl>
		args = []string{"create", "vite@latest", opts.Name, "--", "--template", tpl}
	case "next":
		// Keep npm's non-interactive flag, and forward Next's flags after --.
		// The generated manifest must be resolved before dependencies install.
		args = []string{"create", "next-app@latest", opts.Name, "--yes"}
		if pm == "npm" {
			args = append(args, "--", "--yes")
		}
		args = append(args, "--skip-install")
	case "nuxt":
		args = []string{"create", "nuxt@latest", opts.Name, "--no-install", "--no-git-init"}
	case "astro":
		args = []string{"create", "astro@latest", opts.Name, "--", "--template", "minimal", "--no-install", "--no-git", "--yes"}
	}
	// pnpm/yarn/bun use slightly different create-* invocation:
	switch pm {
	case "pnpm":
		// pnpm create vite <name> --template react-ts (no -- separator needed)
		args = pnpmifyCreateArgs(args)
	case "yarn":
		args = yarnifyCreateArgs(args)
	case "bun":
		args = bunifyCreateArgs(args)
	}
	emit(i18n.T("backend.projects.runningCommand", map[string]any{"command": bin + " " + strings.Join(args, " ")}))
	if err := runStreamCommand(streamCommand(ctx, bin, args, c.root, scaffoldNodeDir), emit); err != nil {
		return err
	}
	installCmd, err := c.dependencyInstallCommand(ctx, opts.Name, pm)
	if err != nil {
		return err
	}
	emit(i18n.T("backend.projects.installingDependencies", map[string]any{"pm": pm}))
	if err := runStreamCommand(installCmd, emit); err != nil {
		emit(i18n.T("backend.projects.installFailedWarn", map[string]any{"error": err}))
	}
	return nil
}

func (c *Creator) dependencyInstallCommand(ctx context.Context, name, pm string) (*exec.Cmd, error) {
	target := filepath.Join(c.root, name)
	nodeDir := c.nodeDirOrEmpty()
	if c.projectNodeDir != nil {
		var err error
		nodeDir, err = c.projectNodeDir(name, target)
		if err != nil {
			return nil, err
		}
	}
	bin, err := resolvePackageManager(pm, nodeDir)
	if err != nil {
		return nil, err
	}
	installArgs := []string{"install"}
	if pm == "npm" {
		installArgs = append(installArgs, "--no-fund", "--no-audit")
	}
	return streamCommand(ctx, bin, installArgs, target, nodeDir), nil
}

func (c *Creator) nodeDirOrEmpty() string {
	if c.nodeDir == nil {
		return ""
	}
	return c.nodeDir()
}

// ResolvePackageManagerPublic is the package-public alias of resolvePackageManager
// for use by other internal packages (e.g. devserver).
func ResolvePackageManagerPublic(pm, nodeDir string) (string, error) {
	return resolvePackageManager(pm, nodeDir)
}

// resolvePackageManager finds the executable for a package manager. Prefers
// the bundled file in the managed Node install, falls back to PATH.
func resolvePackageManager(pm, nodeDir string) (string, error) {
	exe := pm + ".cmd"
	if nodeDir != "" {
		candidate := filepath.Join(nodeDir, exe)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath(pm); err == nil {
		return p, nil
	}
	return "", i18n.Errorf("errors.projects.packageManagerNotFound", map[string]any{"manager": pm})
}

// pnpm create vite <name> --template react-ts
func pnpmifyCreateArgs(npmArgs []string) []string {
	out := []string{}
	for _, a := range npmArgs {
		if a == "--" {
			continue
		}
		// strip @latest suffix from "vite@latest" → "vite"
		if i := strings.Index(a, "@latest"); i > 0 {
			a = a[:i]
		}
		out = append(out, a)
	}
	return out
}

// yarn create vite <name> --template react-ts
func yarnifyCreateArgs(npmArgs []string) []string {
	out := []string{}
	for _, a := range npmArgs {
		if a == "--" {
			continue
		}
		if i := strings.Index(a, "@latest"); i > 0 {
			a = a[:i]
		}
		out = append(out, a)
	}
	return out
}

// bun create vite <name> --template react-ts
func bunifyCreateArgs(npmArgs []string) []string {
	return pnpmifyCreateArgs(npmArgs)
}

func (c *Creator) createBlank(target, name string, emit func(string)) error {
	emit(i18n.T("backend.projects.creatingBlankProject", map[string]any{"target": target}))
	pub := filepath.Join(target, "public")
	if err := os.MkdirAll(pub, 0755); err != nil {
		return err
	}
	indexPath := filepath.Join(pub, "index.php")
	body := fmt.Sprintf(`<?php
echo "<h1>Welcome — %s</h1>";
echo "<p>Edit <code>public/index.php</code> to get started.</p>";
echo "<p>PHP " . phpversion() . "</p>";
`, name)
	if err := os.WriteFile(indexPath, []byte(body), 0644); err != nil {
		return err
	}
	emit(i18n.T("backend.projects.done", nil))
	return nil
}

func (c *Creator) createLaravel(ctx context.Context, name, phpVersion, laravelVersion string, emit func(string)) error {
	bin, args, err := c.composerCommand(ctx, phpVersion, emit)
	if err != nil {
		return err
	}
	pkg := "laravel/laravel"
	if laravelVersion != "" {
		pkg = pkg + ":^" + laravelVersion + ".0"
	}
	// --ignore-platform-reqs: Windows PHP lacks POSIX-only extensions (ext-pcntl,
	// ext-posix) that some Laravel packages declare (Horizon, Octane, Pail).
	// Without this, create-project aborts on those platform requirements even
	// though the app runs fine for normal local dev. (This only affects install
	// resolution — it does not make those extensions exist at runtime.)
	args = append(args, "create-project", pkg, name, "--prefer-dist", "--no-interaction", "--ignore-platform-reqs")
	emit(i18n.T("backend.projects.runningCommand", map[string]any{"command": bin + " " + strings.Join(args, " ")}))
	return runStream(ctx, bin, args, c.root, emit)
}

// resolvePHPSeries returns the PHP major.minor series to look up a
// redistributable for. phpVersion wins when it was pinned by the caller.
// When it's empty ("use whatever's active"), the series is read back out of
// the resolved php.exe path instead of guessed: every managed install lives
// at <root>\php\<series>\php.exe (the same layout ExeFor/CgiBinary rely
// on), so the parent folder name IS the series. This is reading our own
// install convention, not sniffing a toolchain out of an arbitrary
// filename — an unmanaged system php (resolved via LookPath, living
// somewhere outside that layout) simply yields a string that matches no
// catalog entry, and installer.RedistURLForPHPSeries falls back to "" (the
// vcruntime default), same as before this resolution existed.
func resolvePHPSeries(phpVersion, phpExe string) string {
	if phpVersion != "" {
		return phpVersion
	}
	return filepath.Base(filepath.Dir(phpExe))
}

// composerCommand returns the binary + leading arguments to invoke composer.
// Strategy:
//  1. If a specific phpVersion is requested → run the bundled composer.phar
//     through that PHP (skipping a system `composer` so the user's pinned
//     version is honored, not whatever PHP composer happens to be wired to).
//  2. Else if `composer` is in PATH → use it directly.
//  3. Else use the bundled composer.phar through the active PHP.
func (c *Creator) composerCommand(ctx context.Context, phpVersion string, emit func(string)) (string, []string, error) {
	if phpVersion == "" {
		if p, err := exec.LookPath("composer"); err == nil {
			return p, nil, nil
		}
	}
	php := ""
	if c.phpExeFor != nil {
		php = c.phpExeFor(phpVersion)
	}
	if php == "" {
		// Empty phpVersion + no active PHP: try a system php as a last resort
		// (matches legacy behavior). Pinned phpVersion that isn't installed:
		// no fallback — surface a precise error.
		if phpVersion != "" {
			return "", nil, i18n.Errorf("errors.projects.phpVersionNotInstalled", map[string]any{"version": phpVersion})
		}
		php2, err := exec.LookPath("php")
		if err != nil {
			return "", nil, i18n.Errorf("errors.projects.noPhpAvailable", nil)
		}
		php = php2
	}
	// Make sure the PHP we're about to use has a usable php.ini with openssl
	// enabled — composer needs it for HTTPS. (ActiveExe already calls
	// EnsureIni, but if we fell back to LookPath("php") above we still need to
	// patch *some* ini. Best-effort: patch sibling of php executable.)
	if dir := filepath.Dir(php); dir != "" {
		_ = ensurePHPIniAt(dir)
	}

	// Make sure php.exe can actually load — without the Microsoft Visual
	// C++ Redistributable, composer would die with the cryptic
	// "exit status 0xc0000135". vcruntime.Check returns a friendly error
	// containing the redist download URL when the smoke-test detects
	// STATUS_DLL_NOT_FOUND; pipe it straight to the modal log so the
	// user can act on it. resolvePHPSeries figures out which PHP series is
	// actually in play (pinned or resolved-active) so
	// installer.RedistURLForPHPSeries can look up the redistributable that
	// specific series needs (e.g. vs18 for an 8.6 beta) instead of quietly
	// falling back to the default for an unpinned request.
	if err := vcruntime.Check(php, installer.RedistURLForPHPSeries(resolvePHPSeries(phpVersion, php))); err != nil {
		emit(err.Error())
		return "", nil, err
	}

	phar := filepath.Join(c.binDir, "composer.phar")
	if _, err := os.Stat(phar); err != nil {
		emit(i18n.T("backend.projects.composerPharDownloading", nil))
		if err := downloadIfMissing(ctx, "https://getcomposer.org/download/latest-stable/composer.phar", phar, emit); err != nil {
			return "", nil, i18n.Wrapf(err, "errors.projects.downloadComposerPharFailed", nil)
		}
		emit(i18n.T("backend.projects.composerPharSaved", map[string]any{"path": phar}))
	}
	return php, []string{phar}, nil
}

func (c *Creator) createGit(ctx context.Context, target, url string, emit func(string)) error {
	if _, err := exec.LookPath("git"); err != nil {
		return i18n.Errorf("errors.projects.gitNotFound", nil)
	}
	emit(i18n.T("backend.projects.gitCloneCommand", map[string]any{"url": url, "target": target}))
	return runStream(ctx,
		"git",
		[]string{"clone", "--progress", url, target},
		c.root, emit)
}

func (c *Creator) createWordPress(ctx context.Context, target, name string, emit func(string)) error {
	emit(i18n.T("backend.projects.downloadingWordpress", nil))
	cachePath := filepath.Join(c.cacheDir, "wordpress-latest.zip")
	if err := downloadIfMissing(ctx, "https://wordpress.org/latest.zip", cachePath, emit); err != nil {
		return err
	}
	emit(i18n.T("backend.projects.extracting", nil))
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	if err := extractZipFlatten(cachePath, target, "wordpress/"); err != nil {
		return err
	}
	emit(i18n.T("backend.projects.done", nil))
	return nil
}

func runStream(ctx context.Context, bin string, args []string, dir string, emit func(string)) error {
	return runStreamCommand(streamCommand(ctx, bin, args, dir, ""), emit)
}

// streamCommand keeps arguments separate and gives package-manager child
// processes the same Node selected for their executable.
func streamCommand(ctx context.Context, bin string, args []string, dir, nodeDir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	if nodeDir != "" {
		env := cmd.Environ()
		found := false
		for i, entry := range env {
			key, value, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(key, "PATH") {
				if value != "" {
					value = nodeDir + string(os.PathListSeparator) + value
				} else {
					value = nodeDir
				}
				env[i] = key + "=" + value
				found = true
				break
			}
		}
		if !found {
			env = append(env, "PATH="+nodeDir)
		}
		cmd.Env = env
	}
	hideWindow(cmd)
	return cmd
}

func runStreamCommand(cmd *exec.Cmd, emit func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pump := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 64*1024), 1024*1024)
		for s.Scan() {
			emit(s.Text())
		}
	}
	go pump(stdout)
	pump(stderr)
	return cmd.Wait()
}

func downloadIfMissing(ctx context.Context, url, dest string, emit func(string)) error {
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		emit(i18n.T("backend.projects.cacheHit", map[string]any{"path": dest}))
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "laraenv/0.1")
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return i18n.Errorf("errors.projects.downloadFailed", map[string]any{"url": url, "status": resp.StatusCode})
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// extractZipFlatten extracts a zip into target, stripping topPrefix from
// member names if present (e.g. "wordpress/" → "").
func extractZipFlatten(zipPath, target, topPrefix string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := f.Name
		if topPrefix != "" {
			name = strings.TrimPrefix(name, topPrefix)
			if name == "" {
				continue
			}
		}
		dest := filepath.Join(target, name)
		if !strings.HasPrefix(filepath.Clean(dest), filepath.Clean(target)+string(os.PathSeparator)) &&
			filepath.Clean(dest) != filepath.Clean(target) {
			return i18n.Errorf("errors.projects.unsafeZipPath", map[string]any{"name": f.Name})
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, f.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(w, rc); err != nil {
			rc.Close()
			w.Close()
			return err
		}
		rc.Close()
		w.Close()
	}
	return nil
}
