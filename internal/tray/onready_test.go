package tray

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// internal/tray cannot be unit-tested against a real Win32 systray, so this
// is a source-text guard instead of a behavioural one — the same trade-off
// internal/paths/language_test.go makes, and for the same reason: no
// automated coverage beats a structural check that at least proves the shape
// is right.
//
// The failure this guards against has already happened twice: onReady builds
// the tray menu synchronously from hardcoded English titles, and the config
// watcher that would normally retitle it only fires on a *change* to the
// language setting — never once at startup. A user who already has a
// language saved from a previous session gets an English tray on every
// launch, and every automated suite (including this package's other tests,
// if it had any that could run headless) stayed green both times, because
// nothing exercises the real systray at startup.
//
// The fix is a single call to Relabel() at the end of onReady (see tray.go),
// applying whatever language is already stored before the tray is ever shown.
// This test does not run onReady — it can't, there is no systray in CI — it
// only proves the call is still there. If you are looking at a failure here,
// you (or a refactor) removed that call; put it back, at the end of onReady,
// after the menu items it retitles have been created.
func TestOnReadyCallsRelabel(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "tray.go", nil, 0)
	if err != nil {
		t.Fatalf("parse tray.go: %v", err)
	}

	var onReady *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "onReady" || fn.Body == nil {
			continue
		}
		onReady = fn
		break
	}
	if onReady == nil {
		t.Fatal("onReady method not found in tray.go")
	}

	callsRelabel := false
	ast.Inspect(onReady.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name == "Relabel" {
				callsRelabel = true
			}
		case *ast.Ident:
			if fn.Name == "Relabel" {
				callsRelabel = true
			}
		}
		return true
	})

	if !callsRelabel {
		t.Fatal("onReady does not call Relabel: a returning user's saved language " +
			"will not be applied until the language setting changes again, so the " +
			"tray shows hardcoded English on every launch")
	}
}
