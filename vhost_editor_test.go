package main

import (
	"path/filepath"
	"testing"

	"lara4ever/internal/paths"
	"lara4ever/internal/vhost"
)

// testApp builds a minimal *App with just the configurators vhostFileFor
// needs, so the path is derived through the same VhostPath methods used to
// generate the files, not re-derived by hand in the test.
func testApp() *App {
	return &App{
		nginx:  vhost.NewNginxConfigurator(paths.NginxSites()),
		apache: vhost.NewApacheConfigurator(paths.ApacheSites()),
		caddy:  vhost.NewCaddyConfigurator(paths.CaddySites()),
	}
}

func TestVhostFileForResolvesEachServer(t *testing.T) {
	a := testApp()
	for _, tc := range []struct{ server, want string }{
		{"nginx", filepath.Join(paths.NginxSites(), "shop.conf")},
		{"apache", filepath.Join(paths.ApacheSites(), "shop.conf")},
		{"caddy", filepath.Join(paths.CaddySites(), "shop.caddy")},
	} {
		got, err := a.vhostFileFor("shop", tc.server)
		if err != nil {
			t.Errorf("%s: %v", tc.server, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.server, got, tc.want)
		}
	}
}

// An unknown server must not fall through to a default: silently editing the
// nginx config when the user asked for another one is worse than an error.
func TestVhostFileForRejectsAnUnknownServer(t *testing.T) {
	a := testApp()
	if _, err := a.vhostFileFor("shop", "lighttpd"); err == nil {
		t.Fatal("want an error for an unknown server")
	}
}
