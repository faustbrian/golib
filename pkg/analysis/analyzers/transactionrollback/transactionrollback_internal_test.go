package transactionrollback

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestExactPackageRejectsEveryMalformedDimension(t *testing.T) {
	t.Parallel()

	if !exactPackage("example.com/transaction") {
		t.Fatal("exactPackage() rejected a clean import path")
	}
	for _, packagePath := range []string{
		"", ".", "/example.com/transaction", "example.com/../transaction",
		"example.com/transaction/", "example.com/*", "example.com/.../transaction",
	} {
		if exactPackage(packagePath) {
			t.Errorf("exactPackage(%q) = true", packagePath)
		}
	}
}

func TestAnalyzeStatementIgnoresPackageLessFunction(t *testing.T) {
	t.Parallel()

	callee := ast.NewIdent("Begin")
	function := types.NewFunc(
		token.NoPos,
		nil,
		callee.Name,
		types.NewSignatureType(nil, nil, nil, nil, nil, false),
	)
	assignment := &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("transaction")},
		Rhs: []ast.Expr{&ast.CallExpr{Fun: callee}},
	}
	pass := &analysis.Pass{TypesInfo: &types.Info{
		Uses: map[*ast.Ident]types.Object{callee: function},
	}}
	analyzeStatement(pass, &ast.BlockStmt{List: []ast.Stmt{assignment}}, 0, assignment, nil)
}

func TestRollbackDeferRejectsWrongMethodAndNonIdentifierReceiver(t *testing.T) {
	t.Parallel()

	transaction := Transaction{RollbackMethod: "Rollback"}
	for _, deferred := range []*ast.DeferStmt{
		{Call: &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("tx"), Sel: ast.NewIdent("Commit")}}},
		{Call: &ast.CallExpr{Fun: &ast.SelectorExpr{X: &ast.CallExpr{}, Sel: ast.NewIdent("Rollback")}}},
	} {
		if isRollbackDefer(&analysis.Pass{}, deferred, nil, transaction) {
			t.Fatalf("isRollbackDefer(%#v) = true", deferred)
		}
	}
}

func TestTerminatingErrorGuardRejectsEveryStructuralNearMiss(t *testing.T) {
	t.Parallel()

	errorObject := types.NewVar(token.NoPos, nil, "err", types.Universe.Lookup("error").Type())
	errorName := ast.NewIdent("err")
	nilName := ast.NewIdent("nil")
	condition := &ast.BinaryExpr{X: errorName, Op: token.NEQ, Y: nilName}
	pass := &analysis.Pass{TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
		errorName: errorObject,
		nilName:   types.Universe.Lookup("nil"),
	}}}
	tests := []ast.Stmt{
		&ast.EmptyStmt{},
		&ast.IfStmt{Init: &ast.EmptyStmt{}, Body: &ast.BlockStmt{}},
		&ast.IfStmt{Else: &ast.EmptyStmt{}, Body: &ast.BlockStmt{}},
		&ast.IfStmt{Cond: condition, Body: &ast.BlockStmt{}},
		&ast.IfStmt{Cond: condition, Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.BranchStmt{Tok: token.FALLTHROUGH},
		}}},
	}
	for _, statement := range tests {
		if isTerminatingErrorGuard(pass, statement, errorObject) {
			t.Fatalf("isTerminatingErrorGuard(%T) = true", statement)
		}
	}
}

func TestNonNilConditionRejectsNonBinaryExpression(t *testing.T) {
	t.Parallel()

	if isNonNilCondition(&analysis.Pass{}, &ast.BasicLit{}, nil) {
		t.Fatal("isNonNilCondition() accepted a non-binary expression")
	}
}

func TestObjectAndNilRejectsMalformedOperands(t *testing.T) {
	t.Parallel()

	object := types.NewVar(token.NoPos, nil, "err", types.Universe.Lookup("error").Type())
	objectName := ast.NewIdent("err")
	notNil := ast.NewIdent("none")
	pass := &analysis.Pass{TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
		objectName: object,
		notNil:     types.Universe.Lookup("nil"),
	}}}
	for _, operands := range [][2]ast.Expr{
		{&ast.BasicLit{}, ast.NewIdent("nil")},
		{objectName, &ast.BasicLit{}},
		{objectName, notNil},
	} {
		if isObjectAndNil(pass, operands[0], operands[1], object) {
			t.Fatalf("isObjectAndNil(%T, %T) = true", operands[0], operands[1])
		}
	}
}
