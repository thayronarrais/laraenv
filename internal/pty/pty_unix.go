//go:build !windows

package pty

import (
	"os"
	"os/exec"

	cpty "github.com/creack/pty"
)

type unixSession struct {
	f   *os.File
	cmd *exec.Cmd
}

func Start(shell string, cols, rows int, env []string, cwd string) (Session, error) {
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell)
	if len(env) > 0 {
		cmd.Env = env
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	f, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixSession{f: f, cmd: cmd}, nil
}

func StartCommand(executable string, args []string, cols, rows int, env []string, cwd string) (Session, error) {
	cmd := exec.Command(executable, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	f, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixSession{f: f, cmd: cmd}, nil
}

func (s *unixSession) Read(p []byte) (int, error)  { return s.f.Read(p) }
func (s *unixSession) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s *unixSession) Close() error {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return s.f.Close()
}
func (s *unixSession) Resize(cols, rows int) error {
	return cpty.Setsize(s.f, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
