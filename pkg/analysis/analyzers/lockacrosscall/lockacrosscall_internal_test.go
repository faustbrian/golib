package lockacrosscall

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/cfg"
)

func TestConfiguredCallsIgnoreScheduledAndNonFunctions(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "Value"}
	pass := &analysis.Pass{
		Pkg: types.NewPackage("consumer", "consumer"),
		TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
			identifier: types.NewTypeName(
				token.NoPos,
				nil,
				"Value",
				types.Typ[types.Int],
			),
		}},
	}
	call := &ast.CallExpr{Fun: identifier}
	for _, node := range []ast.Node{
		&ast.DeferStmt{Call: call},
		&ast.GoStmt{Call: call},
		&ast.ExprStmt{X: call},
		&ast.BasicLit{},
	} {
		if got := configuredCalls(pass, node, nil); len(got) != 0 {
			t.Fatalf("configuredCalls(%T) = %#v", node, got)
		}
	}
}

func TestLockOperationRejectsNonLockForms(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "call"}
	pass := &analysis.Pass{TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{}}}
	for _, node := range []ast.Node{
		&ast.BasicLit{},
		&ast.ExprStmt{X: &ast.BasicLit{}},
		&ast.ExprStmt{X: &ast.CallExpr{Fun: identifier}},
	} {
		if _, action, _ := lockOperation(pass, node); action != lockNone {
			t.Fatalf("lockOperation(%T) action = %d", node, action)
		}
	}
}

func TestSyncLockMethodRejectsUnrelatedFunctions(t *testing.T) {
	t.Parallel()

	signature := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	for _, function := range []*types.Func{
		types.NewFunc(token.NoPos, nil, "Lock", signature),
		types.NewFunc(
			token.NoPos,
			types.NewPackage("sync", "sync"),
			"Other",
			signature,
		),
		types.NewFunc(
			token.NoPos,
			types.NewPackage("sync", "sync"),
			"Lock",
			signature,
		),
	} {
		if syncLockMethod(function) {
			t.Fatalf("syncLockMethod(%s) = true", function.Name())
		}
	}
}

func TestLockIdentityRejectsUnknownExpressions(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "mutex"}
	pass := &analysis.Pass{TypesInfo: &types.Info{
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}}
	for _, expression := range []ast.Expr{
		identifier,
		&ast.SelectorExpr{X: identifier, Sel: &ast.Ident{Name: "field"}},
		&ast.CallExpr{Fun: identifier},
	} {
		if _, ok := lockIdentity(pass, expression); ok {
			t.Fatalf("lockIdentity(%T) accepted expression", expression)
		}
	}
}

func TestLockIdentityUnwrapsPointerAndParentheses(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "mutex"}
	variable := types.NewVar(token.NoPos, nil, "mutex", types.Typ[types.Int])
	pass := &analysis.Pass{TypesInfo: &types.Info{
		Uses: map[*ast.Ident]types.Object{identifier: variable},
		Defs: map[*ast.Ident]types.Object{},
	}}
	expression := &ast.ParenExpr{X: &ast.StarExpr{X: identifier}}
	key, ok := lockIdentity(pass, expression)
	if !ok || key.root != variable {
		t.Fatalf("lockIdentity() = %#v, %t", key, ok)
	}
}

func TestLockIdentityRejectsMethodSelections(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "selection.go", `package sample
type value struct{}
func (value) Method() {}
func use(item value) { _ = item.Method }
`, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	information := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	checked, err := (&types.Config{}).Check("sample", fset, []*ast.File{file}, information)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	pass := &analysis.Pass{Pkg: checked, TypesInfo: information}
	var method *ast.SelectorExpr
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Method" {
			method = selector
		}
		return true
	})
	if method == nil {
		t.Fatal("method selection not found")
	}
	if _, ok := lockIdentity(pass, method); ok {
		t.Fatal("lockIdentity() accepted method selection")
	}
}

func TestSetHelpersCoverEmptyJoinsAndDistinctKeys(t *testing.T) {
	t.Parallel()

	first := lockKey{root: types.NewVar(token.NoPos, nil, "first", types.Typ[types.Int])}
	second := lockKey{root: types.NewVar(token.NoPos, nil, "second", types.Typ[types.Int])}
	if got := intersectPredecessors(nil, []lockSet{}, lockSet{first: {}}); len(got) != 0 {
		t.Fatalf("intersectPredecessors() = %#v", got)
	}
	if maps.Equal(lockSet{first: {}}, lockSet{second: {}}) {
		t.Fatal("sameLocks() accepted distinct keys")
	}
	block := &cfg.Block{Index: 0}
	got := intersectPredecessors(
		[]*cfg.Block{block},
		[]lockSet{{first: {}}},
		lockSet{first: {}, second: {}},
	)
	if !maps.Equal(got, lockSet{first: {}}) {
		t.Fatalf("intersection = %#v", got)
	}
}

func TestCalledObjectAndFunctionSymbolBoundaries(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "Call"}
	object := types.NewVar(token.NoPos, nil, "Call", types.Typ[types.Int])
	pass := &analysis.Pass{TypesInfo: &types.Info{
		Uses: map[*ast.Ident]types.Object{identifier: object},
	}}
	if calledObject(pass, &ast.IndexExpr{X: identifier}) != object ||
		calledObject(pass, &ast.IndexListExpr{X: identifier}) != object ||
		calledObject(pass, &ast.BasicLit{}) != nil {
		t.Fatal("calledObject() boundary mismatch")
	}

	packageValue := types.NewPackage("ioapi", "ioapi")
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, packageValue, "Client", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	receiver := types.NewVar(token.NoPos, packageValue, "receiver", types.NewPointer(named))
	function := types.NewFunc(
		token.NoPos,
		packageValue,
		"Call",
		types.NewSignatureType(receiver, nil, nil, nil, nil, false),
	)
	if got := functionSymbol(function); got != "Client.Call" {
		t.Fatalf("functionSymbol() = %q", got)
	}
}

func TestFunctionSymbolPreservesUnnamedReceiver(t *testing.T) {
	t.Parallel()

	receiver := types.NewVar(
		token.NoPos,
		nil,
		"receiver",
		types.NewInterfaceType(nil, nil).Complete(),
	)
	function := types.NewFunc(
		token.NoPos,
		types.NewPackage("ioapi", "ioapi"),
		"Call",
		types.NewSignatureType(receiver, nil, nil, nil, nil, false),
	)
	if got := functionSymbol(function); got != "Call" {
		t.Fatalf("functionSymbol() = %q", got)
	}
}

func TestAnalyzeBodyRejectsExcessiveCFG(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("var mutex sync.Mutex\nmutex.Lock()\n")
	for range maximumCFGBlocks {
		source.WriteString("if true {}\n")
	}
	pass, body := checkedLockBody(t, source.String())
	if err := analyzeBody(pass, body, nil); err == nil ||
		err.Error() != "lock analysis exceeds 4096 CFG blocks" {
		t.Fatalf("analyzeBody() error = %v", err)
	}
}

func TestCFGBlockBudgetAcceptsExactLimit(t *testing.T) {
	t.Parallel()

	if err := validateCFGBlockCount(maximumCFGBlocks); err != nil {
		t.Fatalf("validateCFGBlockCount() error = %v", err)
	}
	if err := validateCFGBlockCount(maximumCFGBlocks + 1); err == nil {
		t.Fatal("validateCFGBlockCount() accepted an excessive graph")
	}
}

func TestAnalyzerPropagatesAnalysisLimit(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("var mutex sync.Mutex\nmutex.Lock()\n")
	for range maximumCFGBlocks {
		source.WriteString("if true {}\n")
	}
	pass, _ := checkedLockBody(t, source.String())
	pass.Files[0].Decls = append(pass.Files[0].Decls, &ast.FuncDecl{
		Name: ast.NewIdent("after"),
		Type: &ast.FuncType{Params: &ast.FieldList{}},
		Body: &ast.BlockStmt{},
	})
	analyzer, err := New(Options{Calls: []Call{{
		Package: "example.com/io", Symbol: "Call",
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = analyzer.Run(pass)
	if err == nil || err.Error() != "lock analysis exceeds 4096 CFG blocks" {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestAnalyzeBodyRejectsExcessiveLockIdentities(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for index := range maximumLockIdentities + 1 {
		_, _ = fmt.Fprintf(&source, "var mutex%d sync.Mutex\n", index)
		_, _ = fmt.Fprintf(&source, "mutex%d.Lock()\n", index)
	}
	pass, body := checkedLockBody(t, source.String())
	if err := analyzeBody(pass, body, nil); err == nil ||
		err.Error() != "lock analysis exceeds 256 lock identities" {
		t.Fatalf("analyzeBody() error = %v", err)
	}
}

func TestAnalyzeBodyAcceptsExactLockIdentityLimit(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for index := range maximumLockIdentities {
		_, _ = fmt.Fprintf(&source, "var mutex%d sync.Mutex\n", index)
		_, _ = fmt.Fprintf(&source, "mutex%d.Lock()\n", index)
	}
	pass, body := checkedLockBody(t, source.String())
	if err := analyzeBody(pass, body, nil); err != nil {
		t.Fatalf("analyzeBody() error = %v", err)
	}
}

func checkedLockBody(t *testing.T, bodySource string) (*analysis.Pass, *ast.BlockStmt) {
	t.Helper()

	files := token.NewFileSet()
	file, err := parser.ParseFile(
		files,
		"limits.go",
		"package sample\nimport \"sync\"\nfunc use() {\n"+bodySource+"}\n",
		parser.SkipObjectResolution,
	)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	information := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	checked, err := (&types.Config{Importer: importer.Default()}).Check(
		"sample",
		files,
		[]*ast.File{file},
		information,
	)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	function := file.Decls[1].(*ast.FuncDecl)
	return &analysis.Pass{
		Fset: files, Files: []*ast.File{file}, Pkg: checked, TypesInfo: information,
	}, function.Body
}
