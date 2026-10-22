//go:build windows

package pty

import (
	"context"
	"strings"
	"syscall"

	"github.com/UserExistsError/conpty"
)

type winSession struct {
	c *conpty.ConPty
}

func StartCommand(executable string, args []string, cols, rows int, env []string, cwd string) (Session, error) {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(executable))
	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}
	return Start(strings.Join(parts, " "), cols, rows, env, cwd)
}

func Start(shell string, cols, rows int, env []string, cwd string) (Session, error) {
	if shell == "" {
		shell = "powershell.exe"
	}
	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(cols, rows)}
	if len(env) > 0 {
		opts = append(opts, conpty.ConPtyEnv(env))
	}
	if cwd != "" {
		opts = append(opts, conpty.ConPtyWorkDir(cwd))
	}
	c, err := conpty.Start(shell, opts...)
	if err != nil {
		return nil, err
	}
	return &winSession{c: c}, nil
}

// Wait blocks on the hosted process's handle. This is the only way a
// pseudoconsole reports that the shell is gone: conhost.exe keeps the output
// pipe open, so Read below would block indefinitely instead of ending.
func (s *winSession) Wait() error {
	_, err := s.c.Wait(context.Background())
	return err
}

func (s *winSession) Read(p []byte) (int, error)  { return s.c.Read(p) }
func (s *winSession) Write(p []byte) (int, error) { return s.c.Write(p) }
func (s *winSession) Close() error                { return s.c.Close() }
func (s *winSession) Resize(cols, rows int) error { return s.c.Resize(cols, rows) }
