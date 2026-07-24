package nounsafe

import (
	"go/ast"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRunReportsCAndIgnoresMalformedImportLiteral(t *testing.T) {
	t.Parallel()

	file := &ast.File{Imports: []*ast.ImportSpec{
		{Path: &ast.BasicLit{Kind: token.STRING, Value: `"C"`}},
		{Path: &ast.BasicLit{Kind: token.STRING, Value: `"`}},
	}}
	var diagnostics []analysis.Diagnostic
	pass := &analysis.Pass{
		Files: []*ast.File{file},
		Report: func(diagnostic analysis.Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	}

	if _, err := run(pass); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
}
