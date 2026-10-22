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

// Everything the user reads is English. A Portuguese string shipped in the
// MySQL first-run path and went unnoticed until someone spotted it in the UI,
// so this looks for the giveaway: Latin characters this codebase's English
// never needs.
//
// It scans string literals through the AST, so comments — which are a
// different question — are left alone.
func TestNoNonEnglishStringLiterals(t *testing.T) {
	const accents = "ãõçáâàéêíóôúÃÕÇÁÂÀÉÊÍÓÔÚñÑ"
	moduleRoot := filepath.Join("..", "..")
	var offenders []string

	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
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
			if strings.ContainsAny(lit.Value, accents) {
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
		t.Fatalf("non-English text in a string literal — everything the user reads is English:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
