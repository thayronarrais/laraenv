package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func mcpDBFixture(t *testing.T) mcpDatabaseAdapter {
	t.Helper()
	root := t.TempDir()
	return mcpDatabaseAdapter{stateRoot: filepath.Join(root, "state"), mysqlBin: filepath.Join(root, "mysql", "bin"), postgresBin: filepath.Join(root, "postgres", "bin"), status: func(string) string { return "running" }}
}

func TestMCPDatabaseListUsesLocalConnectionAndReturnsNamesOnly(t *testing.T) {
	adapter := mcpDBFixture(t)
	adapter.run = func(ctx context.Context, executable string, args []string, stdout io.Writer) error {
		if filepath.Base(executable) != "mysql.exe" || !reflect.DeepEqual(args, []string{"--no-defaults", "--protocol=TCP", "-h", "127.0.0.1", "-P", "3306", "-u", "root", "--skip-password", "--batch", "--raw", "--skip-column-names", "-e", "SHOW DATABASES;"}) {
			t.Fatalf("unexpected local database command: %s %v", executable, args)
		}
		_, err := io.WriteString(stdout, "mysql\ndemo\ndemo\ninformation_schema\n")
		return err
	}
	result, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "list", engine: "mysql"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := result.(mcpDatabaseList)
	if !reflect.DeepEqual(list.Databases, []mcpDatabaseEntry{{Name: "demo"}, {Name: "information_schema", System: true}, {Name: "mysql", System: true}}) {
		t.Fatalf("unexpected databases: %#v", list)
	}
}

func TestMCPDatabaseRejectsUnsafeMutationsBeforeExecution(t *testing.T) {
	for _, input := range []mcpDatabaseRequest{
		{action: "create", engine: "mysql", database: "demo`; DROP DATABASE mysql;--"},
		{action: "snapshot", engine: "postgres", database: "../outside"},
		{action: "create", engine: "remote", database: "demo"},
		{action: "drop", engine: "mysql", database: "demo"},
		{action: "drop", engine: "mysql", database: "MySQL", confirm: true},
		{action: "drop", engine: "postgres", database: "template1", confirm: true},
		{action: "create", engine: "postgres", database: strings.Repeat("a", 64)},
	} {
		t.Run(input.action+"_"+input.database, func(t *testing.T) {
			adapter := mcpDBFixture(t)
			adapter.run = func(context.Context, string, []string, io.Writer) error {
				t.Fatal("unsafe request reached command execution")
				return nil
			}
			if _, err := adapter.call(context.Background(), input, nil); err == nil {
				t.Fatal("unsafe database request accepted")
			}
		})
	}
}

func TestMCPDatabaseCreatesAndDropsWithValidatedIdentifiers(t *testing.T) {
	for _, action := range []string{"create", "drop"} {
		t.Run(action, func(t *testing.T) {
			adapter := mcpDBFixture(t)
			adapter.run = func(_ context.Context, executable string, args []string, _ io.Writer) error {
				if filepath.Base(executable) != "mysql.exe" || args[2] != "-h" || args[3] != "127.0.0.1" {
					t.Fatalf("non-local command: %s %v", executable, args)
				}
				query := args[len(args)-1]
				if action == "create" && query != "CREATE DATABASE IF NOT EXISTS `demo_app` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;" {
					t.Fatal(query)
				}
				if action == "drop" && query != "DROP DATABASE IF EXISTS `demo_app`;" {
					t.Fatal(query)
				}
				return nil
			}
			result, err := adapter.call(context.Background(), mcpDatabaseRequest{action: action, engine: "mysql", database: "demo_app", confirm: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.(mcpDatabaseMutation).Database != "demo_app" {
				t.Fatal(result)
			}
		})
	}
}

func TestMCPDatabaseSnapshotIsUniqueOwnedAndContainsDump(t *testing.T) {
	adapter := mcpDBFixture(t)
	adapter.run = func(_ context.Context, executable string, args []string, stdout io.Writer) error {
		if filepath.Base(executable) != "pg_dump.exe" || !reflect.DeepEqual(args, []string{"-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "--no-password", "--format=plain", "--dbname=demo"}) {
			t.Fatalf("unsafe dump command: %s %v", executable, args)
		}
		_, err := io.WriteString(stdout, "-- database dump\n")
		return err
	}
	var previous string
	for range 2 {
		result, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "snapshot", engine: "postgres", database: "demo"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		snapshot := result.(mcpDatabaseSnapshot)
		if snapshot.Path == previous || filepath.Dir(snapshot.Path) != filepath.Join(adapter.stateRoot, "mcp-snapshots") || len(filepath.Base(snapshot.Path)) > 128 {
			t.Fatalf("invalid snapshot location: %#v", snapshot)
		}
		data, err := os.ReadFile(snapshot.Path)
		if err != nil || string(data) != "-- database dump\n" || snapshot.Bytes != int64(len(data)) {
			t.Fatalf("invalid snapshot data: %q %v", data, err)
		}
		previous = snapshot.Path
	}
}

func TestMCPDatabaseSnapshotRemovesFailedPartialAndRedactsCLIError(t *testing.T) {
	adapter := mcpDBFixture(t)
	adapter.run = func(_ context.Context, _ string, _ []string, stdout io.Writer) error {
		_, _ = io.WriteString(stdout, "partial secret table")
		return errors.New("MYSQL_PWD=secret .env contents")
	}
	_, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "snapshot", engine: "mysql", database: "demo"}, nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unredacted failure: %v", err)
	}
	files, readErr := os.ReadDir(filepath.Join(adapter.stateRoot, "mcp-snapshots"))
	if readErr != nil || len(files) != 0 {
		t.Fatalf("partial dump was retained: %v %v", files, readErr)
	}
}

func TestMCPDatabaseCancellationRemovesPartial(t *testing.T) {
	adapter := mcpDBFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	adapter.run = func(ctx context.Context, _ string, _ []string, stdout io.Writer) error {
		_, _ = io.WriteString(stdout, "partial")
		cancel()
		return ctx.Err()
	}
	_, err := adapter.call(ctx, mcpDatabaseRequest{action: "snapshot", engine: "mysql", database: "demo"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	files, _ := os.ReadDir(filepath.Join(adapter.stateRoot, "mcp-snapshots"))
	if len(files) != 0 {
		t.Fatal("canceled snapshot was retained")
	}
}

func TestMCPDatabaseRequiresRunningOwnedEngine(t *testing.T) {
	adapter := mcpDBFixture(t)
	adapter.status = func(string) string { return "stopped" }
	adapter.run = func(context.Context, string, []string, io.Writer) error {
		t.Fatal("stopped engine queried")
		return nil
	}
	if _, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "list", engine: "mysql"}, nil); err == nil {
		t.Fatal("stopped engine accepted")
	}
}

func TestMCPDatabasePostgresClientContracts(t *testing.T) {
	for _, test := range []struct {
		action, executable string
		args               []string
	}{
		{"list", "psql.exe", []string{"-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "--no-password", "--no-psqlrc", "--dbname=postgres", "--tuples-only", "--no-align", "-c", "SELECT datname FROM pg_database ORDER BY datname;"}},
		{"create", "createdb.exe", []string{"-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "--no-password", "--", "demo_app"}},
		{"drop", "dropdb.exe", []string{"-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "--no-password", "--if-exists", "--", "demo_app"}},
	} {
		t.Run(test.action, func(t *testing.T) {
			adapter := mcpDBFixture(t)
			adapter.run = func(_ context.Context, executable string, args []string, stdout io.Writer) error {
				if filepath.Base(executable) != test.executable || !reflect.DeepEqual(args, test.args) {
					t.Fatalf("unexpected PostgreSQL command: %s %v", executable, args)
				}
				if test.action == "list" {
					_, err := io.WriteString(stdout, "postgres\r\ndemo_app\r\ntemplate1\r\n")
					return err
				}
				return nil
			}
			result, err := adapter.call(context.Background(), mcpDatabaseRequest{action: test.action, engine: "postgres", database: "demo_app", confirm: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.action == "list" && !reflect.DeepEqual(result.(mcpDatabaseList).Databases, []mcpDatabaseEntry{{Name: "demo_app"}, {Name: "postgres", System: true}, {Name: "template1", System: true}}) {
				t.Fatal(result)
			}
		})
	}
}

func TestMCPDatabaseListingIsBoundedEvenWhenRunnerIgnoresWriterError(t *testing.T) {
	adapter := mcpDBFixture(t)
	adapter.run = func(_ context.Context, _ string, _ []string, stdout io.Writer) error {
		_, _ = io.WriteString(stdout, strings.Repeat("a", mcpDatabaseListLimit+1))
		return nil
	}
	if _, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "list", engine: "mysql"}, nil); err == nil {
		t.Fatal("oversized database listing accepted")
	}
}

func TestMCPDatabaseWriterDoesNotWriteBeyondLimit(t *testing.T) {
	var output strings.Builder
	writer := &mcpDatabaseWriter{writer: &output, remaining: 4}
	if n, err := writer.Write([]byte("secret data")); err == nil || n != 4 || output.String() != "secr" || !writer.exceeded {
		t.Fatalf("limit was not enforced: %d %v %q", n, err, output.String())
	}
	if n, err := writer.Write([]byte("more")); err == nil || n != 0 || output.String() != "secr" {
		t.Fatalf("limit was exceeded on a later write: %d %v %q", n, err, output.String())
	}
}

func TestMCPDatabaseSnapshotsCanPrepareDirectoryConcurrently(t *testing.T) {
	adapter := mcpDBFixture(t)
	adapter.run = func(_ context.Context, _ string, _ []string, stdout io.Writer) error {
		_, err := io.WriteString(stdout, "-- fixture\n")
		return err
	}
	var group sync.WaitGroup
	results := make(chan string, 16)
	errors := make(chan error, 16)
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "snapshot", engine: "mysql", database: "demo"}, nil)
			if err != nil {
				errors <- err
				return
			}
			results <- result.(mcpDatabaseSnapshot).Path
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	seen := map[string]bool{}
	for path := range results {
		if seen[path] {
			t.Error("concurrent snapshots reused a filename")
		}
		seen[path] = true
	}
	if len(seen) != 16 {
		t.Fatalf("only %d snapshots completed", len(seen))
	}
}

func TestMCPDatabaseSnapshotRefusesLinkedOwnedState(t *testing.T) {
	adapter := mcpDBFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, adapter.stateRoot); err != nil {
		t.Skip("symbolic links unavailable on this host")
	}
	adapter.run = func(context.Context, string, []string, io.Writer) error {
		t.Fatal("linked state reached dump client")
		return nil
	}
	if _, err := adapter.call(context.Background(), mcpDatabaseRequest{action: "snapshot", engine: "mysql", database: "demo"}, nil); err == nil {
		t.Fatal("linked state accepted")
	}
	files, err := os.ReadDir(outside)
	if err != nil || len(files) != 0 {
		t.Fatalf("snapshot preparation escaped owned state: %v %v", files, err)
	}
}
