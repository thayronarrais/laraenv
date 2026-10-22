package main

import (
	"errors"
	"fmt"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/editor"
	"lara4ever/internal/projects"
	"lara4ever/internal/workspacefiles"
)

func newWorkspaceResolveSource(list func() ([]projects.Project, error), get func(string) (pkgagents.AgentSession, bool)) workspacefiles.ResolveSource {
	return func(ref workspacefiles.ContextRef) (string, string, error) {
		switch ref.Kind {
		case "project":
			if ref.ProjectName == "" || ref.SessionID != "" {
				return "", "", errors.New("invalid project reference")
			}
			items, err := list()
			if err != nil {
				return "", "", err
			}
			for _, item := range items {
				if item.Name == ref.ProjectName && item.Path != "" {
					return item.Path, item.Name, nil
				}
			}
		case "agent":
			if ref.SessionID == "" || ref.ProjectName != "" {
				return "", "", errors.New("invalid agent reference")
			}
			session, ok := get(ref.SessionID)
			if ok && session.ID == ref.SessionID && session.ProjectPath != "" {
				return session.ProjectPath, session.ProjectName, nil
			}
		}
		return "", "", errors.New("workspace source unavailable")
	}
}

func (a *App) resolveWorkspaceSource(ref workspacefiles.ContextRef) (string, string, error) {
	if a.projects == nil || a.agents == nil {
		return "", "", errors.New("workspace source unavailable")
	}
	return newWorkspaceResolveSource(a.projects.List, a.agents.GetSession)(ref)
}

func (a *App) fileService() *workspacefiles.Service {
	if a.workspaceFiles != nil {
		return a.workspaceFiles
	}
	return workspacefiles.NewService(a.resolveWorkspaceSource)
}

func (a *App) GetWorkspaceFileContext(ref workspacefiles.ContextRef) workspacefiles.Result {
	return a.fileService().Context(ref)
}
func (a *App) ListWorkspaceDirectory(ref workspacefiles.ContextRef, key, relative, cursor string) workspacefiles.Result {
	return a.fileService().List(ref, key, relative, cursor)
}
func (a *App) ReadWorkspaceDocument(ref workspacefiles.ContextRef, key, relative string) workspacefiles.Result {
	return a.fileService().Read(ref, key, relative)
}
func (a *App) CheckWorkspaceDocument(ref workspacefiles.ContextRef, key, relative, revision string) workspacefiles.Result {
	return a.fileService().Check(ref, key, relative, revision)
}
func (a *App) SaveWorkspaceDocument(ref workspacefiles.ContextRef, key, relative, content, expectedRevision string) workspacefiles.Result {
	return a.fileService().Save(ref, key, relative, content, expectedRevision)
}
func (a *App) ReadWorkspaceImage(ref workspacefiles.ContextRef, key, relative string) workspacefiles.Result {
	return a.fileService().ReadImage(ref, key, relative)
}

func (a *App) OpenWorkspaceDocumentExternally(ref workspacefiles.ContextRef, key, relative string) workspacefiles.Result {
	file, issue := a.fileService().ResolveFile(ref, key, relative)
	if issue.Code != "ok" {
		return issue
	}
	projectName := ref.ProjectName
	if ref.Kind == "agent" {
		if a.agents == nil {
			return workspacefiles.Result{Code: "unavailable", Message: "agent session unavailable"}
		}
		session, ok := a.agents.GetSession(ref.SessionID)
		if !ok || session.ID != ref.SessionID {
			return workspacefiles.Result{Code: "unavailable", Message: "agent session unavailable"}
		}
		projectName = session.ProjectName
	}
	picked, custom, err := a.resolveEditor(projectName, "")
	if err != nil {
		return workspacefiles.Result{Code: "unavailable", Message: err.Error()}
	}
	if err = editor.OpenFile(picked, file, custom); err != nil {
		return workspacefiles.Result{Code: "io_error", Message: fmt.Sprint(err)}
	}
	return workspacefiles.Result{Code: "ok"}
}
