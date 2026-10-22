package agents

import (
	"os"
	"path/filepath"
	"strings"

	"lara4ever/internal/i18n"
)

func validateAdditionalDirectorySupport(profile AgentProfile, directories []string) error {
	if len(directories) == 0 {
		return nil
	}
	switch profile.ID {
	case AgentClaude, AgentCodex, AgentKimi:
		return nil
	default:
		label := profile.Label
		if label == "" {
			label = string(profile.ID)
		}
		return i18n.Errorf("errors.agents.additionalDirectoriesUnsupported", map[string]any{"label": label})
	}
}

// validateAdditionalDirectories runs before workspace resolution, which can
// create a worktree or switch a branch. Invalid context must not cause either.
func validateAdditionalDirectories(profile AgentProfile, directories []string) ([]string, error) {
	if err := validateAdditionalDirectorySupport(profile, directories); err != nil {
		return nil, err
	}
	var result []string
	seen := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		directory = strings.TrimSpace(directory)
		if directory == "" || strings.ContainsAny(directory, "\x00\r\n") || !filepath.IsAbs(directory) {
			return nil, i18n.Errorf("errors.agents.additionalDirectoryNotAbsolute", map[string]any{"directory": directory})
		}
		directory = filepath.Clean(directory)
		info, err := os.Stat(directory)
		if err != nil {
			return nil, i18n.Wrapf(err, "errors.agents.additionalDirectoryUnavailable", map[string]any{"directory": directory})
		}
		if !info.IsDir() {
			return nil, i18n.Errorf("errors.agents.additionalDirectoryNotFolder", map[string]any{"directory": directory})
		}
		key := contextDirectoryKey(directory)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, directory)
	}
	return result, nil
}

func excludePrimaryDirectory(projectPath string, directories []string) []string {
	primaryKey := contextDirectoryKey(projectPath)
	var result []string
	for _, directory := range directories {
		if contextDirectoryKey(directory) != primaryKey {
			result = append(result, directory)
		}
	}
	return result
}

func contextDirectoryKey(directory string) string {
	if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		directory = resolved
	}
	return NormalizeProjectPath(directory)
}
