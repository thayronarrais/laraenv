package projects

import "testing"

func TestDetectPackageManagerDeclarationWinsLockfile(t *testing.T) {
	for _, pm := range []string{"npm", "pnpm", "yarn", "bun"} {
		t.Run(pm, func(t *testing.T) {
			root := t.TempDir()
			runtimeFile(t, root, "package.json", `{"packageManager":"`+pm+`@10.1.0"}`)
			runtimeFile(t, root, "pnpm-lock.yaml", "")
			if got := Detect(root).PackageManager; got != pm {
				t.Fatalf("got %q, want %q", got, pm)
			}
		})
	}
}

func TestDetectPackageManagerUnknownDeclarationFallsBack(t *testing.T) {
	root := t.TempDir()
	runtimeFile(t, root, "package.json", `{"packageManager":"other@1.0.0"}`)
	runtimeFile(t, root, "yarn.lock", "")
	if got := Detect(root).PackageManager; got != "yarn" {
		t.Fatalf("got %q", got)
	}
}
