package vhost

import (
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/paths"
)

// The log directives are absolute, because nginx would otherwise resolve a
// relative path against its own prefix. Absolute means they have to follow
// the install root: a portable copy running from another drive was writing
// its logs to the default location, which does not exist there, and nginx
// refused to start with "CreateFile() ... failed (3)".
func TestNginxVhostLogsUnderTheResolvedRoot(t *testing.T) {
	conf := string(RenderNginxVhost("shop", VhostSpec{
		ServerName: "shop.test",
		DocRoot:    filepath.Join(paths.ProjectsRoot(), "shop", "public"),
		Kind:       "laravel",
	}))

	want := filepath.ToSlash(paths.NginxRoot()) + "/logs/shop.access.log"
	if !strings.Contains(conf, want) {
		t.Fatalf("no access_log at %q in:\n%s", want, conf)
	}
	if strings.Contains(conf, "access_log logs/") {
		t.Error("relative log path: nginx would resolve it against its own prefix")
	}
}

func TestApacheLogsResolveUnderTheInstallRoot(t *testing.T) {
	if got, want := apacheLogsForward(), filepath.ToSlash(paths.ApacheLogs()); got != want {
		t.Fatalf("apacheLogsForward() = %q, want %q", got, want)
	}
}
