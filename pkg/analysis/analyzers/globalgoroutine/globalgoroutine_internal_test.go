package globalgoroutine

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerContinuesAfterMalformedVariableSpecification(t *testing.T) {
	t.Parallel()

	reported := false
	_, err := Analyzer.Run(&analysis.Pass{
		Pkg: types.NewPackage("example.com/global", "global"),
		Files: []*ast.File{{Decls: []ast.Decl{&ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{
				&ast.ImportSpec{},
				&ast.ValueSpec{Values: []ast.Expr{&ast.CallExpr{
					Fun: &ast.FuncLit{Body: &ast.BlockStmt{
						List: []ast.Stmt{&ast.GoStmt{}},
					}},
				}}},
			},
		}}}},
		Report: func(analysis.Diagnostic) { reported = true },
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reported {
		t.Fatal("Run() stopped before the valid variable specification")
	}
}
