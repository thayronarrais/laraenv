package vhost

import (
	"strings"
	"testing"
)

// Apache's mod_proxy tries every address a hostname resolves to, so pointing
// at localhost covers dev servers bound to either 127.0.0.1 or ::1.
func TestRenderApacheVhostProxyTargetsLocalhost(t *testing.T) {
	out := string(RenderApacheVhost("cycloops-despesas", VhostSpec{
		ServerName: "cycloops-despesas.test",
		ProxyPort:  3000,
	}))
	for _, want := range []string{
		"ProxyPass        / http://localhost:3000/",
		"ProxyPassReverse / http://localhost:3000/",
		"ws://localhost:3000/$1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "127.0.0.1:3000") {
		t.Errorf("proxy must not hardcode 127.0.0.1:\n%s", out)
	}
}
