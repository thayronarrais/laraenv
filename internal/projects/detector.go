package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Kind string

const (
	KindLaravel   Kind = "laravel"
	KindWordPress Kind = "wordpress"
	KindPublicDir Kind = "public"
	KindPlain     Kind = "plain"
)

const (
	JSFrameworkNext    = "next"
	JSFrameworkNuxt    = "nuxt"
	JSFrameworkAstro   = "astro"
	JSFrameworkVite    = "vite"
	JSFrameworkVue     = "vue"
	JSFrameworkReact   = "react"
	PackageManagerNpm  = "npm"
	PackageManagerPnpm = "pnpm"
	PackageManagerYarn = "yarn"
	PackageManagerBun  = "bun"
)

type Detection struct {
	Kind           Kind   `json:"kind"`
	DocRoot        string `json:"docRoot"`
	JSFramework    string `json:"jsFramework"`    // "" if not a JS project
	PackageManager string `json:"packageManager"` // "" if no package.json
}

// Detect inspects a project directory and returns its kind plus the
// document root that should be served by the web server.
func Detect(projectPath string) Detection {
	js, pm := detectJS(projectPath)
	d := Detection{JSFramework: js, PackageManager: pm}
	switch {
	case isLaravel(projectPath):
		d.Kind = KindLaravel
		d.DocRoot = filepath.Join(projectPath, "public")
	case isWordPress(projectPath):
		d.Kind = KindWordPress
		d.DocRoot = projectPath
	case exists(filepath.Join(projectPath, "public")):
		d.Kind = KindPublicDir
		d.DocRoot = filepath.Join(projectPath, "public")
	default:
		d.Kind = KindPlain
		d.DocRoot = projectPath
	}
	return d
}

func detectJS(p string) (framework, pm string) {
	pkgPath := filepath.Join(p, "package.json")
	b, found, problem := readRuntimeFile(pkgPath, 1024*1024)
	if !found {
		return "", ""
	}
	if problem != "" {
		return "", detectPackageManager(p)
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		PackageManager  string            `json:"packageManager"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return "", detectPackageManager(p)
	}
	has := func(name string) bool {
		if _, ok := pkg.Dependencies[name]; ok {
			return true
		}
		_, ok := pkg.DevDependencies[name]
		return ok
	}
	switch {
	case has("next"):
		framework = JSFrameworkNext
	case has("nuxt") || has("nuxt3"):
		framework = JSFrameworkNuxt
	case has("astro"):
		framework = JSFrameworkAstro
	case has("vite"):
		framework = JSFrameworkVite
	case has("vue"):
		framework = JSFrameworkVue
	case has("react"):
		framework = JSFrameworkReact
	}
	manager, _, _ := strings.Cut(strings.TrimSpace(pkg.PackageManager), "@")
	switch manager {
	case PackageManagerNpm, PackageManagerPnpm, PackageManagerYarn, PackageManagerBun:
		return framework, manager
	default:
		return framework, detectPackageManager(p)
	}
}

func detectPackageManager(p string) string {
	switch {
	case exists(filepath.Join(p, "pnpm-lock.yaml")):
		return PackageManagerPnpm
	case exists(filepath.Join(p, "yarn.lock")):
		return PackageManagerYarn
	case exists(filepath.Join(p, "bun.lockb")), exists(filepath.Join(p, "bun.lock")):
		return PackageManagerBun
	case exists(filepath.Join(p, "package-lock.json")):
		return PackageManagerNpm
	case exists(filepath.Join(p, "package.json")):
		return PackageManagerNpm
	}
	return ""
}

func isLaravel(p string) bool {
	if !exists(filepath.Join(p, "artisan")) {
		return false
	}
	composer := filepath.Join(p, "composer.json")
	b, found, problem := readRuntimeFile(composer, 1024*1024)
	if !found || problem != "" {
		return false
	}
	s := string(b)
	return strings.Contains(s, "laravel/framework") || strings.Contains(s, "laravel/laravel")
}

func isWordPress(p string) bool {
	return exists(filepath.Join(p, "wp-config.php")) ||
		exists(filepath.Join(p, "wp-load.php")) ||
		exists(filepath.Join(p, "wp-content"))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
