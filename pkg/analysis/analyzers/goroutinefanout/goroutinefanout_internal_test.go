package goroutinefanout

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestCountIterationsBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		start      int64
		limit      int64
		comparison token.Token
		post       token.Token
		want       int64
		known      bool
	}{
		{0, 2000, token.LSS, token.INC, maximumStaticFanout + 1, true},
		{math.MaxInt64, math.MaxInt64, token.LEQ, token.INC, 0, false},
		{math.MinInt64, math.MinInt64, token.GEQ, token.DEC, 0, false},
		{0, 1, token.LSS, token.ADD, 0, false},
	}
	for _, test := range tests {
		got, known := countIterations(test.start, test.limit, test.comparison, test.post)
		if got != test.want || known != test.known {
			t.Fatalf("countIterations() = %d, %t, want %d, %t", got, known, test.want, test.known)
		}
	}
	if compare(0, 0, token.ADD) {
		t.Fatal("compare() accepted an unsupported operation")
	}
	comparisons := []struct {
		left, right int64
		token       token.Token
	}{
		{0, 1, token.LSS},
		{1, 1, token.LEQ},
		{2, 1, token.GTR},
		{1, 1, token.GEQ},
	}
	for _, comparison := range comparisons {
		if !compare(comparison.left, comparison.right, comparison.token) {
			t.Fatalf("compare(%d, %d, %s) = false", comparison.left, comparison.right, comparison.token)
		}
	}
}

func TestWalkerRejectsNonLoopAndMalformedStarts(t *testing.T) {
	t.Parallel()

	if isChannel(nil) {
		t.Fatal("isChannel() accepted nil")
	}
	current := walker{}
	if _, known := current.staticIterations(&ast.EmptyStmt{}); known {
		t.Fatal("staticIterations() accepted a non-loop")
	}
	if _, _, ok := current.loopStart(nil); ok {
		t.Fatal("loopStart() accepted nil")
	}
	if _, _, ok := current.loopStart(&ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("one"), ast.NewIdent("two")},
		Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}},
	}); ok {
		t.Fatal("loopStart() accepted multiple targets")
	}
	right := &ast.BasicLit{Kind: token.INT, Value: "0"}
	current.pass = &analysis.Pass{TypesInfo: &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{
			right: {Type: types.Typ[types.UntypedInt], Value: constant.MakeInt64(0)},
		},
	}}
	if _, _, ok := current.loopStart(&ast.AssignStmt{
		Lhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}},
		Rhs: []ast.Expr{right},
	}); ok {
		t.Fatal("loopStart() accepted a non-identifier target")
	}
}

func TestIdentityHelpersRejectAmbiguousExpressions(t *testing.T) {
	t.Parallel()

	const source = `package identity
type inner struct { field chan struct{} }
type outer struct { nested inner }
func (inner) Method() {}
func use() {
    var value inner
    _ = ((value))
    _ = value.field
    _ = (&value).field
    _ = (*(&value)).field
    _ = value.Method
    var outerValue outer
    _ = outerValue.nested.field
    _ = !true
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "identity.go", source, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	information := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	checked, err := (&types.Config{}).Check("identity", fset, []*ast.File{file}, information)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	pass := &analysis.Pass{Pkg: checked, TypesInfo: information}
	ast.Inspect(file, func(node ast.Node) bool {
		if expression, ok := node.(ast.Expr); ok {
			_, _ = valueIdentity(pass, expression)
			_ = objectOf(pass, expression)
		}
		return true
	})
	missing := &ast.SelectorExpr{X: ast.NewIdent("missing"), Sel: ast.NewIdent("field")}
	if _, ok := valueIdentity(pass, missing); ok {
		t.Fatal("valueIdentity() accepted a selector without type selection")
	}
	if isBuiltin(pass, &ast.BasicLit{Kind: token.INT, Value: "1"}, "make") {
		t.Fatal("isBuiltin() accepted a basic literal")
	}
}

func TestWaitGroupMethodIdentity(t *testing.T) {
	t.Parallel()

	syncPackage := types.NewPackage("sync", "sync")
	otherPackage := types.NewPackage("other", "other")
	if isWaitGroupMethod(method(otherPackage, "WaitGroup", "Add"), "Add") {
		t.Fatal("isWaitGroupMethod() accepted another package")
	}
	if isWaitGroupMethod(method(syncPackage, "WaitGroup", "Done"), "Add") {
		t.Fatal("isWaitGroupMethod() accepted another method")
	}
	if isWaitGroupMethod(method(syncPackage, "Other", "Add"), "Add") {
		t.Fatal("isWaitGroupMethod() accepted another receiver")
	}
	if !isWaitGroupMethod(method(syncPackage, "WaitGroup", "Add"), "Add") {
		t.Fatal("isWaitGroupMethod() rejected sync.WaitGroup.Add")
	}
}

func method(pkg *types.Package, receiverName, methodName string) *types.Func {
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, receiverName, nil),
		types.NewStruct(nil, nil),
		nil,
	)
	receiver := types.NewVar(token.NoPos, pkg, "receiver", types.NewPointer(named))
	signature := types.NewSignatureType(receiver, nil, nil, nil, nil, false)
	return types.NewFunc(token.NoPos, pkg, methodName, signature)
}
