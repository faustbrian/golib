package importboundary

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerIgnoresMalformedImportLiteral(t *testing.T) {
	t.Parallel()

	analyzer, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = analyzer.Run(&analysis.Pass{
		Pkg: types.NewPackage("example.com/service", "service"),
		Files: []*ast.File{{Imports: []*ast.ImportSpec{{
			Path: &ast.BasicLit{Value: "not-quoted"},
		}}}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
