//go:build windows

package cron

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Killing only cmd.exe leaves both helpers running with inherited output pipes.
// The runner must return promptly and terminate the entire command tree.
func TestCronDeadlineTerminatesCommandTree(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	finished := make(chan struct{})
	command := `call %LARAENV_CRON_HELPER_EXE% -test.run=^TestCronProcessHelper$ -- child .`
	go func() {
		defer close(finished)
		output, err := runCommand(ctx, command, dir, append(os.Environ(), "LARAENV_CRON_PROCESS_HELPER=1", `LARAENV_CRON_HELPER_EXE="`+executable+`"`))
		done <- result{output, err}
	}()
	t.Cleanup(func() {
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("command runner remained blocked after fixture cleanup")
		}
	})

	var handles []windows.Handle
	for _, name := range []string{"child", "grandchild"} {
		pidPath := filepath.Join(dir, name+".pid")
		deadline := time.Now().Add(5 * time.Second)
		var pid int
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(pidPath); err == nil {
				pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if pid > 0 {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		if pid == 0 {
			select {
			case result := <-done:
				t.Fatalf("%s did not start: %v %s", name, result.err, result.output)
			default:
			}
			t.Fatalf("%s did not start", name)
		}
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
		if err != nil {
			t.Fatalf("open %s process: %v", name, err)
		}
		handles = append(handles, handle)
		// Retained handles prevent PID reuse from touching any unrelated process.
		t.Cleanup(func() {
			_ = windows.TerminateProcess(handle, 1)
			if state, err := windows.WaitForSingleObject(handle, 3000); err != nil || state != windows.WAIT_OBJECT_0 {
				t.Errorf("fixture process was not cleaned up: state=%d err=%v", state, err)
			}
			_ = windows.CloseHandle(handle)
		})
	}
	select {
	case result := <-done:
		if !errors.Is(result.err, context.DeadlineExceeded) {
			t.Errorf("run error = %v, want deadline exceeded", result.err)
		}
	case <-time.After(7 * time.Second):
		t.Error("cron command did not return after its deadline; descendants still own output pipes")
	}
	for i, handle := range handles {
		state, err := windows.WaitForSingleObject(handle, 0)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			t.Errorf("descendant %d survived deadline: state=%d err=%v", i, state, err)
		}
	}
}

func TestCronCommandAlreadyCancelledDoesNotSpawn(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runCommand(ctx, "echo unwanted > spawned.txt", dir, os.Environ())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want canceled", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "spawned.txt")); !os.IsNotExist(err) {
		t.Fatalf("canceled command spawned: %v", err)
	}
}

func TestCronCommandOutputAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, command, output string
		wantError             bool
	}{
		{"success", "echo cron-output", "cron-output", false},
		{"missing command", "laraenv-cron-command-that-does-not-exist", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			output, err := runCommand(ctx, tc.command, t.TempDir(), os.Environ())
			if (err != nil) != tc.wantError {
				t.Fatalf("run error = %v; wantError=%v output=%s", err, tc.wantError, output)
			}
			if !tc.wantError && strings.TrimSpace(string(output)) != tc.output {
				t.Fatalf("output = %q; want %q", output, tc.output)
			}
		})
	}
}

func TestCronProcessHelper(t *testing.T) {
	if os.Getenv("LARAENV_CRON_PROCESS_HELPER") != "1" {
		return
	}
	args := os.Args
	mode, dir := args[len(args)-2], args[len(args)-1]
	if mode != "child" && mode != "grandchild" {
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(dir, mode+".pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(3)
	}
	if mode == "child" {
		cmd := exec.Command(args[0], "-test.run=^TestCronProcessHelper$", "--", "grandchild", dir)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		if err := cmd.Start(); err != nil {
			os.Exit(4)
		}
		_ = cmd.Wait()
	} else {
		// Self-expire as a fallback even if the test runner is interrupted.
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}
