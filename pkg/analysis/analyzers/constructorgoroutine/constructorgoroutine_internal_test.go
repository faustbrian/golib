package constructorgoroutine

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerContinuesAfterDeclarationsWithoutTypeObjects(t *testing.T) {
	t.Parallel()

	analyzer, err := New(Options{Policies: []Policy{{
		Package: "missingdefs",
		Symbols: []string{"New"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	missing := ast.NewIdent("Missing")
	constructor := ast.NewIdent("New")
	signature := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	pass := &analysis.Pass{
		Files: []*ast.File{{Decls: []ast.Decl{
			&ast.FuncDecl{
				Name: missing,
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: &ast.BlockStmt{},
			},
			&ast.FuncDecl{
				Name: constructor,
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: &ast.BlockStmt{List: []ast.Stmt{&ast.GoStmt{}}},
			},
		}}},
		Pkg: types.NewPackage("missingdefs", "missingdefs"),
		TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{
			constructor: types.NewFunc(token.NoPos, nil, "New", signature),
		}},
		Fset: token.NewFileSet(),
	}
	reported := false
	pass.Report = func(analysis.Diagnostic) {
		reported = true
	}
	if _, err := analyzer.Run(pass); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reported {
		t.Fatal("Run() stopped before the configured constructor")
	}
}
