package vhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseNginxConf = `worker_processes  1;
events {
    worker_connections  1024;
}
http {
    include       mime.types;
    default_type  application/octet-stream;
    sendfile        on;
    server {
        listen       80;
        server_name  localhost;
    }
}
`

func TestEnsurePhpUpstreamsInjectAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte(baseNginxConf), 0644); err != nil {
		t.Fatal(err)
	}

	ups := []PhpUpstream{
		{Name: "php_8_1", Ports: []int{9081, 9181, 9281, 9381}},
		{Name: "php_8_4", Ports: []int{9084, 9184}},
	}
	if err := EnsurePhpUpstreams(conf, ups); err != nil {
		t.Fatalf("EnsurePhpUpstreams: %v", err)
	}
	got, _ := os.ReadFile(conf)
	s := string(got)
	for _, want := range []string{
		"upstream php_8_1 {",
		"server 127.0.0.1:9081;",
		"server 127.0.0.1:9381;",
		"upstream php_8_4 {",
		"server 127.0.0.1:9184;",
		phpUpstreamBegin,
		phpUpstreamEnd,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	// Upstreams must live inside the http {} block.
	httpIdx := strings.Index(s, "http {")
	if strings.Index(s, "upstream php_8_1") < httpIdx {
		t.Errorf("upstream injected outside http {} block")
	}

	// Idempotent: same input → byte-identical.
	before, _ := os.ReadFile(conf)
	if err := EnsurePhpUpstreams(conf, ups); err != nil {
		t.Fatalf("EnsurePhpUpstreams 2: %v", err)
	}
	after, _ := os.ReadFile(conf)
	if string(before) != string(after) {
		t.Errorf("second call changed content (not idempotent)")
	}

	// Changing worker count rewrites the managed region in place (no duplication).
	if err := EnsurePhpUpstreams(conf, []PhpUpstream{{Name: "php_8_1", Ports: []int{9081, 9181}}}); err != nil {
		t.Fatalf("EnsurePhpUpstreams 3: %v", err)
	}
	got3, _ := os.ReadFile(conf)
	s3 := string(got3)
	if strings.Count(s3, phpUpstreamBegin) != 1 {
		t.Errorf("managed region duplicated: %d sentinels", strings.Count(s3, phpUpstreamBegin))
	}
	if strings.Contains(s3, "server 127.0.0.1:9281;") {
		t.Errorf("stale worker port 9281 still present after shrink:\n%s", s3)
	}
	if strings.Contains(s3, "upstream php_8_4") {
		t.Errorf("removed version php_8_4 still present after refresh")
	}
}
