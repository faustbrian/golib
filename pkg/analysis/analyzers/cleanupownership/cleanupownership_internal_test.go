package cleanupownership

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestHelpersRejectNonCallableExpressions(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "Value"}
	pass := &analysis.Pass{
		Pkg:       types.NewPackage("consumer", "consumer"),
		TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{}},
		Report: func(analysis.Diagnostic) {
			t.Fatal("unexpected diagnostic")
		},
	}
	pass.TypesInfo.Uses[identifier] = types.NewTypeName(
		token.NoPos,
		pass.Pkg,
		"Value",
		types.Typ[types.Int],
	)
	constructors := map[symbolKey]compiledConstructor{}
	reportIgnoredCall(pass, &ast.BasicLit{}, constructors)
	if _, _, ok := configuredCall(pass, &ast.CallExpr{Fun: identifier}, constructors); ok {
		t.Fatal("configuredCall() accepted a type conversion")
	}
	if object := calledObject(pass, &ast.BasicLit{}); object != nil {
		t.Fatalf("calledObject() = %#v, want nil", object)
	}
}

func TestConfiguredCallRejectsPackageLessFunction(t *testing.T) {
	t.Parallel()

	identifier := &ast.Ident{Name: "open"}
	pass := &analysis.Pass{
		Pkg: types.NewPackage("consumer", "consumer"),
		TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
			identifier: types.NewFunc(
				token.NoPos,
				nil,
				"open",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			),
		}},
	}
	if _, _, ok := configuredCall(
		pass,
		&ast.CallExpr{Fun: identifier},
		map[symbolKey]compiledConstructor{},
	); ok {
		t.Fatal("configuredCall() accepted a package-less function")
	}
}

func TestNewAcceptsFirstResultAsCleanup(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Constructors: []Constructor{{
		Package: "resourceapi", Symbol: "Open", CleanupResult: 0,
	}}}); err != nil {
		t.Fatalf("New() error = %v", err)
	}
}

func TestReportAssignmentRejectsCleanupIndexAtAssignmentWidth(t *testing.T) {
	t.Parallel()

	packageValue := types.NewPackage("resourceapi", "resourceapi")
	identifier := &ast.Ident{Name: "Open"}
	function := types.NewFunc(
		token.NoPos,
		packageValue,
		"Open",
		types.NewSignatureType(nil, nil, nil, nil, nil, false),
	)
	pass := &analysis.Pass{
		Pkg: types.NewPackage("consumer", "consumer"),
		TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
			identifier: function,
		}},
		Report: func(analysis.Diagnostic) {
			t.Fatal("unexpected diagnostic")
		},
	}
	reportAssignment(
		pass,
		nil,
		[]ast.Expr{&ast.CallExpr{Fun: identifier}},
		map[symbolKey]compiledConstructor{
			{packagePath: "resourceapi", symbol: "Open"}: {cleanupResult: 0},
		},
	)
}

func TestFunctionSymbolPreservesUnnamedReceiverMethod(t *testing.T) {
	t.Parallel()

	receiverType := types.NewInterfaceType(nil, nil).Complete()
	receiver := types.NewVar(token.NoPos, nil, "receiver", receiverType)
	function := types.NewFunc(
		token.NoPos,
		types.NewPackage("resourceapi", "resourceapi"),
		"Close",
		types.NewSignatureType(receiver, nil, nil, nil, nil, false),
	)
	if got := functionSymbol(function); got != "Close" {
		t.Fatalf("functionSymbol() = %q, want Close", got)
	}
}

func TestFunctionSymbolQualifiesPointerReceiverMethod(t *testing.T) {
	t.Parallel()

	packageValue := types.NewPackage("resourceapi", "resourceapi")
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, packageValue, "Manager", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	receiver := types.NewVar(token.NoPos, packageValue, "receiver", types.NewPointer(named))
	function := types.NewFunc(
		token.NoPos,
		packageValue,
		"Open",
		types.NewSignatureType(receiver, nil, nil, nil, nil, false),
	)
	if got := functionSymbol(function); got != "Manager.Open" {
		t.Fatalf("functionSymbol() = %q, want Manager.Open", got)
	}
}
