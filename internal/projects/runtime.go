package projects

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeCandidate separates an installation's folder identity from its actual
// executable version. An unknown FullVersion cannot satisfy a file constraint.
type RuntimeCandidate struct {
	Version     string
	FullVersion string
	Prerelease  bool
	LTS         bool
}

// RuntimeResolution is also the diagnostic contract used by project health UI.
// Version is empty when no managed runtime can safely satisfy the declaration.
type RuntimeResolution struct {
	Version        string `json:"version"`
	FullVersion    string `json:"fullVersion"`
	Source         string `json:"source"`
	Constraint     string `json:"constraint"`
	Status         string `json:"status"`
	InstallVersion string `json:"installVersion,omitempty"`
	Problem        string `json:"problem,omitempty"`
}

type runtimeDeclaration struct {
	source, constraint, problem string
	explicit                    bool
}

// ResolvePHP reads only the supplied checkout. It never changes configuration,
// executes PHP, or substitutes a default for a broken project declaration.
func ResolvePHP(projectPath, manual, global string, installed, available []RuntimeCandidate) RuntimeResolution {
	declaration := readRuntimeDeclaration(projectPath, "php")
	return resolveRuntime("php", manual, global, declaration, installed, available)
}

// ResolveNode uses .nvmrc, .node-version, then package.json engines.node.
func ResolveNode(projectPath, manual, global string, installed, available []RuntimeCandidate) RuntimeResolution {
	declaration := readRuntimeDeclaration(projectPath, "node")
	return resolveRuntime("node", manual, global, declaration, installed, available)
}

func readRuntimeDeclaration(projectPath, kind string) runtimeDeclaration {
	// A task without a checkout must not inherit manifests from the app's cwd.
	if strings.TrimSpace(projectPath) == "" {
		return runtimeDeclaration{}
	}
	files := []string{".php-version"}
	manifest, section, field := "composer.json", "require", "php"
	if kind == "node" {
		files = []string{".nvmrc", ".node-version"}
		manifest, section, field = "package.json", "engines", "node"
	}
	for _, name := range files {
		data, exists, problem := readRuntimeFile(filepath.Join(projectPath, name), 4096)
		if !exists {
			continue
		}
		declaration := runtimeDeclaration{source: name, explicit: true, problem: problem, constraint: strings.TrimSpace(string(data))}
		if problem == "" && declaration.constraint == "" {
			declaration.problem = "empty_version_file"
		}
		if strings.ContainsAny(declaration.constraint, "\r\n") {
			declaration.problem = "invalid_constraint"
		}
		return declaration
	}
	data, exists, problem := readRuntimeFile(filepath.Join(projectPath, manifest), 1024*1024)
	if !exists {
		return runtimeDeclaration{}
	}
	declaration := runtimeDeclaration{source: manifest, problem: problem}
	if problem != "" {
		return declaration
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil || document == nil {
		declaration.problem = "invalid_manifest"
		return declaration
	}
	raw, exists := document[section]
	if !exists {
		return runtimeDeclaration{}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		declaration.problem = "invalid_manifest"
		return declaration
	}
	raw, exists = fields[field]
	if !exists {
		return runtimeDeclaration{}
	}
	if string(raw) == "null" || json.Unmarshal(raw, &declaration.constraint) != nil {
		declaration.problem = "invalid_manifest"
	}
	declaration.constraint = strings.TrimSpace(declaration.constraint)
	if declaration.problem == "" && declaration.constraint == "" {
		declaration.problem = "invalid_constraint"
	}
	return declaration
}

func readRuntimeFile(path string, limit int64) ([]byte, bool, string) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, false, ""
	}
	if err != nil {
		return nil, true, "unreadable_file"
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, true, "unreadable_file"
	}
	if info.Size() > limit {
		return nil, true, "file_too_large"
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, true, "unreadable_file"
	}
	if int64(len(data)) > limit {
		return nil, true, "file_too_large"
	}
	return data, true, ""
}

func resolveRuntime(kind, manual, global string, declaration runtimeDeclaration, installed, available []RuntimeCandidate) RuntimeResolution {
	result := RuntimeResolution{Source: declaration.source, Constraint: declaration.constraint}
	var constraint runtimeRange
	if declaration.problem == "" && declaration.source != "" {
		var err error
		constraint, err = parseRuntimeRange(declaration.constraint, kind, declaration.explicit)
		if err != nil {
			declaration.problem = "invalid_constraint"
		}
	}
	manual = strings.TrimSpace(manual)
	if manual != "" {
		result.Source = "manual"
		if candidate, ok := findRuntimeIdentity(kind, manual, installed); ok {
			result.Version, result.FullVersion = candidate.Version, candidate.FullVersion
			result.Status = "resolved"
			if declaration.problem != "" {
				result.Status, result.Problem = "invalid", declaration.problem
			} else if declaration.source != "" {
				if candidate.FullVersion == "" {
					result.Status, result.Problem = "incompatible", "unknown_runtime_version"
				} else if !constraint.matches(candidate, kind) {
					result.Status, result.Problem = "incompatible", "incompatible_override"
				}
			}
			return result
		}
		result.Status, result.Problem = "missing", "missing_runtime"
		// A missing manual selection cannot silently select a different runtime.
		if wanted, err := parseRuntimeRange(manual, kind, true); err == nil {
			if candidate, ok := highestRuntime(available, &wanted, kind); ok {
				result.InstallVersion = candidate.FullVersion
			}
		}
		return result
	}
	if declaration.problem != "" {
		result.Status, result.Problem = "invalid", declaration.problem
		return result
	}
	if declaration.source != "" {
		if candidate, ok := highestRuntime(installed, &constraint, kind); ok {
			result.Version, result.FullVersion, result.Status = candidate.Version, candidate.FullVersion, "resolved"
			return result
		}
		result.Status, result.Problem = "missing", "missing_runtime"
		if candidate, ok := highestRuntime(available, &constraint, kind); ok {
			result.InstallVersion = candidate.FullVersion
		}
		return result
	}
	result.Source = "global"
	if candidate, ok := findRuntimeIdentity(kind, strings.TrimSpace(global), installed); ok {
		result.Version, result.FullVersion, result.Status = candidate.Version, candidate.FullVersion, "resolved"
		return result
	}
	if candidate, ok := highestRuntime(installed, nil, kind); ok {
		result.Version, result.FullVersion, result.Status = candidate.Version, candidate.FullVersion, "resolved"
		return result
	}
	result.Status = "unmanaged"
	return result
}

func findRuntimeIdentity(kind, wanted string, candidates []RuntimeCandidate) (RuntimeCandidate, bool) {
	if wanted == "" {
		return RuntimeCandidate{}, false
	}
	for _, candidate := range candidates {
		if candidate.Version == wanted || candidate.Version == strings.TrimPrefix(wanted, "v") {
			return candidate, true
		}
	}
	// Old PHP preferences stored patch versions while installs now use series.
	// Map the identity only; preserve the real executable FullVersion.
	if kind == "php" {
		if version, err := parseRuntimeVersion(wanted, kind); err == nil && version.parts == 3 && version.wildcard < 0 {
			for _, candidate := range candidates {
				if candidate.Version == version.series() {
					return candidate, true
				}
			}
		}
	}
	return RuntimeCandidate{}, false
}

func highestRuntime(candidates []RuntimeCandidate, constraint *runtimeRange, kind string) (RuntimeCandidate, bool) {
	var highest RuntimeCandidate
	var highestVersion runtimeVersion
	found := false
	for _, candidate := range candidates {
		version, err := parseRuntimeVersion(candidate.FullVersion, kind)
		if err != nil || version.parts != 3 || version.wildcard >= 0 {
			continue
		}
		if constraint != nil {
			if !constraint.matches(candidate, kind) {
				continue
			}
		} else if candidate.Prerelease || version.pre != "" {
			continue
		}
		if !found || compareRuntimeVersions(version, highestVersion) > 0 || (compareRuntimeVersions(version, highestVersion) == 0 && candidate.Version < highest.Version) {
			highest, highestVersion, found = candidate, version, true
		}
	}
	return highest, found
}
