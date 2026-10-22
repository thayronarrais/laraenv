package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"lara4ever/internal/applicationcontext"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/projects"
)

type MCPBoostContext struct {
	Declared          bool   `json:"declared"`
	Installed         bool   `json:"installed"`
	GuidelinesPresent bool   `json:"guidelinesPresent"`
	Message           string `json:"message"`
}
type MCPApplicationContext struct {
	Project     projects.Project            `json:"project"`
	Logs        applicationcontext.Logs     `json:"logs"`
	Capture     applicationcontext.Capture  `json:"capture"`
	Mail        applicationcontext.MailList `json:"mail"`
	Boost       MCPBoostContext             `json:"boost"`
	Limitations []string                    `json:"limitations"`
}

func mcpBoostContext(path string) MCPBoostContext {
	out := MCPBoostContext{Message: "Boost runs as a separate project MCP server. Review context.guidelines; write_guidelines requires confirmation. Run boost:install/boost:update through confirmed exec only after reviewing project changes."}
	root, err := os.OpenRoot(path)
	if err != nil {
		return out
	}
	defer root.Close()
	if st, err := root.Stat("vendor/laravel/boost/composer.json"); err == nil && st.Mode().IsRegular() {
		out.Installed = true
	}
	if st, err := root.Stat(".ai/guidelines/laraenv.md"); err == nil && st.Mode().IsRegular() {
		out.GuidelinesPresent = true
	}
	f, err := root.Open("composer.json")
	if err != nil {
		return out
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 256<<10))
	if err != nil {
		return out
	}
	var composer struct {
		Require map[string]string `json:"require"`
		Dev     map[string]string `json:"require-dev"`
	}
	if json.Unmarshal(data, &composer) == nil {
		out.Declared = composer.Require["laravel/boost"] != "" || composer.Dev["laravel/boost"] != ""
	}
	return out
}
func (a *App) mcpContextMail(ctx context.Context, project string, limit int) (applicationcontext.MailList, error) {
	out := applicationcontext.MailList{Status: "unavailable", Message: "Local Mailpit must be running; only mail explicitly tagged by the selected project is attributable", ProjectTag: applicationcontext.MailTag(project), Messages: []applicationcontext.MailSummary{}}
	if limit < 0 || limit > 50 {
		return out, errors.New("mail limit must be between 1 and 50")
	}
	if a.services == nil || a.ServiceStatus("mailpit") != "running" {
		return out, nil
	}
	got, err := applicationcontext.NewMailReader().List(ctx, project, limit)
	if err != nil {
		out.Message = err.Error()
		return out, err
	}
	return got, nil
}
func (a *App) mcpApplicationContext(ctx context.Context, input mcpbridge.Args) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := a.mcpProject(input.Project)
	if err != nil {
		return nil, err
	}
	switch input.Action {
	case "logs":
		return applicationcontext.ReadLogs(ctx, p.Path, input.Lines)
	case "capture_status", "capture_read":
		count := input.Lines
		if input.Action == "capture_status" {
			count = 1
		}
		return applicationcontext.ReadCapture(ctx, p.Path, p.Name, count)
	case "mail_list":
		return a.mcpContextMail(ctx, p.Name, input.Lines)
	case "mail_read":
		if a.services == nil || a.ServiceStatus("mailpit") != "running" {
			return nil, errors.New("start local Mailpit before reading project mail")
		}
		return applicationcontext.NewMailReader().Read(ctx, p.Name, input.ID)
	case "integration":
		return applicationcontext.IntegrationArtifact(p.Name), nil
	case "guidelines":
		return applicationcontext.Guidelines(p.Name), nil
	case "install_integration":
		files, err := applicationcontext.InstallIntegration(p.Path, p.Name, input.Confirm)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "installed-inactive", "written": files, "integration": applicationcontext.IntegrationArtifact(p.Name)}, nil
	case "write_guidelines":
		files, err := applicationcontext.WriteGuidelines(p.Path, p.Name, input.Confirm)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "written", "written": files, "guidelines": applicationcontext.Guidelines(p.Name), "message": "Review and run Boost install/update explicitly; no agent files were rewritten"}, nil
	case "summary":
		logs, logErr := applicationcontext.ReadLogs(ctx, p.Path, 40)
		if logErr != nil {
			logs.Message = "Log evidence unavailable: " + logErr.Error()
		}
		capture, captureErr := applicationcontext.ReadCapture(ctx, p.Path, p.Name, 40)
		if captureErr != nil {
			capture.Message = "Capture evidence unavailable: " + captureErr.Error()
		}
		mail, _ := a.mcpContextMail(ctx, p.Name, 10)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return MCPApplicationContext{Project: p, Logs: logs, Capture: capture, Mail: mail, Boost: mcpBoostContext(p.Path), Limitations: []string{"Reads existing evidence only; never boots the application or runs Artisan/tests/queries", "Capture and mail attribution require the explicit project integration. Historical evidence does not prove capture is currently active", "Log secret redaction is best effort; review diagnostic evidence before sharing it. Mail returns plain text only; HTML and attachments are omitted"}}, nil
	default:
		return nil, errors.New("unknown Laravel context action")
	}
}
