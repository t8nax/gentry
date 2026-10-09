package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// jsonReaders are the functions that read Env.json: Run sets it, the others
// print the output in its form.
var jsonReaders = []string{"Run", "emit", "warn", "fail"}

// TestOneOutput checks the sources of the package: JSON is printed only by
// emit and fail, and no command reads --json itself, so that no command
// forks into JSON and a text of its own data.
func TestOneOutput(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				// The form of the output is chosen by Run, and printed by emit,
				// warn and fail.
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "json" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "env" && !slices.Contains(jsonReaders, fn.Name.Name) {
						t.Errorf("%s: %s reads env.json", fset.Position(sel.Pos()), fn.Name.Name)
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					if fun.Name == "writeJSON" && fn.Name.Name != "emit" && fn.Name.Name != "fail" {
						t.Errorf("%s: %s prints JSON itself", fset.Position(call.Pos()), fn.Name.Name)
					}
				case *ast.SelectorExpr:
					if fun.Sel.Name == "Bool" && len(call.Args) == 1 {
						if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == `"json"` {
							t.Errorf("%s: %s reads --json", fset.Position(call.Pos()), fn.Name.Name)
						}
					}
				}
				return true
			})
		}
	}
}
