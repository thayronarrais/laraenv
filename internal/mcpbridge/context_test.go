package mcpbridge

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This exercises the client-visible stdio proxy as well as the authenticated
// GUI server. Resources/prompts must preserve the same scoped read boundary.
func TestContextViewsAndToolThroughProxy(t *testing.T) {
	server := startTestServer(t, backendFunc(func(ctx context.Context, group string, args Args, progress func(string)) (any, error) {
		if group != "context" || args.Action != "summary" || args.Project != "demo" {
			t.Errorf("unexpected context access: %s %+v", group, args)
		}
		return map[string]any{"project": "demo", "capture": map[string]string{"status": "unavailable"}}, nil
	}), nil)
	proxy, closeProxy, err := newProxy(t.Context(), server.Status().DescriptorPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	a, b := mcp.NewInMemoryTransports()
	serverSession, err := proxy.Connect(t.Context(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "context-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	templates, err := session.ListResourceTemplates(t.Context(), nil)
	if err != nil || len(templates.ResourceTemplates) != 1 {
		t.Fatalf("resource templates %+v %v", templates, err)
	}
	resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "laraenv://projects/demo/context"})
	if err != nil || len(resource.Contents) != 1 || !strings.Contains(resource.Contents[0].Text, "unavailable") {
		t.Fatalf("resource %+v %v", resource, err)
	}
	prompt, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "diagnose_project", Arguments: map[string]string{"project": "demo"}})
	if err != nil || len(prompt.Messages) == 0 || !strings.Contains(prompt.Messages[0].Content.(*mcp.TextContent).Text, "untrusted") {
		t.Fatalf("prompt %+v %v", prompt, err)
	}
	for _, uri := range []string{"laraenv://projects/../context", "laraenv://projects/demo%2Fother/context", "laraenv://projects/demo/context?mutate=1"} {
		if _, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("accepted unsafe context URI %s", uri)
		}
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_context", Arguments: map[string]any{"action": "install_integration", "project": "demo"}})
	if err == nil && !result.IsError {
		t.Fatal("unconfirmed integration write accepted by schema")
	}
}
