package httpclienttimeout

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestHTTPClientRequiresExactPackageAndName(t *testing.T) {
	t.Parallel()

	httpPackage := types.NewPackage("net/http", "http")
	otherPackage := types.NewPackage("example.com/http", "http")
	tests := map[string]types.Type{
		"wrong package": namedType(otherPackage, "Client"),
		"wrong name":    namedType(httpPackage, "Transport"),
	}
	for name, candidate := range tests {
		if isHTTPClient(candidate) {
			t.Fatalf("isHTTPClient(%s) = true", name)
		}
	}
	if !isHTTPClient(namedType(httpPackage, "Client")) {
		t.Fatal("isHTTPClient() rejected net/http.Client")
	}
}

func TestReportZeroValueIgnoresExplicitInitializers(t *testing.T) {
	t.Parallel()

	reportZeroValue(&analysis.Pass{}, &ast.ValueSpec{
		Type:   ast.NewIdent("Client"),
		Values: []ast.Expr{&ast.CompositeLit{}},
	})
}

func namedType(packageValue *types.Package, name string) types.Type {
	return types.NewNamed(
		types.NewTypeName(token.NoPos, packageValue, name, nil),
		types.NewStruct(nil, nil),
		nil,
	)
}
