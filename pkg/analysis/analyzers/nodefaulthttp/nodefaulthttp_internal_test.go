package nodefaulthttp

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestHTTPTransportRequiresEveryTypeIdentityDimension(t *testing.T) {
	t.Parallel()

	httpPackage := types.NewPackage("net/http", "http")
	otherPackage := types.NewPackage("example.com/http", "http")
	tests := map[string]types.Type{
		"basic":         types.Typ[types.Int],
		"pointer basic": types.NewPointer(types.Typ[types.Int]),
		"package-less": types.NewPointer(types.NewNamed(
			types.NewTypeName(token.NoPos, nil, "Transport", nil),
			types.NewStruct(nil, nil),
			nil,
		)),
		"wrong package": transportType(otherPackage, "Transport"),
		"wrong name":    transportType(httpPackage, "Client"),
	}
	for name, candidate := range tests {
		if isHTTPTransport(candidate) {
			t.Fatalf("isHTTPTransport(%s) = true", name)
		}
	}
	if !isHTTPTransport(transportType(httpPackage, "Transport")) {
		t.Fatal("isHTTPTransport() rejected *net/http.Transport")
	}
}

func TestOwnedTransportCloneRequiresExactVariableIdentity(t *testing.T) {
	t.Parallel()

	httpPackage := types.NewPackage("net/http", "http")
	otherPackage := types.NewPackage("example.com/http", "http")
	transport := transportType(httpPackage, "Transport")
	objects := []types.Object{
		types.NewTypeName(token.NoPos, httpPackage, "DefaultTransport", nil),
		types.NewVar(token.NoPos, nil, "DefaultTransport", transport),
		types.NewVar(token.NoPos, otherPackage, "DefaultTransport", transport),
		types.NewVar(token.NoPos, httpPackage, "OtherTransport", transport),
		types.NewVar(token.NoPos, httpPackage, "DefaultTransport", transport),
	}
	uses := make(map[*ast.Ident]types.Object, len(objects))
	typesByExpression := make(map[ast.Expr]types.TypeAndValue, len(objects))
	expressions := make([]ast.Expr, 0, len(objects))
	var expected *ast.Ident
	for index, object := range objects {
		identifier := ast.NewIdent("transport")
		assertionType := ast.NewIdent("Transport")
		uses[identifier] = object
		typesByExpression[assertionType] = types.TypeAndValue{Type: transport}
		expressions = append(expressions, &ast.CallExpr{Fun: &ast.SelectorExpr{
			X:   &ast.TypeAssertExpr{X: identifier, Type: assertionType},
			Sel: ast.NewIdent("Clone"),
		}})
		if index == len(objects)-1 {
			expected = identifier
		}
	}
	file := &ast.File{Decls: []ast.Decl{&ast.FuncDecl{
		Name: ast.NewIdent("cloneTransports"),
		Type: &ast.FuncType{Params: &ast.FieldList{}},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent("_")},
			Tok: token.ASSIGN,
			Rhs: expressions,
		}}},
	}}}
	clones := ownedTransportClones(&analysis.Pass{TypesInfo: &types.Info{
		Uses:  uses,
		Types: typesByExpression,
	}}, file)
	if len(clones) != 1 {
		t.Fatalf("len(clones) = %d, want 1", len(clones))
	}
	if _, ok := clones[expected]; !ok {
		t.Fatal("exact net/http.DefaultTransport clone was not recognized")
	}
}

func transportType(packageValue *types.Package, name string) types.Type {
	return types.NewPointer(types.NewNamed(
		types.NewTypeName(token.NoPos, packageValue, name, nil),
		types.NewStruct(nil, nil),
		nil,
	))
}
