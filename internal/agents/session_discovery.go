package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"lara4ever/internal/i18n"
)

type SessionIDDiscoverer func(context.Context, AgentProfile, string) (map[string]struct{}, error)

// DiscoverSessionIDs reads only provider-owned session metadata exposed by an
// official command/protocol, documented metadata index, or documented session
// filename. It never reads prompts, transcripts, tool calls, credentials or
// transcript contents.
func DiscoverSessionIDs(ctx context.Context, profile AgentProfile, cwd string) (map[string]struct{}, error) {
	switch profile.ID {
	case AgentClaude:
		return discoverClaudeSessionIDs(cwd)
	case AgentCodex:
		return discoverCodexSessionIDs(ctx, profile.Executable, cwd)
	case AgentKimi:
		return discoverKimiSessionIDs(cwd)
	case AgentOpenCode:
		return discoverOpenCodeSessionIDs(ctx, profile.Executable, cwd)
	default:
		return nil, i18n.Errorf("errors.agents.discoveryNotSupported", nil)
	}
}

// Claude documents transcripts at
// ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl. Listing those filenames
// is enough to validate a caller-assigned UUID without reading conversation
// content. Directory matching is case-insensitive for Windows drive letters.
func discoverClaudeSessionIDs(cwd string) (map[string]struct{}, error) {
	configDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".claude")
	}
	projectsDir := filepath.Join(configDir, "projects")
	projects, err := os.ReadDir(projectsDir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]struct{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	wanted := claudeProjectDirectory(cwd)
	var projectDir string
	for _, entry := range projects {
		if entry.IsDir() && strings.EqualFold(entry.Name(), wanted) {
			projectDir = filepath.Join(projectsDir, entry.Name())
			break
		}
	}
	if projectDir == "" {
		return map[string]struct{}{}, nil
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids, nil
}

func claudeProjectDirectory(cwd string) string {
	var encoded strings.Builder
	for _, char := range filepath.Clean(strings.TrimSpace(cwd)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			encoded.WriteRune(char)
		} else {
			encoded.WriteByte('-')
		}
	}
	return encoded.String()
}

func discoverCodexSessionIDs(ctx context.Context, executable, cwd string) (map[string]struct{}, error) {
	result, err := callCodexAppServer(ctx, executable, "thread/list", map[string]any{
		"limit": 100, "cwd": []string{cwd}, "sortKey": "created_at", "sortDirection": "desc",
	})
	if err != nil {
		return nil, err
	}
	return parseCodexSessionIDs(result)
}

func parseCodexSessionIDs(data []byte) (map[string]struct{}, error) {
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, i18n.Errorf("errors.agents.decodeCodexSessionMetadataFailed", nil)
	}
	ids := make(map[string]struct{}, len(page.Data))
	for _, thread := range page.Data {
		if thread.ID != "" {
			ids[thread.ID] = struct{}{}
		}
	}
	return ids, nil
}

func callCodexAppServer(ctx context.Context, executable, method string, params any) (json.RawMessage, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, i18n.Errorf("errors.agents.codexExecutableUnavailable", nil)
	}
	command := exec.CommandContext(ctx, executable, "app-server", "--stdio")
	hideUsageWindow(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()
	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	if err := encoder.Encode(map[string]any{
		"method": "initialize", "id": 1,
		"params": map[string]any{"clientInfo": map[string]string{"name": "laraenv", "title": "LaraEnv", "version": "1"}, "capabilities": map[string]any{}},
	}); err != nil {
		return nil, err
	}
	if _, err := scanCodexResponse(scanner, 1); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	request := map[string]any{"method": method, "id": 2}
	if params != nil {
		request["params"] = params
	}
	if err := encoder.Encode(request); err != nil {
		return nil, err
	}
	response, err := scanCodexResponse(scanner, 2)
	if err != nil {
		return nil, err
	}
	return response.Result, nil
}

func discoverOpenCodeSessionIDs(ctx context.Context, executable, cwd string) (map[string]struct{}, error) {
	command := exec.CommandContext(ctx, executable, "session", "list", "--format", "json", "--max-count", "200")
	command.Dir = cwd
	hideUsageWindow(command)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, i18n.Errorf("errors.agents.openCodeSessionMetadataCommandFailed", nil)
	}
	if stdout.Len() > 8*1024*1024 {
		return nil, i18n.Errorf("errors.agents.openCodeSessionMetadataTooLarge", nil)
	}
	return parseOpenCodeSessionIDs(stdout.Bytes(), cwd)
}

func parseOpenCodeSessionIDs(data []byte, cwd string) (map[string]struct{}, error) {
	var sessions []struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, i18n.Errorf("errors.agents.decodeOpenCodeSessionMetadataFailed", nil)
	}
	wanted := NormalizeProjectPath(cwd)
	ids := make(map[string]struct{})
	for _, session := range sessions {
		if session.ID != "" && NormalizeProjectPath(session.Directory) == wanted {
			ids[session.ID] = struct{}{}
		}
	}
	return ids, nil
}

func discoverKimiSessionIDs(cwd string) (map[string]struct{}, error) {
	home := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(userHome, ".kimi-code")
	}
	file, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]struct{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parseKimiSessionIDs(file, cwd)
}

func parseKimiSessionIDs(reader io.Reader, cwd string) (map[string]struct{}, error) {
	wanted := NormalizeProjectPath(cwd)
	ids := make(map[string]struct{})
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			SessionIDSnake string `json:"session_id"`
			SessionIDCamel string `json:"sessionId"`
			WorkDirSnake   string `json:"work_dir"`
			WorkDirCamel   string `json:"workDir"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		id := firstNonEmptyString(entry.SessionIDCamel, entry.SessionIDSnake)
		workDir := firstNonEmptyString(entry.WorkDirCamel, entry.WorkDirSnake)
		if id != "" && NormalizeProjectPath(workDir) == wanted {
			ids[id] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, i18n.Wrapf(err, "errors.agents.readKimiSessionMetadataFailed", nil)
	}
	return ids, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
