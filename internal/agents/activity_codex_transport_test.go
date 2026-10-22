package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The subprocess is this test binary, never an installed provider CLI.
func TestActivityCodexStdioHelper(t *testing.T) {
	mode := os.Getenv("LARAENV_ACTIVITY_CODEX_HELPER")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(3)
		}
		if request.Method == "initialized" {
			continue
		}
		if mode == "hang" && request.Method == "thread/list" {
			// Wait on the live pipe; a bare select{} would trigger the Go
			// runtime's deadlock detector and exit instead of testing cancellation.
			scanner.Scan()
			os.Exit(0)
		}
		if mode == "oversized" && request.Method == "thread/list" {
			fmt.Fprintln(os.Stdout, strings.Repeat("x", 2*1024*1024+1))
			continue
		}
		if mode == "rpc-error" && request.Method == "thread/list" {
			_ = encoder.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32602, "message": "synthetic private error must not escape"}})
			continue
		}
		result := json.RawMessage(`{"data":[],"nextCursor":null}`)
		if request.Method == "initialize" {
			var p struct {
				Capabilities struct {
					Experimental bool `json:"experimentalApi"`
				} `json:"capabilities"`
			}
			_ = json.Unmarshal(request.Params, &p)
			if !p.Capabilities.Experimental {
				os.Exit(4)
			}
			result = json.RawMessage(`{"userAgent":"codex/0.160.0"}`)
		}
		_ = encoder.Encode(map[string]any{"method": "synthetic/notification", "params": map[string]bool{"ignored": true}})
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
	os.Exit(0)
}
func activityCodexHelperOpen(t *testing.T, mode string) (func(context.Context, string) (CodexActivityTransport, error), **codexActivityStdioTransport) {
	t.Helper()
	var transport *codexActivityStdioTransport
	return func(ctx context.Context, _ string) (CodexActivityTransport, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestActivityCodexStdioHelper$")
		cmd.Env = append(os.Environ(), "LARAENV_ACTIVITY_CODEX_HELPER="+mode)
		transport, err = newCodexActivityStdioTransport(ctx, cmd)
		return transport, err
	}, &transport
}
func TestActivityCodexTransportProcessClosed(t *testing.T) {
	open, ptr := activityCodexHelperOpen(t, "normal")
	r, err := NewCodexActivityReader(open).Read(t.Context(), ActivityMetadataQuery{Executable: "synthetic", RootProviderID: "root"})
	if err != nil || r.Status != ActivityAvailable {
		t.Fatalf("result %+v error %v", r, err)
	}
	if (*ptr).command.ProcessState == nil {
		t.Fatal("process not reaped")
	}
	if err := (*ptr).Close(); err != nil {
		t.Fatal(err)
	}
}
func TestActivityCodexCancellationClosesProcess(t *testing.T) {
	open, ptr := activityCodexHelperOpen(t, "hang")
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	_, err := NewCodexActivityReader(open).Read(ctx, ActivityMetadataQuery{Executable: "synthetic", RootProviderID: "root"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v", err)
	}
	if *ptr == nil || (*ptr).command.ProcessState == nil {
		t.Fatal("cancelled process not reaped")
	}
}
func TestActivityCodexTransportOversizedClosesProcess(t *testing.T) {
	open, ptr := activityCodexHelperOpen(t, "oversized")
	_, err := NewCodexActivityReader(open).Read(t.Context(), ActivityMetadataQuery{Executable: "synthetic", RootProviderID: "root"})
	if err == nil {
		t.Fatal("oversized frame accepted")
	}
	if (*ptr).command.ProcessState == nil {
		t.Fatal("failed process not reaped")
	}
}
func TestActivityCodexTransportUnsupportedRPC(t *testing.T) {
	open, ptr := activityCodexHelperOpen(t, "rpc-error")
	r, err := NewCodexActivityReader(open).Read(t.Context(), ActivityMetadataQuery{Executable: "synthetic", RootProviderID: "root"})
	if err != nil || r.MessageCode != ActivityMessageUnsupportedProtocol {
		t.Fatalf("result %+v error %v", r, err)
	}
	if (*ptr).command.ProcessState == nil {
		t.Fatal("failed process not reaped")
	}
}
