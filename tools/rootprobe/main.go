// Command rootprobe reports which install root the app would resolve.
// Diagnostic only — it must sit in the same folder as LaraEnv.exe, because
// the answer depends on that folder.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"lara4ever/internal/installer"
	"lara4ever/internal/paths"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("os.Executable() failed:", err)
		return
	}
	dir := filepath.Dir(exe)
	marker := filepath.Join(dir, ".laraenv-portable")
	_, markerErr := os.Stat(marker)

	fmt.Println("exe            :", exe)
	fmt.Println("exe dir        :", dir)
	fmt.Printf("LARAENV_ROOT   : %q\n", os.Getenv(paths.EnvRoot))
	fmt.Println("marker path    :", marker)
	fmt.Println("marker present :", markerErr == nil)
	fmt.Println()
	fmt.Println(">>> RESOLVED ROOT  :", paths.Root())
	fmt.Println(">>> config.json    :", paths.ConfigFile())
	if _, err := os.Stat(paths.ConfigFile()); err == nil {
		fmt.Println(">>> config.json exists: yes")
	} else {
		fmt.Println(">>> config.json exists: no")
	}
	fmt.Println(">>> www            :", paths.ProjectsRoot())
	fmt.Println(">>> nginx          :", paths.NginxRoot())

	fmt.Println()
	fmt.Println("--- installer catalog targets (installer.Catalog) ---")
	seen := map[string]bool{}
	for _, src := range installer.Catalog() {
		if seen[string(src.Kind)] {
			continue
		}
		seen[string(src.Kind)] = true
		fmt.Printf("  %-10s -> %s\n", src.Kind, src.InstallTo)
	}
}
