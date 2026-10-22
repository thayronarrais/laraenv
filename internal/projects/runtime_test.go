package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runtimeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func phpCandidates() []RuntimeCandidate {
	return []RuntimeCandidate{
		{Version: "8.2", FullVersion: "8.2.33"},
		{Version: "8.3", FullVersion: "8.3.15"},
		{Version: "8.4", FullVersion: "8.4.2"},
		{Version: "8.5", FullVersion: "8.5.10"},
		{Version: "8.6", FullVersion: "8.6.0beta3", Prerelease: true},
	}
}

func nodeCandidates() []RuntimeCandidate {
	return []RuntimeCandidate{
		{Version: "22.11.0", FullVersion: "22.11.0", LTS: true},
		{Version: "22.22.2", FullVersion: "22.22.2", LTS: true},
		{Version: "24.15.0", FullVersion: "24.15.0", LTS: true},
		{Version: "25.0.0", FullVersion: "25.0.0"},
		{Version: "26.0.0-beta.1", FullVersion: "26.0.0-beta.1", Prerelease: true},
	}
}

func TestResolveEmptyProjectPathUsesGlobalInsteadOfCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	runtimeFile(t, root, ".php-version", "8.5")
	runtimeFile(t, root, "composer.json", `{"require":{"php":"^8.5"}}`)
	runtimeFile(t, root, ".nvmrc", "25")
	runtimeFile(t, root, "package.json", `{"engines":{"node":"25"}}`)
	t.Chdir(root)
	for _, tt := range []struct {
		kind string
		got  RuntimeResolution
		want string
	}{
		{"php", ResolvePHP("", "", "8.2", phpCandidates(), nil), "8.2"},
		{"node", ResolveNode("", "", "22.11.0", nodeCandidates(), nil), "22.11.0"},
	} {
		if tt.got.Version != tt.want || tt.got.Source != "global" || tt.got.Status != "resolved" || tt.got.Constraint != "" {
			t.Errorf("%s without checkout used current-directory requirement: %+v, want global %s", tt.kind, tt.got, tt.want)
		}
	}
}

func TestResolvePHPConstraints(t *testing.T) {
	tests := []struct{ constraint, want, status string }{
		{"^8.2", "8.5", "resolved"},
		{">=8.1 <8.4", "8.3", "resolved"},
		{"8.3.*", "8.3", "resolved"},
		{"~8.2", "8.5", "resolved"},
		{"~8.3.1", "8.3", "resolved"},
		{"8.3", "", "missing"}, // Composer partials are exact, not series.
		{"8.4.2", "8.4", "resolved"},
		{">=8.4.20 <8.5", "", "missing"}, // Never invent patch precision from folder.
		{"8.2 - 8.4", "8.4", "resolved"},
		{"8.2.0 - 8.3.15", "8.3", "resolved"},
		{"<8.3 || >=8.4 <8.5", "8.4", "resolved"},
		{"<8.3 | >=8.4 <8.5", "8.4", "resolved"},
		{">=8.2, <8.4, !=8.3.15", "8.2", "resolved"},
		{"=8.3.15", "8.3", "resolved"},
		{"8.6.0beta3", "8.6", "resolved"},
		{">=8.6.0-beta.1 <8.6.0", "", "missing"},
		{">=8.6.0-beta.1 <8.6.0-stable", "8.6", "resolved"},
		{"*", "8.5", "resolved"},
		{"^8.4@dev", "", "invalid"},
		{"banana", "", "invalid"},
		{">=8.2 ||", "", "invalid"},
		{"", "", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			root := t.TempDir()
			runtimeFile(t, root, "composer.json", `{"require":{"php":"`+tt.constraint+`"}}`)
			got := ResolvePHP(root, "", "8.2", phpCandidates(), nil)
			if got.Version != tt.want || got.Status != tt.status || got.Source != "composer.json" {
				t.Fatalf("got %+v, want version=%q status=%q source=composer.json", got, tt.want, tt.status)
			}
		})
	}
}

func TestResolvePHPExplicitAndManualPrecedence(t *testing.T) {
	root := t.TempDir()
	runtimeFile(t, root, "composer.json", `{"require":{"php":"^8.4"}}`)
	runtimeFile(t, root, ".php-version", "8.3\n")
	got := ResolvePHP(root, "", "8.2", phpCandidates(), nil)
	if got.Version != "8.3" || got.Source != ".php-version" || got.Status != "resolved" {
		t.Fatalf("explicit file must beat composer: %+v", got)
	}
	got = ResolvePHP(root, "8.4", "8.2", phpCandidates(), nil)
	if got.Version != "8.4" || got.Source != "manual" || got.Status != "incompatible" {
		t.Fatalf("manual must remain selected but diagnose explicit series mismatch: %+v", got)
	}
	got = ResolvePHP(root, "9.0", "8.2", phpCandidates(), nil)
	if got.Version != "" || got.Source != "manual" || got.Status != "missing" {
		t.Fatalf("missing manual must not fall back: %+v", got)
	}
	got = ResolvePHP(root, "8.3.30", "8.2", phpCandidates(), nil)
	if got.Version != "8.3" || got.FullVersion != "8.3.15" || got.Status != "resolved" {
		t.Fatalf("legacy PHP override may map to installed series without pretending exact patch: %+v", got)
	}
	runtimeFile(t, root, ".php-version", "8.6.0beta3")
	got = ResolvePHP(root, "", "8.2", phpCandidates(), nil)
	if got.Version != "8.6" || got.Status != "resolved" {
		t.Fatalf("explicit prerelease should resolve: %+v", got)
	}
}

func TestResolveRuntimeMissingSuggestions(t *testing.T) {
	root := t.TempDir()
	runtimeFile(t, root, "composer.json", `{"require":{"php":">=8.4.20 <8.5"}}`)
	got := ResolvePHP(root, "", "8.3", phpCandidates(), []RuntimeCandidate{{Version: "8.4", FullVersion: "8.4.25"}, {Version: "8.5", FullVersion: "8.5.10"}})
	if got.Status != "missing" || got.Version != "" || got.InstallVersion != "8.4.25" {
		t.Fatalf("missing installed match should suggest compatible catalog patch: %+v", got)
	}
	runtimeFile(t, root, ".nvmrc", "28")
	got = ResolveNode(root, "", "24.15.0", nodeCandidates(), []RuntimeCandidate{{Version: "28.1.0", FullVersion: "28.1.0"}, {Version: "28.2.0-beta.1", FullVersion: "28.2.0-beta.1", Prerelease: true}})
	if got.Status != "missing" || got.Version != "" || got.InstallVersion != "28.1.0" {
		t.Fatalf("missing node should suggest stable catalog match: %+v", got)
	}
}

func TestResolveNodeConstraintsAndChain(t *testing.T) {
	tests := []struct{ constraint, want, status string }{
		{"~22.11", "22.11.0", "resolved"},
		{"^22.11.0", "22.22.2", "resolved"},
		{"22", "22.22.2", "resolved"},
		{"v22.11.0", "22.11.0", "resolved"},
		{">=22 <25", "24.15.0", "resolved"},
		{"22.11 - 24.15", "24.15.0", "resolved"},
		{"22.x || 24.x", "24.15.0", "resolved"},
		{"node", "25.0.0", "resolved"},
		{"stable", "25.0.0", "resolved"},
		{"lts/*", "24.15.0", "resolved"},
		{"26.0.0-beta.1", "26.0.0-beta.1", "resolved"},
		{"<0.0.0", "", "missing"},
		{"lts/jod", "", "invalid"},
		{"22 ||", "", "invalid"},
		{"", "", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			root := t.TempDir()
			runtimeFile(t, root, ".nvmrc", tt.constraint)
			got := ResolveNode(root, "", "22.11.0", nodeCandidates(), nil)
			if got.Version != tt.want || got.Status != tt.status || got.Source != ".nvmrc" {
				t.Fatalf("got %+v, want %q/%q/.nvmrc", got, tt.want, tt.status)
			}
		})
	}
	root := t.TempDir()
	runtimeFile(t, root, "package.json", `{"engines":{"node":"22"}}`)
	runtimeFile(t, root, ".node-version", "24")
	runtimeFile(t, root, ".nvmrc", "22.11.0")
	if got := ResolveNode(root, "", "", nodeCandidates(), nil); got.Version != "22.11.0" || got.Source != ".nvmrc" {
		t.Fatalf("nvmrc precedence: %+v", got)
	}
	os.Remove(filepath.Join(root, ".nvmrc"))
	if got := ResolveNode(root, "", "", nodeCandidates(), nil); got.Version != "24.15.0" || got.Source != ".node-version" {
		t.Fatalf("node-version precedence: %+v", got)
	}
	os.Remove(filepath.Join(root, ".node-version"))
	if got := ResolveNode(root, "", "", nodeCandidates(), nil); got.Version != "22.22.2" || got.Source != "package.json" {
		t.Fatalf("engines fallback: %+v", got)
	}
}

func TestResolveRuntimeDefaultsAndUnknownMetadata(t *testing.T) {
	root := t.TempDir()
	if got := ResolvePHP(root, "", "8.3", phpCandidates(), nil); got.Version != "8.3" || got.Source != "global" {
		t.Fatalf("global: %+v", got)
	}
	if got := ResolvePHP(root, "", "removed", phpCandidates(), nil); got.Version != "8.5" || got.Source != "global" {
		t.Fatalf("highest stable fallback: %+v", got)
	}
	if got := ResolveNode(root, "", "", nil, nil); got.Status != "unmanaged" || got.Version != "" {
		t.Fatalf("unmanaged node: %+v", got)
	}
	unknown := []RuntimeCandidate{{Version: "8.4"}}
	if got := ResolvePHP(root, "8.4", "", unknown, nil); got.Status != "resolved" || got.Version != "8.4" {
		t.Fatalf("manual unknown: %+v", got)
	}
	if got := ResolvePHP(root, "", "8.4", unknown, nil); got.Status != "resolved" || got.Version != "8.4" {
		t.Fatalf("global unknown: %+v", got)
	}
	runtimeFile(t, root, "composer.json", `{"require":{"php":"^8.2"}}`)
	if got := ResolvePHP(root, "", "8.4", unknown, nil); got.Status != "missing" || got.Version != "" {
		t.Fatalf("unknown cannot satisfy constraint: %+v", got)
	}
	if got := ResolvePHP(root, "8.4", "", unknown, nil); got.Version != "8.4" || got.Problem != "unknown_runtime_version" {
		t.Fatalf("selected unknown must diagnose compatibility uncertainty: %+v", got)
	}
}

func TestResolveRuntimeMalformedFilesDoNotFallBack(t *testing.T) {
	tests := []struct {
		name, content, problem string
		dir                    bool
	}{
		{".php-version", "", "empty_version_file", false},
		{".php-version", "8.4\n8.3", "invalid_constraint", false},
		{".php-version", strings.Repeat("8", 5000), "file_too_large", false},
		{".php-version", "", "unreadable_file", true},
		{"composer.json", "{", "invalid_manifest", false},
		{"composer.json", `{"require":{"php":82}}`, "invalid_manifest", false},
		{"composer.json", strings.Repeat(" ", 1024*1024+1), "file_too_large", false},
	}
	for _, tt := range tests {
		t.Run(tt.name+tt.problem, func(t *testing.T) {
			root := t.TempDir()
			if tt.dir {
				os.Mkdir(filepath.Join(root, tt.name), 0700)
			} else {
				runtimeFile(t, root, tt.name, tt.content)
			}
			got := ResolvePHP(root, "", "8.3", phpCandidates(), nil)
			if got.Status != "invalid" || got.Version != "" || got.Problem != tt.problem {
				t.Fatalf("invalid cannot silently fall back: %+v", got)
			}
		})
	}
}

func TestResolveRuntimeWorktreeIsIndependent(t *testing.T) {
	root, worktree := t.TempDir(), t.TempDir()
	runtimeFile(t, root, "composer.json", `{"require":{"php":"8.2.*"}}`)
	runtimeFile(t, worktree, "composer.json", `{"require":{"php":"8.4.*"}}`)
	if got := ResolvePHP(root, "", "", phpCandidates(), nil); got.Version != "8.2" {
		t.Fatalf("main: %+v", got)
	}
	if got := ResolvePHP(worktree, "", "", phpCandidates(), nil); got.Version != "8.4" {
		t.Fatalf("worktree: %+v", got)
	}
}

func TestResolveRuntimeRangeBoundarySemantics(t *testing.T) {
	tests := []struct {
		kind, constraint, candidate string
		matches                     bool
	}{
		{"php", "~8.2", "8.9.9", true},
		{"php", "~8.2", "9.0.0", false},
		{"node", "~8.2", "8.3.0", false},
		{"node", "~8.2", "8.2.9", true},
		{"php", "^0.0.3", "0.0.4", false},
		{"node", "^0.0", "0.0.9", true},
		{"node", "^0.0", "0.1.0", false},
		{"node", "^0", "0.9.0", true},
		{"node", "^0", "1.0.0", false},
		{"node", ">22", "22.99.0", false},
		{"node", ">22", "23.0.0", true},
		{"node", "<=22.11", "22.11.99", true},
		{"node", "<=22.11", "22.12.0", false},
		{"php", "<=8.3", "8.3.15", false},
		{"php", ">8.3", "8.3.15", true},
		{"node", ">=22.0.0-beta.1 <23", "22.0.0-beta.2", true},
		{"node", ">=22.0.0-beta.1 <23", "22.1.0-beta.2", false},
		{"node", ">=26.0.0-beta.1 <26", "26.0.0-beta.2", false},
		{"node", ">=26.0.0-beta.1 <26.0.0", "26.0.0-beta.2", true},
		{"node", ">=26.0.0-beta.1 <26.0", "26.0.0-beta.2", false},
		{"node", "26 >=27.0.0-beta.1", "27.0.0-beta.2", false},
		{"node", "26.x >=27.0.0-beta.1", "27.0.0-beta.2", false},
		{"node", "<=26 >=27.0.0-beta.1", "27.0.0-beta.2", false},
		{"node", "^26.0.0 >=27.0.0-beta.1", "27.0.0-beta.2", false},
		{"node", "~26.1 >=26.2.0-beta.1", "26.2.0-beta.2", false},
		{"node", "26.1.0-beta.1 - 26.0", "26.1.0-beta.2", false},
		{"node", "26.1.0-beta.1 - 26.1.0", "26.1.0-beta.2", true},
		{"php", ">=8.6.0-dev <8.6.0-alpha1", "8.6.0-dev", true},
		{"php", ">=8.6.0-beta2 <8.7.0", "8.6.0beta10", true},
		{"php", "~8.6.0-beta2", "8.7.0beta1", false},
	}
	for _, tt := range tests {
		t.Run(tt.kind+tt.constraint+tt.candidate, func(t *testing.T) {
			root := t.TempDir()
			installed := []RuntimeCandidate{{Version: "fixture", FullVersion: tt.candidate}}
			var got RuntimeResolution
			if tt.kind == "php" {
				runtimeFile(t, root, "composer.json", `{"require":{"php":"`+tt.constraint+`"}}`)
				got = ResolvePHP(root, "", "", installed, nil)
			} else {
				runtimeFile(t, root, "package.json", `{"engines":{"node":"`+tt.constraint+`"}}`)
				got = ResolveNode(root, "", "", installed, nil)
			}
			if (got.Status == "resolved") != tt.matches || got.Status == "invalid" {
				t.Fatalf("got %+v, want matched=%v", got, tt.matches)
			}
		})
	}
}

func TestResolveRuntimeManualDiagnostics(t *testing.T) {
	root := t.TempDir()
	runtimeFile(t, root, "package.json", `{"engines":{"node":"^24"}}`)
	if got := ResolveNode(root, "22.22.2", "24.15.0", nodeCandidates(), nil); got.Version != "22.22.2" || got.Status != "incompatible" || got.Problem != "incompatible_override" {
		t.Fatalf("manual mismatch: %+v", got)
	}
	runtimeFile(t, root, "package.json", "{")
	if got := ResolveNode(root, "22.22.2", "24.15.0", nodeCandidates(), nil); got.Version != "22.22.2" || got.Status != "invalid" || got.Problem != "invalid_manifest" {
		t.Fatalf("manual malformed manifest: %+v", got)
	}
}

func TestResolveRuntimeAbsentConstraintUsesGlobal(t *testing.T) {
	root := t.TempDir()
	runtimeFile(t, root, "composer.json", `{"require":{"laravel/framework":"^12"}}`)
	runtimeFile(t, root, "package.json", `{"scripts":{"dev":"vite"}}`)
	if got := ResolvePHP(root, "", "8.2", phpCandidates(), nil); got.Version != "8.2" || got.Source != "global" {
		t.Fatalf("composer without php: %+v", got)
	}
	if got := ResolveNode(root, "", "22.11.0", nodeCandidates(), nil); got.Version != "22.11.0" || got.Source != "global" {
		t.Fatalf("package without engines: %+v", got)
	}
}
