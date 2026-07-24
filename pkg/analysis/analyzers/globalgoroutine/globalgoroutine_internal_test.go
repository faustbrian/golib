package globalgoroutine

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerIgnoresMalformedVariableSpecification(t *testing.T) {
	t.Parallel()

	_, err := Analyzer.Run(&analysis.Pass{
		Pkg: types.NewPackage("example.com/global", "global"),
		Files: []*ast.File{{Decls: []ast.Decl{&ast.GenDecl{
			Tok:   token.VAR,
			Specs: []ast.Spec{&ast.ImportSpec{}},
		}}}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
