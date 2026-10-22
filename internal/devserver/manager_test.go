package devserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultDevScriptOnlyForwardsPortToCompatibleDevTools(t *testing.T) {
	for _, tc := range []struct {
		name, scripts, requested, selected string
		portFlag                           bool
	}{
		{"generic concurrent dev", `{"dev":"concurrently \"npm run server\" \"npm run client\""}`, "", "dev", false},
		{"generic node start fallback", `{"start":"node server.js"}`, "", "start", false},
		{"generic watch", `{"watch":"node watch.js"}`, "watch", "watch", false},
		{"vite dev", `{"dev":"vite --host"}`, "", "dev", true},
		{"next dev", `{"dev":"next dev --turbo"}`, "", "dev", true},
		{"next start fallback", `{"start":"next start"}`, "", "start", true},
		{"nuxt dev", `{"dev":"nuxt dev"}`, "", "dev", true},
		{"nuxi dev", `{"dev":"nuxi dev"}`, "", "dev", true},
		{"astro dev", `{"dev":"astro dev"}`, "", "dev", true},
		{"vite build task", `{"build":"vite build"}`, "build", "build", false},
		{"next build task", `{"build":"next build"}`, "build", "build", false},
		{"shell chain", `{"dev":"vite && echo finished"}`, "", "dev", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, executable := serverFixture(t)
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":`+tc.scripts+`}`), 0600); err != nil {
				t.Fatal(err)
			}
			manager := NewManager(nil, func(_, prefer string) (string, string, error) {
				if prefer != "pnpm" {
					t.Errorf("package manager preference = %q, want pnpm", prefer)
				}
				return executable, "pnpm", nil
			}, func(string) []string { return []string{`C:\selected-node`} }, nil)
			pid, err := manager.Start("demo", root, tc.requested, "pnpm", 3077, "")
			if err != nil {
				t.Fatal(err)
			}
			killServerHelper(t, pid)
			manager.mu.Lock()
			command := manager.procs["demo"].cmd
			manager.mu.Unlock()
			want := []string{executable, "run", tc.selected}
			if tc.portFlag {
				// The selected port may shift if another process occupies 3077.
				want = append(want, "--", "--port", strings.TrimPrefix(findEnv(command.Env, "PORT="), "PORT="))
			}
			if !reflect.DeepEqual(command.Args, want) {
				t.Errorf("launch arguments = %q, want %q", command.Args, want)
			}
			if command.Dir != root || findEnv(command.Env, "PORT=") == "" || !strings.HasPrefix(findEnv(command.Env, "PATH="), `PATH=C:\selected-node;`) {
				t.Errorf("project directory/runtime environment not retained: cwd=%q PATH=%q PORT=%q", command.Dir, findEnv(command.Env, "PATH="), findEnv(command.Env, "PORT="))
			}
		})
	}
}

func findEnv(env []string, prefix string) string {
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			return prefix + entry[len(prefix):]
		}
	}
	return ""
}

func TestSubstitutePort(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		port int
		want string
	}{
		{"token replaced", "php artisan serve --port={port}", 3007, "php artisan serve --port=3007"},
		{"no token left untouched", "composer run dev", 3007, "composer run dev"},
		{"multiple tokens", "x {port} y {port}", 8080, "x 8080 y 8080"},
		{"zero port strips token", "serve --port={port}", 0, "serve --port="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := substitutePort(c.cmd, c.port); got != c.want {
				t.Fatalf("substitutePort(%q, %d) = %q, want %q", c.cmd, c.port, got, c.want)
			}
		})
	}
}

func TestExtractBindingPort(t *testing.T) {
	cases := []struct {
		line     string
		wantPort int
		wantPrio int
	}{
		{"  INFO  Server running on [http://127.0.0.1:8000].", 8000, 2},
		{"➜  Local:   http://localhost:5173/", 5173, 1},
		{"VITE v5.4.0  ready in 312 ms", 0, 0},
		{"started server on 0.0.0.0:3000", 3000, 2},
		{"ready - started server on http://localhost:3000", 3000, 2},
		{"random http://127.0.0.1:9999/path", 9999, 0},
		{"no port here", 0, 0},
	}
	for _, c := range cases {
		port, prio := extractBindingPort(c.line)
		if port != c.wantPort || prio != c.wantPrio {
			t.Errorf("extractBindingPort(%q) = (%d,%d), want (%d,%d)", c.line, port, prio, c.wantPort, c.wantPrio)
		}
	}
}

// simulatePump applies the pump's port-selection rule to a sequence of log
// lines and returns the final chosen port. Mirrors the logic in Start's pump.
func simulatePump(lines []string) int {
	p := &process{port: 3000, portPrio: -1}
	for _, line := range lines {
		if isPortConflictLine(line) {
			continue
		}
		if port, prio := extractBindingPort(line); port > 0 && prio >= p.portPrio && port != p.port {
			p.port = port
			p.portPrio = prio
		}
	}
	return p.port
}

func TestPumpPrefersAppServerOverVite(t *testing.T) {
	// Laravel `composer run dev` interleaves artisan serve (the app) and Vite
	// (assets). The proxy must target the app server regardless of which line
	// prints last.
	artisanFirst := []string{
		"INFO  Server running on [http://127.0.0.1:8000].",
		"VITE v5.4.0  ready in 300 ms",
		"➜  Local:   http://localhost:5173/",
	}
	viteFirst := []string{
		"➜  Local:   http://localhost:5173/",
		"INFO  Server running on [http://127.0.0.1:8000].",
	}
	if got := simulatePump(artisanFirst); got != 8000 {
		t.Errorf("artisan-first: got %d, want 8000", got)
	}
	if got := simulatePump(viteFirst); got != 8000 {
		t.Errorf("vite-first: got %d, want 8000", got)
	}
	// A pure-Vite (SPA) project still gets its single port.
	if got := simulatePump([]string{"➜  Local:   http://localhost:5174/"}); got != 5174 {
		t.Errorf("vite-only: got %d, want 5174", got)
	}
}

func TestStatusKeepsMainAndWorktreeInstancesSeparate(t *testing.T) {
	manager := &Manager{procs: map[string]*process{}}
	manager.procs["demo"] = &process{cmd: &exec.Cmd{Process: &os.Process{Pid: 101}}, instance: "demo", project: "demo", path: `C:\projects\demo`, port: 5173}
	manager.procs["agent-worktree:s1"] = &process{cmd: &exec.Cmd{Process: &os.Process{Pid: 202}}, instance: "agent-worktree:s1", project: "demo", path: `C:\worktrees\demo\feature`, port: 5174}

	main := manager.Status("demo")
	worktree := manager.Status("agent-worktree:s1")
	if main.Port != 5173 || worktree.Port != 5174 || worktree.ProjectPath == main.ProjectPath {
		t.Fatalf("instances were not isolated: main=%#v worktree=%#v", main, worktree)
	}
	if got := len(manager.ListRunning()); got != 2 {
		t.Fatalf("got %d running instances, want 2", got)
	}
}

func TestAllowedHostsEnv(t *testing.T) {
	cases := []struct {
		name  string
		hosts []string
		want  string
	}{
		{"none", nil, ""},
		{"blank entries dropped", []string{"", "  "}, ""},
		{"single", []string{"app.test"}, "__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS=app.test"},
		{"multiple trimmed and deduped", []string{" app.test ", "wt.app.test", "app.test"}, "__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS=app.test,wt.app.test"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := allowedHostsEnv(c.hosts); got != c.want {
				t.Fatalf("allowedHostsEnv(%q) = %q, want %q", c.hosts, got, c.want)
			}
		})
	}
}
