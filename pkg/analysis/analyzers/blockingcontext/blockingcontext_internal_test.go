package blockingcontext

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerToleratesMissingDefinition(t *testing.T) {
	t.Parallel()

	analyzer, err := New(Options{Policies: []Policy{{
		Package: "example.com/p", Functions: []string{"Fetch"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = analyzer.Run(&analysis.Pass{
		Pkg: types.NewPackage("example.com/p", "p"),
		Files: []*ast.File{{Decls: []ast.Decl{&ast.FuncDecl{
			Name: ast.NewIdent("Fetch"),
		}}}},
		TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestContextContractRejectsInvalidPackages(t *testing.T) {
	t.Parallel()

	other := types.NewPackage("fmt", "fmt")
	tests := []*types.Package{
		other,
		contextPackage(nil),
		contextPackage(types.Typ[types.Int]),
	}
	for _, imported := range tests {
		owner := types.NewPackage("example.com/p", "p")
		owner.SetImports([]*types.Package{imported})
		contextType, contract := contextContract(&analysis.Pass{Pkg: owner})
		if contextType != nil || contract != nil {
			t.Fatalf("contextContract() = %v, %v", contextType, contract)
		}
	}
	owner := types.NewPackage("example.com/p", "p")
	if contextType, contract := contextContract(&analysis.Pass{Pkg: owner}); contextType != nil || contract != nil {
		t.Fatalf("contextContract(empty) = %v, %v", contextType, contract)
	}
}

func TestFunctionSymbolFallsBackForUnnamedReceiver(t *testing.T) {
	t.Parallel()

	receiver := types.NewVar(token.NoPos, nil, "receiver", types.Typ[types.Int])
	signature := types.NewSignatureType(receiver, nil, nil, nil, nil, false)
	function := types.NewFunc(token.NoPos, nil, "Load", signature)
	if got := functionSymbol(function, signature); got != "Load" {
		t.Fatalf("functionSymbol() = %q", got)
	}
	if hasContext(signature, nil, nil) {
		t.Fatal("hasContext() accepted a missing context contract")
	}
}

func contextPackage(underlying types.Type) *types.Package {
	pkg := types.NewPackage("context", "context")
	if underlying == nil {
		return pkg
	}
	name := types.NewTypeName(token.NoPos, pkg, "Context", nil)
	types.NewNamed(name, underlying, nil)
	pkg.Scope().Insert(name)

	return pkg
}
