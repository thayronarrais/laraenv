package mcpbridge

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolSpec struct {
	group, description string
	actions, fields    []string
	required           map[string][]string
}

var toolSpecs = []toolSpec{
	{"project", "Manage LaraEnv projects. list: list projects; detail: project configuration and runtime; set_php/set_node: select installed version (empty resets override); set_ssl: enable/disable HTTPS; set_proxy: set port (0 clears); pause/resume: suspend/restore serving. Mutations require an existing project.", []string{"list", "detail", "set_php", "set_node", "set_ssl", "set_proxy", "pause", "resume"}, []string{"project", "version", "enabled", "port"}, map[string][]string{"detail": {"project"}, "set_php": {"project", "version"}, "set_node": {"project", "version"}, "set_ssl": {"project", "enabled"}, "set_proxy": {"project", "port"}, "pause": {"project"}, "resume": {"project"}}},
	{"service", "Control services owned by the running LaraEnv GUI. status: one named service or all services; start/stop/reload: operate on one named service. reload also reloads its dependencies.", []string{"status", "start", "stop", "reload"}, []string{"service"}, map[string][]string{"start": {"service"}, "stop": {"service"}, "reload": {"service"}}},
	{"worker", "Inspect/control devserver, Laravel queue, or cron workers. status: inspect all workers of kind, or optionally scope by project/cron id; start/stop: start/stop devserver/queue or enable/disable cron scheduling; restart: devserver/queue only; logs: recent buffered output. Other actions require project for devserver/queue or job id for cron. Workers use their saved project arguments. start, restart and run require confirm:true because project scripts, queued jobs and stored cron commands may change data. run executes an existing cron job now.", []string{"status", "start", "stop", "restart", "logs", "run"}, []string{"kind", "project", "id", "lines", "confirm"}, map[string][]string{}},
	{"exec", "Execute project-scoped artisan, composer, or npm using LaraEnv's selected PHP/Node runtime. Pass a literal argument array in args; arbitrary shell commands are unavailable. Returns stdout, stderr, exit status and duration. All commands outside the supported inspection allowlist require confirm:true, including project scripts.", []string{"artisan", "composer", "npm"}, []string{"project", "args", "confirm"}, map[string][]string{"artisan": {"project", "args"}, "composer": {"project", "args"}, "npm": {"project", "args"}}},
	{"logs", "Read bounded logs. service: captured service stdout/stderr, requires service; app: project application logs, requires project; worker: devserver/queue/cron logs, requires kind plus project or cron id; activity: LaraEnv MCP activity metadata. lines defaults to 100 and is capped at 500.", []string{"service", "app", "worker", "activity"}, []string{"service", "project", "kind", "id", "lines"}, map[string][]string{"service": {"service"}, "app": {"project"}, "worker": {"kind"}}},
	{"db", "Operate on local LaraEnv MySQL/PostgreSQL databases. list: list databases; create: create database; snapshot: save a dump without changing its contents; drop: delete database and requires confirm:true. System databases cannot be dropped. Uses LaraEnv's installed client tools and existing services.", []string{"list", "create", "snapshot", "drop"}, []string{"engine", "database", "confirm"}, map[string][]string{"list": {"engine"}, "create": {"engine", "database"}, "snapshot": {"engine", "database"}, "drop": {"engine", "database", "confirm"}}},
	{"diag", "Run local diagnostics. doctor: inspect project directories, installed runtimes and service status; request_timing (alias timing): measure HTTP connection, TLS and response timing for an existing local project, verifying HTTPS certificates. Diagnostics report evidence and do not repair configuration.", []string{"doctor", "request_timing", "timing"}, []string{"project"}, map[string][]string{"request_timing": {"project"}, "timing": {"project"}}},
	{"context", "Read local Laravel evidence without booting the application. summary: setup, daily/single logs, capture and explicitly project-tagged Mailpit; logs: structured entries/errors; mail_list/mail_read: read-only project mail (id required for mail_read); capture_status/capture_read: observed metadata, unavailable if not activated. integration/guidelines: review generated files and activation/removal instructions. install_integration/write_guidelines write only reviewed missing/identical files and require confirm:true; never register providers, edit .env or install dependencies.", []string{"summary", "logs", "mail_list", "mail_read", "capture_status", "capture_read", "integration", "guidelines", "install_integration", "write_guidelines"}, []string{"project", "id", "lines", "confirm"}, map[string][]string{"summary": {"project"}, "logs": {"project"}, "mail_list": {"project"}, "mail_read": {"project", "id"}, "capture_status": {"project"}, "capture_read": {"project"}, "integration": {"project"}, "guidelines": {"project"}, "install_integration": {"project", "confirm"}, "write_guidelines": {"project", "confirm"}}},
}

func fieldSchemas() map[string]any {
	text := func(description string, min, max int) map[string]any {
		return map[string]any{"type": "string", "description": description, "minLength": min, "maxLength": max}
	}
	return map[string]any{
		"project":        text("Existing project name from project.list", 1, 255),
		"service":        text("Registered LaraEnv service name", 1, 128),
		"kind":           map[string]any{"type": "string", "enum": []string{"devserver", "queue", "cron"}},
		"id":             text("Existing cron job id", 1, 128),
		"version":        text("Installed runtime version; empty clears override", 0, 64),
		"enabled":        map[string]any{"type": "boolean"},
		"port":           map[string]any{"type": "integer", "minimum": 0, "maximum": 65535},
		"args":           map[string]any{"type": "array", "maxItems": 128, "items": text("Literal command argument", 0, 8192)},
		"lines":          map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		"engine":         map[string]any{"type": "string", "enum": []string{"mysql", "postgres"}},
		"database":       text("Database identifier", 1, 128),
		"confirm":        map[string]any{"type": "boolean", "description": "Explicit approval to execute scripts or jobs, or to perform a destructive operation"},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 300, "description": "Maximum operation time; defaults to 60 seconds"},
	}
}

func schemaFor(spec toolSpec) map[string]any {
	fields := fieldSchemas()
	properties := map[string]any{"action": map[string]any{"type": "string", "enum": spec.actions}, "timeoutSeconds": fields["timeoutSeconds"]}
	for _, name := range spec.fields {
		properties[name] = fields[name]
	}
	conditions := []any{}
	for action, required := range spec.required {
		conditions = append(conditions, map[string]any{"if": map[string]any{"properties": map[string]any{"action": map[string]any{"const": action}}}, "then": map[string]any{"required": required}})
	}
	if spec.group == "db" {
		conditions = append(conditions, confirmationFor("drop"))
	}
	if spec.group == "context" {
		conditions = append(conditions, confirmationFor("install_integration"), confirmationFor("write_guidelines"))
	}
	if spec.group == "worker" {
		conditions = append(conditions, map[string]any{"required": []string{"kind"}})
		conditions = append(conditions, map[string]any{"if": map[string]any{"properties": map[string]any{"action": map[string]any{"not": map[string]any{"const": "status"}}}}, "then": map[string]any{"if": map[string]any{"properties": map[string]any{"kind": map[string]any{"const": "cron"}}}, "then": map[string]any{"required": []string{"id"}}, "else": map[string]any{"required": []string{"project"}}}})
		conditions = append(conditions, confirmationFor("run"))
		conditions = append(conditions, confirmationFor("start"), confirmationFor("restart"))
	}
	return map[string]any{"type": "object", "properties": properties, "required": []string{"action"}, "additionalProperties": false, "allOf": conditions}
}

func confirmationFor(action string) map[string]any {
	return map[string]any{"if": map[string]any{"properties": map[string]any{"action": map[string]any{"const": action}}}, "then": map[string]any{"required": []string{"confirm"}, "properties": map[string]any{"confirm": map[string]any{"const": true}}}}
}

type toolCall func(context.Context, *mcp.CallToolRequest, string, Args) (*mcp.CallToolResult, error)

func newToolServer(version string, call toolCall) *mcp.Server {
	s := newSDKServer(version)
	for _, spec := range toolSpecs {
		mcp.AddTool(s, &mcp.Tool{Name: "laraenv_" + spec.group, Description: spec.description, InputSchema: schemaFor(spec)}, func(ctx context.Context, req *mcp.CallToolRequest, args Args) (*mcp.CallToolResult, any, error) {
			res, err := call(ctx, req, spec.group, args)
			return res, nil, err
		})
	}
	registerContextViews(s, func(ctx context.Context, project string) (string, error) {
		res, err := call(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "laraenv_context"}}, "context", Args{Action: "summary", Project: project})
		return contextResultText(res, err)
	})
	return s
}

func newSDKServer(version string) *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{Name: "LaraEnv", Version: version}, &mcp.ServerOptions{Instructions: "Operate on the running LaraEnv GUI. List projects/services before mutations. Commands use explicit argument arrays. Destructive operations require confirm:true.", Capabilities: &mcp.ServerCapabilities{}})
}

// The proxy advertises the same schemas, but lets the authenticated GUI validate
// calls so rejected inputs are included in its central audit feed as well.
func newProxyToolServer(version string, call toolCall) *mcp.Server {
	s := newSDKServer(version)
	for _, spec := range toolSpecs {
		s.AddTool(&mcp.Tool{Name: "laraenv_" + spec.group, Description: spec.description, InputSchema: schemaFor(spec)}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return call(ctx, req, spec.group, Args{})
		})
	}
	return s
}

func resultFor(value any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, StructuredContent: value}, nil
}
