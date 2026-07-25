package nostoredcontext

import (
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

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
