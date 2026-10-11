// Package literals finds composite literals with several fields on one line. Each field of such a literal should have a
// line of its own. In code that is not a test, that holds for every literal, including lists of values. In a test it
// holds only for literals of keyed fields and for literals of literals, so that tables of test data stay compact.
package literals

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports the literals, and offers to expand them.
var Analyzer = &analysis.Analyzer{
	Name: "literals",
	Doc:  "finds composite literals with several fields on one line",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		test := strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go")
		ast.Inspect(file, func(n ast.Node) bool {
			literal, ok := n.(*ast.CompositeLit)
			if !ok || !crowded(pass.Fset, literal, test) {
				return true
			}
			pass.Report(analysis.Diagnostic{
				Pos:     literal.Pos(),
				Message: "composite literal has several fields on one line",
				SuggestedFixes: []analysis.SuggestedFix{{
					Message: "put each field on a line of its own",
					TextEdits: []analysis.TextEdit{{
						Pos:     literal.Lbrace,
						End:     literal.Rbrace + 1,
						NewText: expand(pass, literal),
					}},
				}},
			})
			// The fields of an expanded literal are checked on the next run, so that the edits never overlap.
			return false
		})
	}
	return nil, nil
}

// crowded reports whether two fields of literal start on the same line, and whether the rule covers it.
func crowded(fset *token.FileSet, literal *ast.CompositeLit, test bool) bool {
	if len(literal.Elts) < 2 {
		return false
	}
	if test && !keyedOrNested(literal) {
		return false
	}
	seen := map[int]bool{}
	for _, element := range literal.Elts {
		line := fset.Position(element.Pos()).Line
		if seen[line] {
			return true
		}
		seen[line] = true
	}
	return false
}

// keyedOrNested reports whether the elements of literal are keyed, or are all literals themselves.
func keyedOrNested(literal *ast.CompositeLit) bool {
	if _, keyed := literal.Elts[0].(*ast.KeyValueExpr); keyed {
		return true
	}
	for _, element := range literal.Elts {
		if _, nested := element.(*ast.CompositeLit); !nested {
			return false
		}
	}
	return true
}

// expand returns the braces of literal with each element on a line of its own, each ending in a comma.
func expand(pass *analysis.Pass, literal *ast.CompositeLit) []byte {
	var out strings.Builder
	out.WriteString("{\n")
	for _, element := range literal.Elts {
		out.Write(sourceOf(pass, element))
		out.WriteString(",\n")
	}
	out.WriteString("}")
	return []byte(out.String())
}

// sourceOf returns the source text of node.
func sourceOf(pass *analysis.Pass, node ast.Node) []byte {
	file := pass.Fset.File(node.Pos())
	content, err := pass.ReadFile(file.Name())
	if err != nil {
		return nil
	}
	return content[file.Offset(node.Pos()):file.Offset(node.End())]
}
