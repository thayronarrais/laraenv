package sourcecontrol

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const structuredLimit int64 = 16 << 20
const consoleLimit = 64 << 10
const queryTimeout = 30 * time.Second

type CommandResult struct {
	Stdout                                                       []byte
	Stderr                                                       string
	ExitCode                                                     int
	HasExitCode, Overflow, Cancelled, TimedOut, ConsoleTruncated bool
	Err                                                          error
}
type commandRunner func(context.Context, string, []string, []byte, int64, func(string, string)) CommandResult

// boundedBuffer always accepts all bytes so exec continues draining both pipes.
type boundedBuffer struct {
	data     []byte
	limit    int64
	overflow bool
}

type redactedConsole struct {
	mu        sync.Mutex
	remaining int
	emit      func(string, string)
	truncated bool
}

func (c *redactedConsole) write(stream, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	text = redact(text)
	if len(text) > c.remaining {
		c.truncated = true
		text = text[:c.remaining]
	}
	if text != "" {
		c.emit(stream, text)
		c.remaining -= len(text)
	}
}

// Keep the unfinished token between writes: URL userinfo can straddle pipe
// chunks. Each stream retains at most one bounded token, and both share the
// console budget. Draining continues after that budget is exhausted.
type consoleStream struct {
	console   *redactedConsole
	name      string
	pending   []byte
	accepted  int
	discarded bool
}

func (s *consoleStream) Write(p []byte) (int, error) {
	n := len(p)
	remaining := consoleLimit - s.accepted
	if len(p) > remaining {
		s.discarded = true
		p = p[:remaining]
	}
	s.accepted += len(p)
	s.pending = append(s.pending, p...)
	last := bytes.LastIndexAny(s.pending, " \t\r\n")
	if last >= 0 {
		s.console.write(s.name, string(s.pending[:last+1]))
		s.pending = append(s.pending[:0], s.pending[last+1:]...)
	}
	// A partial token at the budget boundary may be userinfo whose ':' or '@'
	// has not arrived. Never flush that fragment, even at command completion.
	if s.discarded {
		s.pending = nil
	}
	return n, nil
}
func (s *consoleStream) flush() {
	if len(s.pending) > 0 {
		s.console.write(s.name, string(s.pending))
		s.pending = nil
	}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - int64(len(b.data))
	if int64(n) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func gitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		key = strings.ToUpper(key)
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS":
			continue
		}
		if strings.HasPrefix(key, "GIT_CONFIG") {
			continue
		}
		env = append(env, value)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}

func run(ctx context.Context, dir string, args []string, stdin []byte, stdoutLimit int64, emit func(stream, chunk string)) CommandResult {
	if stdoutLimit < 0 {
		stdoutLimit = 0
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnvironment()
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.WaitDelay = 2 * time.Second
	out := &boundedBuffer{limit: stdoutLimit}
	errout := &boundedBuffer{limit: consoleLimit}
	cmd.Stdout = out
	cmd.Stderr = errout
	var stdoutStream, stderrStream *consoleStream
	if emit != nil {
		console := &redactedConsole{remaining: consoleLimit, emit: emit}
		stdoutStream = &consoleStream{console: console, name: "stdout"}
		stderrStream = &consoleStream{console: console, name: "stderr"}
		cmd.Stdout = io.MultiWriter(out, stdoutStream)
		cmd.Stderr = io.MultiWriter(errout, stderrStream)
	}
	err := runProcess(cmd)
	if emit != nil {
		stdoutStream.flush()
		stderrStream.flush()
	}
	diagnostics := errout.data
	if errout.overflow {
		if last := bytes.LastIndexAny(diagnostics, " \t\r\n"); last >= 0 {
			diagnostics = diagnostics[:last+1]
		} else {
			diagnostics = nil
		}
	}
	r := CommandResult{Stdout: out.data, Stderr: redact(string(diagnostics)), Err: err, Overflow: out.overflow, Cancelled: errors.Is(ctx.Err(), context.Canceled), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded), ConsoleTruncated: errout.overflow}
	if emit != nil {
		r.ConsoleTruncated = r.ConsoleTruncated || stdoutStream.discarded || stderrStream.discarded || stdoutStream.console.truncated
	}
	if cmd.ProcessState != nil {
		r.HasExitCode = true
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	return r
}
