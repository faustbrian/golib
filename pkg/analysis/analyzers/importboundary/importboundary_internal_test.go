package importboundary

import (
	"go/ast"
	"go/token"
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

func TestAnalyzerContinuesAfterMalformedImportLiteral(t *testing.T) {
	t.Parallel()

	analyzer, err := New(Options{Policies: []Policy{{
		Package:     "example.com/service",
		DenyImports: []string{"example.com/infra"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reports := 0
	_, err = analyzer.Run(&analysis.Pass{
		Pkg: types.NewPackage("example.com/service", "service"),
		Files: []*ast.File{{Imports: []*ast.ImportSpec{
			{Path: &ast.BasicLit{Kind: token.STRING, Value: "not-quoted"}},
			{Path: &ast.BasicLit{Kind: token.STRING, Value: `"example.com/infra"`}},
		}}},
		Report: func(analysis.Diagnostic) { reports++ },
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if reports != 1 {
		t.Fatalf("Run() reports = %d, want 1", reports)
	}
}

func TestReportExplicitExaminesEveryPolicy(t *testing.T) {
	t.Parallel()

	reports := 0
	pass := &analysis.Pass{
		Pkg:    types.NewPackage("example.com/service", "service"),
		Report: func(analysis.Diagnostic) { reports++ },
	}
	policies := []compiledPolicy{
		{packagePattern: packagePattern{prefix: "example.com/other"}, deny: []packagePattern{{prefix: "example.com/infra"}}},
		{packagePattern: packagePattern{prefix: "example.com/service"}, deny: []packagePattern{{prefix: "example.com/infra"}}},
	}
	if !reportExplicit(pass, token.NoPos, "example.com/infra", policies) {
		t.Fatal("reportExplicit() = false")
	}
	if reports != 1 {
		t.Fatalf("reportExplicit() reports = %d, want 1", reports)
	}
}

func TestReportRestrictedExaminesEveryRestriction(t *testing.T) {
	t.Parallel()

	reports := 0
	pass := &analysis.Pass{
		Pkg:    types.NewPackage("example.com/consumer", "consumer"),
		Report: func(analysis.Diagnostic) { reports++ },
	}
	restrictions := []compiledRestriction{
		{packagePattern: packagePattern{prefix: "example.com/other", tree: true}},
		{packagePattern: packagePattern{prefix: "example.com/backend", tree: true}, allowed: []packagePattern{{prefix: "example.com/adapter", tree: true}}},
	}
	if !reportRestricted(pass, token.NoPos, "example.com/backend/client", restrictions) {
		t.Fatal("reportRestricted() = false")
	}
	if reports != 1 {
		t.Fatalf("reportRestricted() reports = %d, want 1", reports)
	}
}
