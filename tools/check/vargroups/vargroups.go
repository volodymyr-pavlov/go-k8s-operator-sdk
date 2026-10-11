// Package vargroups finds package-level var declarations that group several variables in parentheses. Each variable
// stands on its own, with its own doc comment.
package vargroups

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports the groups.
var Analyzer = &analysis.Analyzer{
	Name: "vargroups",
	Doc:  "finds var groups that hold several variables",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.VAR || !group.Lparen.IsValid() || len(group.Specs) < 2 {
				continue
			}
			pass.Reportf(group.Pos(), "var group holds %d variables: declare each on its own", len(group.Specs))
		}
	}
	return nil, nil
}
