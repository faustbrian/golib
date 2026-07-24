package constructorgoroutine

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerIgnoresDeclarationsWithoutTypeObjects(t *testing.T) {
	t.Parallel()

	analyzer, err := New(Options{Policies: []Policy{{
		Package: "missingdefs",
		Symbols: []string{"New"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	pass := &analysis.Pass{
		Files: []*ast.File{{Decls: []ast.Decl{&ast.FuncDecl{
			Name: ast.NewIdent("New"),
			Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: &ast.BlockStmt{},
		}}}},
		Pkg:       types.NewPackage("missingdefs", "missingdefs"),
		TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{}},
		Fset:      token.NewFileSet(),
		Report:    func(analysis.Diagnostic) {},
	}
	if _, err := analyzer.Run(pass); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
