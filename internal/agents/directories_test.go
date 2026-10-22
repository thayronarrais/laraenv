package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestAdditionalDirectoriesNormalizeDeduplicateAndExcludePrimary(t *testing.T) {
	primary := t.TempDir()
	extra := filepath.Join(t.TempDir(), "shared & source")
	if err := os.Mkdir(extra, 0755); err != nil {
		t.Fatal(err)
	}
	paths := []string{" " + extra + " ", extra + string(filepath.Separator) + ".", primary}
	if runtime.GOOS == "windows" {
		paths = append(paths, strings.ToUpper(extra))
	}
	got, err := validateAdditionalDirectories(AgentProfile{ID: AgentCodex}, paths)
	if err != nil {
		t.Fatal(err)
	}
	got = excludePrimaryDirectory(primary, got)
	if !reflect.DeepEqual(got, []string{extra}) {
		t.Fatalf("directories = %#v, want only %q", got, extra)
	}
}

func TestAdditionalDirectoriesRejectInvalidPaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative/path", filepath.Join(t.TempDir(), "missing"), file, "C:\\invalid\npath"} {
		t.Run(path, func(t *testing.T) {
			if _, err := validateAdditionalDirectories(AgentProfile{ID: AgentClaude}, []string{path}); err == nil {
				t.Fatalf("path %q should be rejected", path)
			}
		})
	}
}
