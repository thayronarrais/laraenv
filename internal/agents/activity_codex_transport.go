package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type codexActivityRPCError struct {
	Code int `json:"code"`
}

func (e *codexActivityRPCError) Error() string { return "Codex activity RPC rejected" }

type codexActivityFrame struct {
	data []byte
	err  error
}
type codexActivityStdioTransport struct {
	command    *exec.Cmd
	stdin      io.WriteCloser
	frames     chan codexActivityFrame
	done       chan struct{}
	readerDone chan struct{}
	once       sync.Once
	mu         sync.Mutex
	nextID     int
}

// OpenCodexActivityTransport starts an isolated metadata client. It never
// attaches/resumes a thread or starts a turn and inherits provider daemon mode.
func OpenCodexActivityTransport(ctx context.Context, executable string) (CodexActivityTransport, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("Codex activity executable unavailable")
	}
	return newCodexActivityStdioTransport(ctx, exec.CommandContext(ctx, executable, "app-server", "--stdio"))
}

func newCodexActivityStdioTransport(ctx context.Context, command *exec.Cmd) (*codexActivityStdioTransport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hideUsageWindow(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	t := &codexActivityStdioTransport{command: command, stdin: stdin, frames: make(chan codexActivityFrame, 1), done: make(chan struct{}), readerDone: make(chan struct{})}
	go func() {
		defer close(t.readerDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), codexActivityMaxPageBytes+1)
		for scanner.Scan() {
			frame := codexActivityFrame{data: append([]byte(nil), scanner.Bytes()...)}
			if len(frame.data) > codexActivityMaxPageBytes {
				frame = codexActivityFrame{err: errors.New("Codex activity frame exceeds metadata limit")}
			}
			select {
			case t.frames <- frame:
			case <-t.done:
				return
			}
			if frame.err != nil {
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		} else {
			err = errors.New("Codex activity invalid or oversized frame")
		}
		select {
		case t.frames <- codexActivityFrame{err: err}:
		case <-t.done:
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = t.Close()
		case <-t.done:
		}
	}()
	return t, nil
}

func (t *codexActivityStdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-t.done:
		return nil, errors.New("Codex activity transport closed")
	default:
	}
	// Keep this transport incapable of sending mutating/provider-history RPCs.
	switch method {
	case "initialize", "initialized", "thread/list":
	default:
		return nil, errors.New("Codex activity RPC not allowed")
	}
	request := map[string]any{"method": method, "params": params}
	if method != "initialized" {
		t.nextID++
		request["id"] = t.nextID
	}
	if err := json.NewEncoder(t.stdin).Encode(request); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Codex activity request failed")
	}
	if method == "initialized" {
		return json.RawMessage(`{}`), nil
	}
	for {
		select {
		case <-ctx.Done():
			_ = t.Close()
			return nil, ctx.Err()
		case <-t.done:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("Codex activity transport closed")
		case frame := <-t.frames:
			if frame.err != nil {
				return nil, frame.err
			}
			var response struct {
				ID     *int                   `json:"id"`
				Method string                 `json:"method"`
				Result json.RawMessage        `json:"result"`
				Error  *codexActivityRPCError `json:"error"`
			}
			if json.Unmarshal(frame.data, &response) != nil {
				return nil, errors.New("Codex activity invalid RPC response")
			}
			if response.ID == nil && response.Method != "" {
				continue
			}
			if response.ID == nil || *response.ID != t.nextID {
				return nil, errors.New("Codex activity unexpected RPC response")
			}
			if response.Error != nil {
				return nil, response.Error
			}
			if len(response.Result) == 0 {
				return nil, errors.New("Codex activity missing RPC result")
			}
			return response.Result, nil
		}
	}
}

func (t *codexActivityStdioTransport) Close() error {
	t.once.Do(func() {
		close(t.done)
		_ = t.stdin.Close()
		_ = t.command.Process.Kill()
		_ = t.command.Wait()
		<-t.readerDone
	})
	return nil
}
