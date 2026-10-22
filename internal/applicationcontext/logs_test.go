package applicationcontext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// Losing daily-channel entries or merging a stack trace into the next exception
// would make this diagnostic evidence misleading.
func TestLogsMergeDailyAndSingleWithStructuredErrors(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "storage/logs/laravel.log", "[2026-10-17 10:00:00] local.INFO: old\n")
	fixtureFile(t, root, "storage/logs/laravel-2026-10-19.log", "[2026-10-19 11:00:00] local.ERROR: Failed token=private {\"exception\":\"[object] (RuntimeException(code: 0): Broken at /app/Foo.php:9)\"}\n[stacktrace]\n#0 /app/Foo.php(9)\n[2026-10-19 11:01:00] local.INFO: Recovered\n")
	fixtureFile(t, root, "storage/logs/other.log", "PRIVATE_OTHER_CHANNEL")
	got, err := ReadLogs(context.Background(), root, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "available" || len(got.Entries) != 3 || len(got.Errors) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Errors[0].Level != "ERROR" || !strings.Contains(got.Errors[0].Stack, "#0") || got.Entries[2].Message != "Recovered" {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Errors[0].Message, "private") {
		t.Fatal("credential assignment was not redacted")
	}
}

func TestLogsBoundMalformedFilesAndCancellation(t *testing.T) {
	root := t.TempDir()
	got, err := ReadLogs(context.Background(), root, 20)
	if err != nil || got.Status != "unavailable" {
		t.Fatalf("%+v %v", got, err)
	}
	fixtureFile(t, root, "storage/logs/laravel-2026-99-99.log", "not a date")
	fixtureFile(t, root, "storage/logs/laravel.log", strings.Repeat("x", 2<<20))
	got, err = ReadLogs(context.Background(), root, 2)
	if err != nil || !got.Truncated || len(got.Lines) > 2 || len(strings.Join(got.Lines, "")) > 16384 {
		t.Fatalf("%+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = ReadLogs(ctx, root, 10); err == nil {
		t.Fatal("cancelled read accepted")
	}
	if _, err = ReadLogs(context.Background(), root, 501); err == nil {
		t.Fatal("unbounded read accepted")
	}
}

func TestLogTailAcrossDailyFilesKeepsNewestEvidence(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "storage/logs/laravel-2026-10-18.log", "[2026-10-18 12:00:00] local.ERROR: yesterday\n")
	fixtureFile(t, root, "storage/logs/laravel-2026-10-19.log", "[2026-10-19 12:00:00] local.ERROR: today\n")
	got, err := ReadLogs(context.Background(), root, 1)
	if err != nil || len(got.Lines) != 1 || !strings.Contains(got.Lines[0], "today") {
		t.Fatalf("old evidence selected: %+v %v", got, err)
	}
}

func TestReadersRejectEvidenceLinksOutsideProject(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	fixtureFile(t, outside, "laravel.log", "PRIVATE_OUTSIDE")
	if err := os.MkdirAll(filepath.Join(root, "storage", "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "laravel.log"), filepath.Join(root, "storage", "logs", "laravel.log")); err != nil {
		t.Skipf("symlink not permitted: %v", err)
	}
	if _, err := ReadLogs(context.Background(), root, 10); err == nil {
		t.Fatal("external log followed")
	}
	if err := os.MkdirAll(filepath.Join(root, "storage", "laraenv"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "laravel.log"), filepath.Join(root, "storage", "laraenv", "context-2026-10-19.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCapture(context.Background(), root, "demo", 10); err == nil {
		t.Fatal("external capture followed")
	}
}
