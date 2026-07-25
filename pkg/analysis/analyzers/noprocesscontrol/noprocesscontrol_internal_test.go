package noprocesscontrol

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerTreatsTestsAsProcessBoundaries(t *testing.T) {
	t.Parallel()

	const source = `package sample
func fixture() { panic("expected test fixture") }
`
	if got := reportedCalls(t, "fixture_test.go", source); got != 0 {
		t.Fatalf("test diagnostics = %d, want 0", got)
	}
	if got := reportedCalls(t, "fixture.go", source); got != 1 {
		t.Fatalf("production diagnostics = %d, want 1", got)
	}
}

func reportedCalls(t *testing.T, filename, source string) int {
	t.Helper()

	files := token.NewFileSet()
	file, err := parser.ParseFile(files, filename, source, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	information := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	checked, err := (&types.Config{Importer: importer.Default()}).Check(
		"example.com/sample",
		files,
		[]*ast.File{file},
		information,
	)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	reported := 0
	_, err = Analyzer.Run(&analysis.Pass{
		Analyzer:  Analyzer,
		Fset:      files,
		Files:     []*ast.File{file},
		Pkg:       checked,
		TypesInfo: information,
		Report: func(analysis.Diagnostic) {
			reported++
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return reported
}
