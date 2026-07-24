package nobackground

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerAllowsTestFileBoundary(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	fileToken := fset.AddFile("service_test.go", -1, 100)
	fileToken.SetLines([]int{0})
	selector := ast.NewIdent("Background")
	selector.NamePos = token.Pos(10)
	call := &ast.CallExpr{Fun: &ast.SelectorExpr{
		X: ast.NewIdent("context"), Sel: selector,
	}}
	file := &ast.File{Package: token.Pos(1), Name: ast.NewIdent("service"), Decls: []ast.Decl{
		&ast.FuncDecl{
			Name: ast.NewIdent("testContext"),
			Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call}}},
		},
	}}
	contextPackage := types.NewPackage("context", "context")
	background := types.NewFunc(
		token.NoPos,
		contextPackage,
		"Background",
		types.NewSignatureType(nil, nil, nil, nil, nil, false),
	)
	owner := types.NewPackage("example.com/service", "service")
	owner.SetImports([]*types.Package{contextPackage})
	reported := 0
	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{file},
		Pkg:   owner,
		TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
			selector: background,
		}},
		Report: func(analysis.Diagnostic) { reported++ },
	}
	if _, err := Analyzer.Run(pass); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if reported != 0 {
		t.Fatalf("reported = %d, want 0", reported)
	}
}
