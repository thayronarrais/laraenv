package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"lara4ever/internal/config"
	"lara4ever/internal/cron"
	"lara4ever/internal/localtls"
	"lara4ever/internal/nodejs"
	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/vhost"
)

// Background cron uses the same checkout resolution as the GUI without loading
// NewApp, initializing services, or rewriting project preferences.
func runProjectCron(configPath, jobID string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var document struct {
		Projects config.Projects `json:"projects"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	prefs := document.Projects
	if prefs.Root == "" {
		prefs.Root = paths.ProjectsRoot()
	}
	phpManager, nodeManager := php.NewManager(paths.PHPRoot()), nodejs.NewManager(paths.NodeRoot())
	workDir := func(name, override string) string {
		if override != "" {
			return override
		}
		if name == "" {
			return ""
		}
		return filepath.Join(prefs.Root, name)
	}
	resolve := func(name, path, phpOverride string) ([]string, error) {
		return runtimeProjectPaths(name, path, phpOverride, prefs, installedRuntimeInventory(phpManager, nodeManager), phpManager, nodeManager)
	}
	environment := func(name, cwd string, env []string) ([]string, error) {
		return localtls.Environment(env, vhost.LocalCACertPath(paths.NginxCerts()), localTLSProject(prefs, name, cwd, nil))
	}
	return cron.RunOnceWithRuntimeFromConfig(configPath, jobID, nil, workDir, nil, resolve, environment)
}
