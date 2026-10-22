package cron

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeValidatorBlocksCommandBeforePathResolution(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "spawned.txt")
	job := Job{ID: "test", ProjectName: "demo", WorkingDir: "ignored", PhpVersion: "8.3", Command: "echo spawned > spawned.txt", LastOutput: "previous"}
	var persisted []Job
	var emitted []Job
	var pathCalled bool
	m := NewManager(func(jobs []Job) error { persisted = jobs; return nil },
		func(project, override string) []string { pathCalled = true; return nil },
		func(project, override string) string { return root },
	)
	m.jobs[job.ID] = &job
	m.SetEmit(func(j Job) { emitted = append(emitted, j) })
	var validationArgs []string
	m.SetRuntimeValidator(func(project, workDir, phpOverride string) error {
		validationArgs = []string{project, workDir, phpOverride}
		return errors.New("required PHP 8.4 is not installed")
	})
	m.execute(&job)
	if !reflect.DeepEqual(validationArgs, []string{"demo", root, "8.3"}) {
		t.Fatalf("validator args=%v", validationArgs)
	}
	if pathCalled {
		t.Fatal("PATH fallback was resolved after runtime rejection")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("command ran despite invalid runtime: %v", err)
	}
	if job.LastStatus != "error" || job.LastError != "required PHP 8.4 is not installed" || job.LastOutput != "" || job.LastRun.IsZero() {
		t.Fatalf("job=%+v", job)
	}
	if len(persisted) != 1 || persisted[0].LastStatus != "error" || len(emitted) != 2 || emitted[1].LastStatus != "error" {
		t.Fatalf("persisted=%+v emitted=%+v", persisted, emitted)
	}
}

func TestRuntimeValidatorExplicitJobPHPReachesValidationAndPath(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "spawned.txt")
	job := Job{ID: "test", ProjectName: "demo", WorkingDir: root, PhpVersion: "8.3", Command: "echo selected > spawned.txt"}
	var validated, resolved string
	m := NewManager(nil, func(project, override string) []string { resolved = override; return nil }, nil)
	m.jobs[job.ID] = &job
	m.SetRuntimeValidator(func(project, workDir, phpOverride string) error {
		validated = phpOverride
		if workDir != root {
			t.Errorf("workDir=%q", workDir)
		}
		return nil
	})
	m.execute(&job)
	if validated != "8.3" || resolved != "8.3" || job.LastStatus != "ok" {
		t.Fatalf("validated=%q resolved=%q job=%+v", validated, resolved, job)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("valid command did not run: %v", err)
	}
}

func TestBackgroundRunnerUsesRuntimeValidatorAndPreservesOtherConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	marker := filepath.Join(root, "spawned.txt")
	job := Job{ID: "background", Name: "job", Enabled: true, BackgroundMode: true, ProjectName: "demo", WorkingDir: root, PhpVersion: "8.3", Schedule: "* * * * *", Command: "echo spawned > spawned.txt"}
	data, err := json.Marshal(map[string]any{"crons": []Job{job}, "language": "pt-BR"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var got []string
	err = RunOnceFromConfig(configPath, job.ID, nil, nil, func(project, cwd, override string) error {
		got = []string{project, cwd, override}
		return errors.New("Node 24 required")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"demo", root, "8.3"}) {
		t.Fatalf("validator args=%v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("background command spawned: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Crons    []Job  `json:"crons"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.Language != "pt-BR" || len(config.Crons) != 1 || config.Crons[0].LastError != "Node 24 required" || config.Crons[0].LastStatus != "error" {
		t.Fatalf("config=%+v", config)
	}
}

func TestRuntimePathResolverUsesActualWorkingDirectoryAndJobPin(t *testing.T) {
	root := t.TempDir()
	job := Job{ID: "runtime-path", ProjectName: "demo", WorkingDir: "worktree override", PhpVersion: "8.3", Command: "echo %PATH%"}
	m := NewManager(nil, func(string, string) []string { t.Error("legacy resolver was used"); return nil }, func(string, string) string { return root })
	m.jobs[job.ID] = &job
	var args []string
	m.SetRuntimePathResolver(func(project, cwd, override string) ([]string, error) {
		args = []string{project, cwd, override}
		return []string{`C:\selected-php`, `C:\selected-node`}, nil
	})
	m.execute(&job)
	if !reflect.DeepEqual(args, []string{"demo", root, "8.3"}) {
		t.Fatalf("runtime path arguments=%v", args)
	}
	if job.LastStatus != "ok" || !strings.HasPrefix(job.LastOutput, `C:\selected-php;C:\selected-node;`) {
		t.Fatalf("resolved PATH was not used: %+v", job)
	}
}

func TestRuntimePathResolverErrorSkipsCommandAndFallback(t *testing.T) {
	root := t.TempDir()
	job := Job{ID: "runtime-path", WorkingDir: root, Command: "echo spawned > spawned.txt"}
	m := NewManager(nil, func(string, string) []string { t.Error("legacy fallback was used"); return nil }, nil)
	m.jobs[job.ID] = &job
	m.SetRuntimePathResolver(func(string, string, string) ([]string, error) { return nil, errors.New("missing Node 24") })
	m.execute(&job)
	if job.LastStatus != "error" || job.LastError != "missing Node 24" {
		t.Fatalf("job=%+v", job)
	}
	if _, err := os.Stat(filepath.Join(root, "spawned.txt")); !os.IsNotExist(err) {
		t.Fatalf("command ran despite path rejection: %v", err)
	}
}

func TestBackgroundRunnerResolvesRuntimePathsForActualWorktree(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	job := Job{ID: "background-path", Enabled: true, BackgroundMode: true, ProjectName: "demo", WorkingDir: root, PhpVersion: "8.3", Schedule: "* * * * *", Command: "echo %PATH%"}
	data, err := json.Marshal(map[string]any{"crons": []Job{job}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var got []string
	err = RunOnceWithRuntimeFromConfig(configPath, job.ID, nil, nil, nil, func(project, cwd, override string) ([]string, error) {
		got = []string{project, cwd, override}
		return []string{`C:\worktree-node`}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"demo", root, "8.3"}) {
		t.Fatalf("runtime path args=%v", got)
	}
	saved, err := loadJob(configPath, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LastStatus != "ok" || !strings.HasPrefix(saved.LastOutput, `C:\worktree-node;`) {
		t.Fatalf("job=%+v", saved)
	}
}
