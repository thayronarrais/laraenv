package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"lara4ever/internal/config"
	"lara4ever/internal/installer"
	"lara4ever/internal/nodejs"
	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/projects"
	"lara4ever/internal/runtimeinventory"
)

type runtimeSelectionInventory struct {
	PHP, Node                   []projects.RuntimeCandidate
	AvailablePHP, AvailableNode []projects.RuntimeCandidate
	DefaultPHP, DefaultNode     string
}

func resolveRuntimeSelection(name, path string, prefs config.Projects, inventory runtimeSelectionInventory) (projects.RuntimeResolution, projects.RuntimeResolution) {
	return projects.ResolvePHP(path, prefs.PhpVersions[name], inventory.DefaultPHP, inventory.PHP, inventory.AvailablePHP),
		projects.ResolveNode(path, prefs.NodeVersions[name], inventory.DefaultNode, inventory.Node, inventory.AvailableNode)
}

func installedRuntimeInventory(phpManager *php.Manager, nodeManager *nodejs.Manager) runtimeSelectionInventory {
	var inventory runtimeSelectionInventory
	// LTS is explicit catalog metadata; folder names alone cannot prove a runtime version.
	ltsSeries := map[string]string{}
	for _, source := range installer.Catalog() {
		candidate := projects.RuntimeCandidate{Version: filepath.Base(source.InstallTo), FullVersion: source.Version, Prerelease: source.Prerelease, LTS: source.LTS}
		switch source.Kind {
		case installer.KindPHP:
			inventory.AvailablePHP = append(inventory.AvailablePHP, candidate)
		case installer.KindNodejs:
			inventory.AvailableNode = append(inventory.AvailableNode, candidate)
			if source.LTS {
				firstLTS := source.LTSFrom
				if firstLTS == "" {
					firstLTS = source.Version
				}
				ltsSeries[strings.Split(source.Version, ".")[0]] = firstLTS
			}
		}
	}
	if phpManager != nil {
		versions, _ := phpManager.List()
		for _, version := range versions {
			metadata, _ := runtimeinventory.Read(filepath.Join(version.Path, "php.exe"))
			inventory.PHP = append(inventory.PHP, projects.RuntimeCandidate{Version: version.Version, FullVersion: metadata.FullVersion, Prerelease: metadata.Prerelease})
			if version.Active {
				inventory.DefaultPHP = version.Version
			}
		}
	}
	if nodeManager != nil {
		versions, _ := nodeManager.List()
		for _, version := range versions {
			if info, err := os.Stat(filepath.Join(version.Path, "node.exe")); err != nil || !info.Mode().IsRegular() {
				continue
			}
			metadata, _ := runtimeinventory.Read(filepath.Join(version.Path, "node.exe"))
			major := strings.Split(strings.TrimPrefix(metadata.FullVersion, "v"), ".")[0]
			inventory.Node = append(inventory.Node, projects.RuntimeCandidate{Version: version.Version, FullVersion: metadata.FullVersion, Prerelease: metadata.Prerelease, LTS: !metadata.Prerelease && nodeLTSVersion(metadata.FullVersion, ltsSeries[major])})
			if version.Active {
				inventory.DefaultNode = version.Version
			}
		}
	}
	return inventory
}

func nodeLTSVersion(version, firstLTS string) bool {
	parse := func(raw string) ([3]uint64, bool) {
		var numbers [3]uint64
		raw, _, _ = strings.Cut(strings.TrimPrefix(raw, "v"), "+")
		parts := strings.Split(raw, ".")
		if len(parts) != 3 {
			return numbers, false
		}
		for i, part := range parts {
			n, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return numbers, false
			}
			numbers[i] = n
		}
		return numbers, true
	}
	actual, actualOK := parse(version)
	floor, floorOK := parse(firstLTS)
	if !actualOK || !floorOK || actual[0] != floor[0] {
		return false
	}
	for i := 1; i < len(actual); i++ {
		if actual[i] != floor[i] {
			return actual[i] > floor[i]
		}
	}
	return true
}

func (a *App) projectRuntimeSelection(name, path string) (projects.RuntimeResolution, projects.RuntimeResolution) {
	return resolveRuntimeSelection(name, path, a.cfg.Get().Projects, installedRuntimeInventory(a.php, a.nodejs))
}

func runtimeRequirementError(name, kind string, resolution projects.RuntimeResolution) error {
	if resolution.Version != "" || resolution.Status == "unmanaged" {
		return nil
	}
	return fmt.Errorf("%s: %s runtime unavailable (%s, requirement %q, %s)", name, kind, resolution.Source, resolution.Constraint, resolution.Problem)
}

func (a *App) projectRuntimePaths(name, path string) ([]string, error) {
	return runtimeProjectPaths(name, path, "", a.cfg.Get().Projects, installedRuntimeInventory(a.php, a.nodejs), a.php, a.nodejs)
}

func runtimeProjectPaths(name, path, phpOverride string, prefs config.Projects, inventory runtimeSelectionInventory, phpManager *php.Manager, nodeManager *nodejs.Manager) ([]string, error) {
	phpManual := prefs.PhpVersions[name]
	if phpOverride != "" {
		phpManual = phpOverride
	}
	phpRuntime := projects.ResolvePHP(path, phpManual, inventory.DefaultPHP, inventory.PHP, inventory.AvailablePHP)
	nodeRuntime := projects.ResolveNode(path, prefs.NodeVersions[name], inventory.DefaultNode, inventory.Node, inventory.AvailableNode)
	return runtimeSelectedPaths(name, phpRuntime, nodeRuntime, phpManager, nodeManager)
}

func runtimeSelectedPaths(name string, phpRuntime, nodeRuntime projects.RuntimeResolution, phpManager *php.Manager, nodeManager *nodejs.Manager) ([]string, error) {
	if err := runtimeRequirementError(name, "PHP", phpRuntime); err != nil {
		return nil, err
	}
	if err := runtimeRequirementError(name, "Node", nodeRuntime); err != nil {
		return nil, err
	}
	var dirs []string
	if phpRuntime.Version != "" {
		dirs = append(dirs, phpManager.VersionDir(phpRuntime.Version))
	}
	if nodeRuntime.Version != "" {
		dirs = append(dirs, nodeManager.VersionDir(nodeRuntime.Version))
	}
	return append(dirs, paths.ComposerRoot()), nil
}

func (a *App) cronRuntimePaths(name, path, phpOverride string) ([]string, error) {
	return runtimeProjectPaths(name, path, phpOverride, a.cfg.Get().Projects, installedRuntimeInventory(a.php, a.nodejs), a.php, a.nodejs)
}

type runtimeCheckoutSnapshot struct {
	Serving   bool
	PHP, Node projects.RuntimeResolution
	Detection projects.Detection
}
type projectRuntimeSnapshot map[string]runtimeCheckoutSnapshot

func (a *App) projectServingPaths(name string) []string {
	if a.cfg.Get().Projects.Paused[name] {
		return nil
	}
	if a.routes != nil {
		if routes, err := a.routes.List(a.appContext(), name); err == nil && len(routes) > 0 {
			seen := map[string]bool{}
			var result []string
			for _, route := range routes {
				if !seen[route.TargetPath] {
					seen[route.TargetPath] = true
					result = append(result, route.TargetPath)
				}
			}
			return result
		}
	}
	return []string{filepath.Join(a.projects.Root(), name)}
}

func (a *App) projectRuntimeCheckoutPaths(name string) []string {
	seen := map[string]bool{}
	var result []string
	add := func(path string) {
		if path != "" && !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	add(filepath.Join(a.projects.Root(), name))
	if a.routes != nil {
		if routes, err := a.routes.List(a.appContext(), name); err == nil {
			for _, route := range routes {
				add(route.TargetPath)
			}
		}
	}
	if a.agents != nil {
		for _, session := range a.agents.ListSessions(name, false) {
			add(session.ProjectPath)
		}
	}
	return result
}

func (a *App) runtimeSnapshot(name string) projectRuntimeSnapshot {
	result := projectRuntimeSnapshot{}
	inventory, prefs := installedRuntimeInventory(a.php, a.nodejs), a.cfg.Get().Projects
	serving := map[string]bool{}
	for _, path := range a.projectServingPaths(name) {
		serving[path] = true
	}
	for _, path := range a.projectRuntimeCheckoutPaths(name) {
		phpRuntime, nodeRuntime := resolveRuntimeSelection(name, path, prefs, inventory)
		result[path] = runtimeCheckoutSnapshot{Serving: serving[path], PHP: phpRuntime, Node: nodeRuntime, Detection: projects.Detect(path)}
		if a.watcher != nil {
			_ = a.watcher.AddRuntimePath(name, path)
		}
	}
	return result
}

func runtimeServingChanged(before, after projectRuntimeSnapshot) bool {
	for path, current := range after {
		if !current.Serving {
			continue
		}
		previous, exists := before[path]
		if !exists || !previous.Serving || previous.PHP.Version != current.PHP.Version || previous.Detection.Kind != current.Detection.Kind || previous.Detection.DocRoot != current.Detection.DocRoot {
			return true
		}
	}
	for path, previous := range before {
		if previous.Serving && !after[path].Serving {
			return true
		}
	}
	return false
}

func (a *App) rememberProjectRuntimes() {
	a.projectRuntimeMu.Lock()
	defer a.projectRuntimeMu.Unlock()
	if a.projectRuntimeSnapshots == nil {
		a.projectRuntimeSnapshots = map[string]projectRuntimeSnapshot{}
	}
	if a.projectRuntimeApplied == nil {
		a.projectRuntimeApplied = map[string]projectRuntimeSnapshot{}
	}
	list, _ := a.projects.List()
	for _, project := range list {
		snapshot := a.runtimeSnapshot(project.Name)
		a.projectRuntimeSnapshots[project.Name] = snapshot
	}
}

func (a *App) projectRuntimeChanged(name string) {
	a.projectRuntimeMu.Lock()
	defer a.projectRuntimeMu.Unlock()
	if a.projectRuntimeSnapshots == nil {
		a.projectRuntimeSnapshots = map[string]projectRuntimeSnapshot{}
	}
	if a.projectRuntimeApplied == nil {
		a.projectRuntimeApplied = map[string]projectRuntimeSnapshot{}
	}
	previous := a.projectRuntimeSnapshots[name]
	current := a.runtimeSnapshot(name)
	servingChanged := runtimeServingChanged(a.projectRuntimeApplied[name], current)
	if reflect.DeepEqual(previous, current) && !servingChanged {
		return
	}
	if servingChanged && a.services != nil {
		a.refreshPhpFpmServicesWithPrune(false)
		if err := a.applyVhost(name, true, a.anyWebServerRunning()); err != nil {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "project:error", err.Error())
			}
		} else {
			a.projectRuntimeApplied[name] = current
		}
	}
	a.projectRuntimeSnapshots[name] = current
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "project:runtime-changed", name)
	}
}

func (a *App) anyWebServerRunning() bool {
	for _, name := range []string{"nginx", "apache", "caddy"} {
		if a.services.Status(name) == "running" {
			return true
		}
	}
	return false
}

func (a *App) refreshProjectRuntimes() {
	list, _ := a.projects.List()
	for _, project := range list {
		a.projectRuntimeChanged(project.Name)
	}
}

func (a *App) projectNameForRuntimePath(path string) string {
	if rel, err := filepath.Rel(a.projects.Root(), path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return strings.Split(rel, string(filepath.Separator))[0]
	}
	if location, ok := a.resolveAgentLocation(path); ok {
		return location.ProjectName
	}
	return ""
}
