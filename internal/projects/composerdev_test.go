package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripPailFromLine(t *testing.T) {
	in := `npx concurrently -c "#93c5fd,#c4b5fd,#fb7185,#fdba74" "php artisan serve" "php artisan queue:listen --tries=1" "php artisan pail --timeout=0" "npm run dev" --names=server,queue,logs,vite --kill-others`
	want := `npx concurrently -c "#93c5fd,#c4b5fd,#fb7185,#fdba74" "php artisan serve" "php artisan queue:listen --tries=1" "npm run dev" --names=server,queue,vite --kill-others`
	got, changed := stripPailFromLine(in)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if got != want {
		t.Fatalf("stripPailFromLine mismatch:\n got: %s\nwant: %s", got, want)
	}
	// Idempotent: running again is a no-op.
	if _, again := stripPailFromLine(got); again {
		t.Fatal("expected second pass to be a no-op")
	}
}

func TestStripPailFromLineNoPail(t *testing.T) {
	in := `npx concurrently "php artisan serve" "npm run dev" --names=server,vite --kill-others`
	if _, changed := stripPailFromLine(in); changed {
		t.Fatal("expected no change when there is no pail")
	}
}

func TestStripPailFromDevScript(t *testing.T) {
	dir := t.TempDir()
	composer := `{
    "name": "laravel/laravel",
    "scripts": {
        "dev": [
            "Composer\\Config::disableProcessTimeout",
            "npx concurrently -c \"#93c5fd,#c4b5fd,#fb7185,#fdba74\" \"php artisan serve\" \"php artisan queue:listen --tries=1\" \"php artisan pail --timeout=0\" \"npm run dev\" --names=server,queue,logs,vite --kill-others"
        ],
        "test": ["@php artisan test"]
    }
}`
	path := filepath.Join(dir, "composer.json")
	if err := os.WriteFile(path, []byte(composer), 0644); err != nil {
		t.Fatal(err)
	}
	changed, summary, err := StripPailFromDevScript(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatalf("expected changed=true, summary=%q", summary)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	if strings.Contains(s, "artisan pail") {
		t.Errorf("pail still present:\n%s", s)
	}
	if strings.Contains(s, "logs") {
		t.Errorf("--names still has logs:\n%s", s)
	}
	// Untouched bits preserved.
	if !strings.Contains(s, "disableProcessTimeout") || !strings.Contains(s, `"@php artisan test"`) {
		t.Errorf("unrelated content not preserved:\n%s", s)
	}
	// Idempotent.
	changed2, _, _ := StripPailFromDevScript(dir)
	if changed2 {
		t.Error("expected second run to be a no-op")
	}
}
