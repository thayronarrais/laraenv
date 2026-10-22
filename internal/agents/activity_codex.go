package agents

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const codexActivityMaxPageBytes = 2 * 1024 * 1024

// CodexActivityTransport carries only bounded, metadata RPC responses.
// Call treats initialized as a notification; it has no response on the wire.
type CodexActivityTransport interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	Close() error
}

type codexActivityReader struct {
	open func(context.Context, string) (CodexActivityTransport, error)
}

func NewCodexActivityReader(open func(context.Context, string) (CodexActivityTransport, error)) ActivityMetadataReader {
	if open == nil {
		open = OpenCodexActivityTransport
	}
	return &codexActivityReader{open: open}
}

// These fields are an allowlist, deliberately excluding preview, names derived
// from conversation content, turns, commands, runtime status and timestamps.
type codexActivityThread struct {
	ID       string `json:"id"`
	Parent   string `json:"parentThreadId"`
	Nickname string `json:"agentNickname"`
	Role     string `json:"agentRole"`
	Model    string `json:"model"`
	Source   struct {
		Subagent struct {
			Spawn *struct {
				Parent   string `json:"parent_thread_id"`
				Nickname string `json:"agent_nickname"`
				Role     string `json:"agent_role"`
			} `json:"thread_spawn"`
		} `json:"subAgent"`
	} `json:"source"`
}

// The 0.160 protocol supports the verified state-db-only and experimental parent
// filters used below. Unknown/older implementations fail closed before listing.
// Source: openai/codex rust-v0.160.0 app-server-protocol/src/protocol/v2/thread.rs.
// Initialize returns originator/build-version before OS and client versions:
// rust-v0.160.0 login/src/auth/default_client.rs get_codex_user_agent.
var codexActivityVersion = regexp.MustCompile(`^[^ /\r\n]+/([0-9]+)\.([0-9]+)\.([0-9]+)(?: |$)`)

func codexActivitySupported(agent string) bool {
	m := codexActivityVersion.FindStringSubmatch(agent)
	if len(m) != 4 {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 0 || minor >= 160
}

func (r *codexActivityReader) Read(parent context.Context, q ActivityMetadataQuery) (result ActivityMetadataResult, err error) {
	result = ActivityMetadataResult{Nodes: []ActivityMetadataNode{}, Status: ActivityUnavailable, MessageCode: ActivityMessageUnsupportedProtocol, ObservedAt: time.Now().UTC()}
	if strings.TrimSpace(q.RootProviderID) == "" {
		result.MessageCode = ActivityMessageNoProviderSession
		return result, nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	transport, err := r.open(ctx, q.Executable)
	if err != nil {
		return codexActivityFailure(result, err)
	}
	defer transport.Close()
	init, err := transport.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "laraenv_activity", "title": "LaraEnv Activity", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err != nil {
		return codexActivityFailure(result, err)
	}
	var hello struct {
		UserAgent string `json:"userAgent"`
	}
	if len(init) > codexActivityMaxPageBytes || json.Unmarshal(init, &hello) != nil || !codexActivitySupported(hello.UserAgent) {
		return result, nil
	}
	if _, err = transport.Call(ctx, "initialized", map[string]any{}); err != nil {
		return codexActivityFailure(result, err)
	}

	// Only known chains that reach the requested root are usable as parents.
	parents := map[string]string{}
	for child := range q.KnownParents {
		if codexActivityReachesRoot(child, q.RootProviderID, q.KnownParents) {
			parents[child] = q.KnownParents[child]
		}
	}
	queue := []string{q.RootProviderID}
	queued := map[string]bool{q.RootProviderID: true}
	// Existing identities are not queried separately: root traversal rediscovers
	// them, while KnownParents can validate an already-observed parent chain.
	seen := map[string]bool{}
	result.Status = ActivityAvailable
	result.MessageCode = ActivityMessageNone
	result.Capabilities.Children = true
	pages, items := 0, 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		cursor := ""
		cursors := map[string]bool{}
		for {
			if err = ctx.Err(); err != nil {
				return codexActivityFailure(result, err)
			}
			if pages >= 5 || items >= 500 {
				return codexActivityTruncated(result), nil
			}
			params := map[string]any{"parentThreadId": current, "sourceKinds": []string{"subAgentThreadSpawn"}, "useStateDbOnly": true, "limit": 500 - items, "sortKey": "updated_at", "sortDirection": "desc"}
			if cursor != "" {
				params["cursor"] = cursor
			}
			var raw json.RawMessage
			raw, err = transport.Call(ctx, "thread/list", params)
			pages++
			if err != nil {
				return codexActivityFailure(result, err)
			}
			if len(raw) > codexActivityMaxPageBytes {
				return codexActivityFailure(result, errors.New("Codex activity page exceeds metadata limit"))
			}
			var page struct {
				Data []json.RawMessage `json:"data"`
				Next string            `json:"nextCursor"`
			}
			if json.Unmarshal(raw, &page) != nil || page.Data == nil {
				return codexActivityIncompatible(result), nil
			}
			// Validate the whole bounded page before admitting any of its identities.
			remaining := 500 - items
			count := len(page.Data)
			if count > remaining {
				count = remaining
			}
			threads := make([]codexActivityThread, 0, count)
			for _, entry := range page.Data[:count] {
				items++
				var thread codexActivityThread
				if json.Unmarshal(entry, &thread) != nil || thread.ID == "" || thread.ID == q.RootProviderID || thread.ID == current || thread.Parent != current || thread.Source.Subagent.Spawn == nil || thread.Source.Subagent.Spawn.Parent != current {
					return codexActivityIncompatible(result), nil
				}
				if old, ok := parents[thread.ID]; ok && old != current {
					return codexActivityIncompatible(result), nil
				}
				if current != q.RootProviderID && !codexActivityReachesRoot(current, q.RootProviderID, parents) {
					return codexActivityIncompatible(result), nil
				}
				threads = append(threads, thread)
			}
			for _, thread := range threads {
				if seen[thread.ID] {
					continue
				}
				seen[thread.ID] = true
				parents[thread.ID] = current
				nickname := thread.Nickname
				if nickname == "" {
					nickname = thread.Source.Subagent.Spawn.Nickname
				}
				role := thread.Role
				if role == "" {
					role = thread.Source.Subagent.Spawn.Role
				}
				node := ActivityMetadataNode{ProviderNodeID: thread.ID, ParentProviderID: current, Label: codexActivityText(nickname), AgentType: codexActivityText(role), FirstObservedAt: result.ObservedAt, LastObservedAt: result.ObservedAt}
				if model := codexActivityText(thread.Model); model != "" {
					node.Model = &ActivityModel{Name: model, Kind: ActivityModelConfigured}
					result.Capabilities.Model = true
				}
				result.Nodes = append(result.Nodes, node)
				if !queued[thread.ID] {
					queued[thread.ID] = true
					queue = append(queue, thread.ID)
				}
			}
			if len(page.Data) > remaining {
				return codexActivityTruncated(result), nil
			}
			if page.Next == "" {
				break
			}
			if cursors[page.Next] {
				return codexActivityTruncated(result), nil
			}
			cursors[page.Next] = true
			cursor = page.Next
		}
	}
	return result, nil
}

func codexActivityReachesRoot(id, root string, parents map[string]string) bool {
	visited := map[string]bool{}
	for id != root {
		if id == "" || visited[id] || len(visited) >= 500 {
			return false
		}
		visited[id] = true
		id = parents[id]
	}
	return true
}
func codexActivityText(value string) string {
	result := []rune{}
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		if len(result) == 240 {
			break
		}
		result = append(result, r)
	}
	return strings.TrimSpace(string(result))
}
func codexActivityTruncated(r ActivityMetadataResult) ActivityMetadataResult {
	r.Truncated = true
	r.Status = ActivityPartial
	r.MessageCode = ActivityMessageTruncated
	return r
}
func codexActivityIncompatible(r ActivityMetadataResult) ActivityMetadataResult {
	r.Nodes = []ActivityMetadataNode{}
	r.Capabilities = ActivityCapabilities{}
	r.Status = ActivityUnavailable
	r.MessageCode = ActivityMessageUnsupportedProtocol
	return r
}
func codexActivityFailure(r ActivityMetadataResult, err error) (ActivityMetadataResult, error) {
	var rpc *codexActivityRPCError
	if errors.As(err, &rpc) && (rpc.Code == -32601 || rpc.Code == -32602) {
		return codexActivityIncompatible(r), nil
	}
	r.Status = ActivityStatusError
	r.MessageCode = ActivityMessageRefreshFailed
	r.Stale = true
	return r, err
}
