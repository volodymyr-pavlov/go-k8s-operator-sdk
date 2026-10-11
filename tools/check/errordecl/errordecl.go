// Package errordecl finds errors that are declared outside errors.go. Every package declares its errors in errors.go,
// each on its own with var, and nowhere else: a call of errors.New, and a package-level variable named like an error,
// belong there. Tests may declare their own.
package errordecl

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports the declarations.
var Analyzer = &analysis.Analyzer{
	Name: "errordecl",
	Doc:  "finds errors that are declared outside errors.go",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		name := filepath.Base(pass.Fset.Position(file.Pos()).Filename)
		if name == "errors.go" || strings.HasSuffix(name, "_test.go") || ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if isErrorsNew(n) {
					pass.Reportf(n.Pos(), "errors.New belongs in errors.go")
				}
			case *ast.GenDecl:
				if n.Tok == token.VAR {
					reportNamedErrors(pass, file, n)
				}
			}
			return true
		})
	}
	return nil, nil
}

// isErrorsNew reports whether call is errors.New(...).
func isErrorsNew(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "New" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "errors"
}

// reportNamedErrors reports the package-level variables of declaration whose name starts with err or Err and goes on
// with a capital letter, as the names of errors do.
func reportNamedErrors(pass *analysis.Pass, file *ast.File, declaration *ast.GenDecl) {
	for _, declared := range file.Decls {
		if declared != declaration {
			continue
		}
		for _, spec := range declaration.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if isErrorName(name.Name) {
					pass.Reportf(name.Pos(), "the error %s belongs in errors.go", name.Name)
				}
			}
		}
	}
}

// isErrorName reports whether name looks like the name of an error: err or Err, then a capital letter.
func isErrorName(name string) bool {
	rest, found := strings.CutPrefix(name, "err")
	if !found {
		rest, found = strings.CutPrefix(name, "Err")
	}
	return found && len(rest) > 0 && rest[0] >= 'A' && rest[0] <= 'Z'
}
