package main

import (
	"path/filepath"
	"testing"

	"lara4ever/internal/config"
	"lara4ever/internal/projectroutes"
)

func TestLocalTLSProjectSelectsOnlyTheServingCheckout(t *testing.T) {
	root := t.TempDir()
	mainPath, worktree := filepath.Join(root, "shop"), filepath.Join(root, "worktree")
	prefs := config.Projects{Root: root, TLD: "custom.local", SSL: map[string]bool{"shop": true}}
	routes := []projectroutes.Route{{ProjectName: "shop", Hostname: "preview.custom.local", TargetPath: worktree, SSL: true}}
	if got := localTLSProject(prefs, "shop", mainPath, routes); got != nil {
		t.Fatal("canonical certificate assigned to a checkout that is no longer routed")
	}
	got := localTLSProject(prefs, "shop", worktree, routes)
	if got == nil || got.Hostname != "preview.custom.local" || got.Path != worktree {
		t.Fatalf("wrong preview TLS context: %+v", got)
	}
	got = localTLSProject(prefs, "shop", mainPath, nil)
	if got == nil || got.Hostname != "shop.custom.local" {
		t.Fatalf("custom TLD lost: %+v", got)
	}
	prefs.SSL["shop"] = false
	if got := localTLSProject(prefs, "shop", mainPath, nil); got != nil {
		t.Fatal("HTTP project received Vite HTTPS settings")
	}
	prefs.SSL["shop"] = true
	prefs.ProxyPorts = map[string]int{"shop": 5173}
	if got := localTLSProject(prefs, "shop", mainPath, nil); got != nil {
		t.Fatal("HTTP reverse proxy would be changed to a TLS upstream without updating the proxy")
	}
}

func TestLocalTLSProjectUsesCurrentServingPreferences(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "shop")
	for _, tc := range []struct {
		name       string
		savedSSL   bool
		currentSSL bool
		proxyPort  int
		wantTLS    bool
	}{
		{"enabled after route creation", false, true, 0, true},
		{"disabled after route creation", true, false, 0, false},
		{"HTTP proxy enabled after route creation", true, true, 5173, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefs := config.Projects{Root: root, TLD: "test", SSL: map[string]bool{"shop": tc.currentSSL}, ProxyPorts: map[string]int{"shop": tc.proxyPort}}
			routes := []projectroutes.Route{{ProjectName: "shop", Hostname: "shop.test", TargetPath: cwd, Canonical: true, TargetKind: projectroutes.TargetMainCheckout, SSL: tc.savedSSL}}
			got := localTLSProject(prefs, "shop", cwd, routes)
			if (got != nil) != tc.wantTLS {
				t.Fatalf("TLS context = %+v, want enabled %v using current preferences", got, tc.wantTLS)
			}
		})
	}
}

func TestLocalTLSProjectClearsStoredCanonicalProxy(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "shop")
	prefs := config.Projects{Root: root, TLD: "test", SSL: map[string]bool{"shop": true}, ProxyPorts: map[string]int{"shop": 0}}
	routes := []projectroutes.Route{{ProjectName: "shop", Hostname: "shop.test", TargetPath: cwd, Canonical: true, TargetKind: projectroutes.TargetMainCheckout, SSL: true, ProxyPort: 5173}}
	if got := localTLSProject(prefs, "shop", cwd, routes); got == nil || got.Hostname != "shop.test" {
		t.Fatalf("cleared canonical proxy retained stale route state: %+v", got)
	}
}
