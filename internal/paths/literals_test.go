package paths

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The install root is resolved at runtime and lives in exactly one place.
// A string literal spelling it out anywhere else is a bypass: it silently
// pins that code to C:\laraenv and breaks the portable build.
//
// Comments are deliberately allowed — they are documentation, and this scan
// walks the AST rather than the raw text so it only sees real literals.
func TestNoHardcodedInstallRootOutsidePathsPackage(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	var offenders []string

	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) || sameDir(path, filepath.Join(moduleRoot, "internal", "paths")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Separators are normalised first: the root is spelled with
			// forward slashes wherever it lands in a config file that
			// dislikes backslashes, and matching only one form left a hole
			// exactly the size of the bug this test exists to catch.
			normalised := strings.ReplaceAll(strings.ToLower(lit.Value), "/", `\`)
			if strings.Contains(normalised, `c:\laraenv`) {
				offenders = append(offenders, fset.Position(lit.Pos()).String()+" "+lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(offenders) > 0 {
		t.Fatalf("hardcoded install root outside the paths package — use paths.Root() and friends:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func skipDir(name string) bool {
	// .claude and .superpowers hold tooling scratch state, including nested git worktrees
	// this project's own tooling creates (git worktree list can show one
	// locked under .claude/worktrees/<name> for another in-flight session).
	// Such a worktree is a full copy of the repo, so without this skip the
	// walk would descend into its internal/paths/paths.go and judge that
	// worktree's own legitimate C:\laraenv constant as if it lived outside
	// this package.
	return name == ".git" || name == "node_modules" || name == "frontend" || name == "build" || name == ".claude" || name == ".superpowers"
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}
