package nostoredcontext

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerContinuesAcrossImportsAndUntypedFields(t *testing.T) {
	t.Parallel()

	contextPackage := contextPackage(types.NewInterfaceType(nil, nil).Complete())
	contextObject := contextPackage.Scope().Lookup("Context")
	owner := types.NewPackage("example.com/owner", "owner")
	owner.SetImports([]*types.Package{
		types.NewPackage("fmt", "fmt"),
		contextPackage,
	})
	untyped := ast.NewIdent("Missing")
	typed := ast.NewIdent("Context")
	reported := 0
	_, err := Analyzer.Run(&analysis.Pass{
		Pkg: owner,
		Files: []*ast.File{{Decls: []ast.Decl{&ast.GenDecl{
			Specs: []ast.Spec{&ast.TypeSpec{
				Name: ast.NewIdent("Owner"),
				Type: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
					{Type: untyped},
					{Type: typed},
				}}},
			}},
		}}}},
		TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
			typed: {Type: contextObject.Type()},
		}},
		Report: func(analysis.Diagnostic) { reported++ },
	})
	if err != nil || reported != 1 {
		t.Fatalf("Run() reported = %d, error = %v", reported, err)
	}
}

func TestResolveContextRejectsInvalidContextPackage(t *testing.T) {
	t.Parallel()

	tests := map[string]*types.Package{
		"missing type": types.NewPackage("context", "context"),
		"wrong type":   contextPackage(types.Typ[types.Int]),
	}
	for name, imported := range tests {
		owner := types.NewPackage("example.com/owner", "owner")
		owner.SetImports([]*types.Package{imported})
		contextType, contextInterface := resolveContext(&analysis.Pass{Pkg: owner})
		if contextType != nil || contextInterface != nil {
			t.Fatalf("resolveContext(%s) = %v, %v", name, contextType, contextInterface)
		}
	}
}

func contextPackage(underlying types.Type) *types.Package {
	pkg := types.NewPackage("context", "context")
	name := types.NewTypeName(token.NoPos, pkg, "Context", nil)
	types.NewNamed(name, underlying, nil)
	pkg.Scope().Insert(name)

	return pkg
}
