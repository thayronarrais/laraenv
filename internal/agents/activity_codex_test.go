package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type activityCodexScript struct {
	t         *testing.T
	pages     []json.RawMessage
	requests  []map[string]any
	methods   map[string]int
	closed    bool
	listError error
	userAgent string
}

func (s *activityCodexScript) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if s.methods == nil {
		s.methods = map[string]int{}
	}
	s.methods[method]++
	b, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	switch method {
	case "initialize":
		if p["capabilities"].(map[string]any)["experimentalApi"] != true {
			s.t.Fatal("missing experimental negotiation")
		}
		ua := s.userAgent
		if ua == "" {
			ua = "codex/0.160.0"
		}
		b, _ = json.Marshal(map[string]string{"userAgent": ua})
		return b, nil
	case "initialized":
		return json.RawMessage(`{}`), nil
	case "thread/list":
		s.requests = append(s.requests, p)
		if p["parentThreadId"] == nil || p["parentThreadId"] == "" {
			s.t.Fatal("unscoped history request")
		}
		if _, ok := p["cwd"]; ok {
			s.t.Fatal("cwd limits descendants")
		}
		if p["useStateDbOnly"] != true {
			s.t.Fatal("rollout scan allowed")
		}
		source, _ := p["sourceKinds"].([]any)
		if len(source) != 1 || source[0] != "subAgentThreadSpawn" {
			s.t.Fatal("missing spawn source filter")
		}
		if s.listError != nil {
			return nil, s.listError
		}
		if len(s.pages) == 0 {
			return json.RawMessage(`{"data":[],"nextCursor":null}`), nil
		}
		page := s.pages[0]
		s.pages = s.pages[1:]
		return page, nil
	default:
		s.t.Fatalf("unexpected RPC %q", method)
		return nil, errors.New("unexpected RPC")
	}
}
func (s *activityCodexScript) Close() error { s.closed = true; return nil }
func codexActivityRead(t *testing.T, s *activityCodexScript, q ActivityMetadataQuery) (ActivityMetadataResult, error) {
	t.Helper()
	s.t = t
	if q.RootProviderID == "" {
		q.RootProviderID = "root"
	}
	q.Executable = "synthetic"
	r, err := NewCodexActivityReader(func(context.Context, string) (CodexActivityTransport, error) { return s, nil }).Read(t.Context(), q)
	if !s.closed {
		t.Fatal("transport left open")
	}
	if s.methods["thread/resume"] != 0 || s.methods["turn/start"] != 0 {
		t.Fatal("mutating RPC")
	}
	return r, err
}
func codexActivityPage(items ...map[string]any) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"data": items, "nextCursor": nil})
	return b
}
func codexActivityChild(id, parent string) map[string]any {
	return map[string]any{"id": id, "parentThreadId": parent, "source": map[string]any{"subAgent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent, "depth": 1}}}, "agentNickname": "Synthetic worker", "model": "synthetic-model", "cwd": "/other/project", "status": map[string]string{"type": "notLoaded"}}
}
func TestActivityCodexRootScopedPagination(t *testing.T) {
	page1, err := os.ReadFile("testdata/codex/activity-thread-page1.json")
	if err != nil {
		t.Fatal(err)
	}
	page2, err := os.ReadFile("testdata/codex/activity-thread-page2.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &activityCodexScript{pages: []json.RawMessage{page1, page2}}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || len(r.Nodes) != 2 || r.Status != ActivityAvailable {
		t.Fatalf("result %+v error %v", r, err)
	}
	if s.requests[1]["cursor"] != "page-two" || r.Nodes[1].ParentProviderID != "root" {
		t.Fatal("pagination lost scope")
	}
	if r.Nodes[0].Label != "Synthetic worker" || r.Nodes[0].Model.Kind != ActivityModelConfigured {
		t.Fatalf("metadata %+v", r.Nodes[0])
	}
}
func TestActivityCodexForeignPageRejected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		child map[string]any
	}{
		{"foreign", codexActivityChild("child", "foreign")},
		{"cycle", codexActivityChild("root", "root")},
		{"source mismatch", func() map[string]any { c := codexActivityChild("child", "root"); c["source"] = "cli"; return c }()},
		{"fork is not delegation", map[string]any{"id": "fork", "forkedFromId": "root", "source": "cli"}},
		{"contradictory source parent", func() map[string]any {
			c := codexActivityChild("child", "root")
			c["source"] = codexActivityChild("x", "foreign")["source"]
			return c
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &activityCodexScript{pages: []json.RawMessage{codexActivityPage(tc.child)}}
			r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
			if err != nil || len(r.Nodes) != 0 || r.MessageCode != ActivityMessageUnsupportedProtocol || len(s.requests) != 1 {
				t.Fatalf("result %+v err %v", r, err)
			}
		})
	}
}
func TestActivityCodexChildOutsideCwd(t *testing.T) {
	s := &activityCodexScript{pages: []json.RawMessage{codexActivityPage(codexActivityChild("child", "root")), codexActivityPage(codexActivityChild("grandchild", "child"))}}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || len(r.Nodes) != 2 || r.Nodes[1].ParentProviderID != "child" {
		t.Fatalf("result %+v error %v", r, err)
	}
}
func TestActivityCodexRepeatedCursorStops(t *testing.T) {
	s := &activityCodexScript{pages: []json.RawMessage{json.RawMessage(`{"data":[],"nextCursor":"repeat"}`), json.RawMessage(`{"data":[],"nextCursor":"repeat"}`)}}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || !r.Truncated || len(s.requests) != 2 {
		t.Fatalf("result %+v error %v requests %d", r, err, len(s.requests))
	}
}
func TestActivityCodexGlobalLimits(t *testing.T) {
	t.Run("tree shares pages", func(t *testing.T) {
		s := &activityCodexScript{}
		parent := "root"
		for i := 0; i < 7; i++ {
			child := fmt.Sprintf("nested%d", i)
			s.pages = append(s.pages, codexActivityPage(codexActivityChild(child, parent)))
			parent = child
		}
		r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
		pages := len(s.requests)
		if pages > 5 || len(r.Nodes) > 500 {
			t.Fatal("budget exceeded")
		}
		if err != nil || pages != 5 || len(r.Nodes) != 5 || !r.Truncated {
			t.Fatalf("tree result %+v error %v pages %d", r, err, pages)
		}
	})
	t.Run("pages", func(t *testing.T) {
		s := &activityCodexScript{}
		for i := 0; i < 7; i++ {
			s.pages = append(s.pages, json.RawMessage(fmt.Sprintf(`{"data":[],"nextCursor":"page%d"}`, i)))
		}
		r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
		pages := len(s.requests)
		if pages > 5 || len(r.Nodes) > 500 {
			t.Fatal("budget exceeded")
		}
		if err != nil || pages != 5 || !r.Truncated {
			t.Fatalf("result %+v error %v", r, err)
		}
	})
	t.Run("items", func(t *testing.T) {
		items := []map[string]any{}
		for i := 0; i < 501; i++ {
			items = append(items, codexActivityChild(fmt.Sprint(i), "root"))
		}
		s := &activityCodexScript{pages: []json.RawMessage{codexActivityPage(items...)}}
		r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
		pages := len(s.requests)
		if pages > 5 || len(r.Nodes) > 500 {
			t.Fatal("budget exceeded")
		}
		if err != nil || len(r.Nodes) != 500 || !r.Truncated || pages != 1 {
			t.Fatalf("items %d error %v truncated %v", len(r.Nodes), err, r.Truncated)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		s := &activityCodexScript{pages: []json.RawMessage{json.RawMessage(`{"data":[],"ignored":"` + strings.Repeat("x", 2*1024*1024) + `"}`)}}
		r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
		if err == nil || len(r.Nodes) != 0 || len(s.requests) != 1 {
			t.Fatal("oversized page accepted")
		}
	})
}
func TestActivityCodexUnsupportedFilterDoesNotEnumerate(t *testing.T) {
	s := &activityCodexScript{listError: &codexActivityRPCError{Code: -32602}}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || r.Status != ActivityUnavailable || r.MessageCode != ActivityMessageUnsupportedProtocol || len(s.requests) != 1 {
		t.Fatalf("result %+v err %v", r, err)
	}
}
func TestActivityCodexUnsupportedVersionDoesNotEnumerate(t *testing.T) {
	s := &activityCodexScript{userAgent: "codex/0.100.0"}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || r.MessageCode != ActivityMessageUnsupportedProtocol || len(s.requests) != 0 {
		t.Fatalf("result %+v err %v", r, err)
	}
}

func TestActivityCodexVersionMustBeServerBuildPrefix(t *testing.T) {
	for _, ua := range []string{"mystery 0.160.0", "unknown/old (Windows 11.0.0) client/0.160.0", "codex/0.160.0-alpha.1"} {
		s := &activityCodexScript{userAgent: ua}
		r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
		if err != nil || r.MessageCode != ActivityMessageUnsupportedProtocol || len(s.requests) != 0 {
			t.Fatalf("unverified build %q accepted: %+v %v", ua, r, err)
		}
	}
}

func TestActivityCodexSanitizesMetadataAndDeduplicates(t *testing.T) {
	child := codexActivityChild("child", "root")
	child["agentNickname"] = "\x00" + strings.Repeat("界", 241) + "\n"
	child["agentRole"] = "re\tsearcher"
	child["preview"] = "synthetic excluded conversation text"
	s := &activityCodexScript{userAgent: "laraenv_activity/0.160.0 (Windows 11.0.0; x86_64) client/1.0.0", pages: []json.RawMessage{codexActivityPage(child, child)}}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || len(r.Nodes) != 1 || len([]rune(r.Nodes[0].Label)) != 240 || r.Nodes[0].AgentType != "researcher" {
		t.Fatalf("metadata %+v error %v", r, err)
	}
}
func TestActivityCodexNotLoadedIsUnknown(t *testing.T) {
	s := &activityCodexScript{pages: []json.RawMessage{codexActivityPage(codexActivityChild("child", "root"))}}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{})
	if err != nil || len(r.Nodes) != 1 || r.Capabilities.LiveState || r.Capabilities.Duration || r.Capabilities.Tokens || r.BackgroundTasks != nil {
		t.Fatalf("metadata claimed telemetry: %+v %v", r, err)
	}
	if r.Nodes[0].FirstObservedAt.IsZero() || r.Nodes[0].LastObservedAt != r.ObservedAt {
		t.Fatal("observation timestamps missing")
	}
}
func TestActivityCodexKnownParentsRejectsCycles(t *testing.T) {
	s := &activityCodexScript{}
	r, err := codexActivityRead(t, s, ActivityMetadataQuery{KnownParents: map[string]string{"a": "b", "b": "a"}})
	if err != nil || len(r.Nodes) != 0 {
		t.Fatalf("result %+v err %v", r, err)
	}
	for _, p := range s.requests {
		if p["parentThreadId"] != "root" {
			t.Fatal("queried unproven parent")
		}
	}
}
