package main

import (
	"errors"

	"lara4ever/internal/devserver"
)

// ListProjectScripts returns the sorted script names declared in package.json.
// Unlike ListProjectCommands, it never invokes artisan or other project code.
func (a *App) ListProjectScripts(name string) ([]string, error) {
	if a.projects == nil {
		return nil, errors.New("configured projects directory is unavailable")
	}
	project, err := a.mcpProject(name)
	if err != nil {
		return nil, err
	}
	return devserver.ListScripts(project.Path)
}
