// Package transactionrollback enforces immediate rollback ownership for
// configured transaction constructors.
package transactionrollback

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "lifecycle/transaction-rollback"

// Transaction identifies one transaction constructor and its rollback
// contract.
type Transaction struct {
	Package        string
	Symbol         string
	Result         int
	RollbackMethod string
}

// Options configures transaction rollback ownership contracts.
type Options struct {
	Transactions []Transaction
}

type symbolKey struct {
	packagePath string
	symbol      string
}

// Rule is the stable metadata for transaction rollback ownership.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryLifecycle,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "A transaction without immediate rollback ownership can remain open on early returns and failed commits.",
	Remediation:       "After the constructor error guard, immediately defer Rollback on the transaction before performing work.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"transactions": {
			Type:        shared.ConfigurationArray,
			Description: "Exact constructors, transaction result positions, and rollback methods.",
		},
	}},
}

// Analyzer is inactive until transaction contracts are configured.
var Analyzer, _ = New(Options{})

// New validates transaction contracts and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	transactions := make(map[symbolKey]Transaction, len(options.Transactions))
	for _, transaction := range options.Transactions {
		if !exactPackage(transaction.Package) || !validSymbol(transaction.Symbol) {
			return nil, errors.New(
				"transaction constructors require an exact package and Function or Type.Method symbol",
			)
		}
		if transaction.Result < 0 {
			return nil, errors.New("transaction result position must be non-negative")
		}
		if !token.IsIdentifier(transaction.RollbackMethod) ||
			transaction.RollbackMethod == "_" {
			return nil, errors.New("transaction rollback method must be an identifier")
		}
		key := symbolKey{transaction.Package, transaction.Symbol}
		if _, duplicate := transactions[key]; duplicate {
			return nil, errors.New("transaction policy contains a duplicate constructor")
		}
		transactions[key] = transaction
	}

	return &analysis.Analyzer{
		Name: "transactionrollback",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if len(transactions) == 0 {
				return nil, nil
			}
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					if block, ok := node.(*ast.BlockStmt); ok {
						analyzeBlock(pass, block, transactions)
					}
					return true
				})
			}
			return nil, nil
		},
	}, nil
}

func analyzeBlock(
	pass *analysis.Pass,
	block *ast.BlockStmt,
	transactions map[symbolKey]Transaction,
) {
	for index, statement := range block.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 {
			continue
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		function, ok := calledObject(pass, call.Fun).(*types.Func)
		if !ok || function.Pkg() == nil {
			continue
		}
		key := symbolKey{function.Pkg().Path(), functionSymbol(function)}
		transaction, configured := transactions[key]
		if !configured || transaction.Result >= len(assignment.Lhs) {
			continue
		}
		identifier, ok := assignment.Lhs[transaction.Result].(*ast.Ident)
		if !ok {
			continue
		}
		if identifier.Name == "_" {
			report(pass, call, key, transaction)
			continue
		}
		transactionObject := assignedObject(pass, identifier)
		next := index + 1
		if next < len(block.List) &&
			isRollbackDefer(pass, block.List[next], transactionObject, transaction) {
			continue
		}
		errorObject := assignedError(pass, assignment, transaction.Result)
		if errorObject != nil && next < len(block.List) &&
			isTerminatingErrorGuard(pass, block.List[next], errorObject) {
			next++
		}
		if next < len(block.List) &&
			(isRollbackDefer(pass, block.List[next], transactionObject, transaction) ||
				isOwnershipReturn(pass, block.List[next], transactionObject)) {
			continue
		}
		report(pass, call, key, transaction)
	}
}

func report(
	pass *analysis.Pass,
	call *ast.CallExpr,
	key symbolKey,
	transaction Transaction,
) {
	pass.Reportf(
		call.Pos(),
		"%s: transaction from %s.%s must immediately establish deferred %s ownership",
		ruleID,
		key.packagePath,
		key.symbol,
		transaction.RollbackMethod,
	)
}

func assignedObject(pass *analysis.Pass, identifier *ast.Ident) types.Object {
	if object := pass.TypesInfo.Defs[identifier]; object != nil {
		return object
	}
	return pass.TypesInfo.Uses[identifier]
}

func assignedError(
	pass *analysis.Pass,
	assignment *ast.AssignStmt,
	transactionResult int,
) types.Object {
	errorType := types.Universe.Lookup("error").Type()
	for index, expression := range assignment.Lhs {
		if index == transactionResult {
			continue
		}
		identifier, ok := expression.(*ast.Ident)
		if !ok || identifier.Name == "_" {
			continue
		}
		object := assignedObject(pass, identifier)
		if object != nil && types.AssignableTo(object.Type(), errorType) {
			return object
		}
	}
	return nil
}

func isRollbackDefer(
	pass *analysis.Pass,
	statement ast.Stmt,
	transactionObject types.Object,
	transaction Transaction,
) bool {
	deferred, ok := statement.(*ast.DeferStmt)
	if !ok {
		return false
	}
	selector, ok := deferred.Call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != transaction.RollbackMethod {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	return ok && pass.TypesInfo.Uses[receiver] == transactionObject
}

func isTerminatingErrorGuard(
	pass *analysis.Pass,
	statement ast.Stmt,
	errorObject types.Object,
) bool {
	guard, ok := statement.(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil ||
		!isNonNilCondition(pass, guard.Cond, errorObject) || len(guard.Body.List) == 0 {
		return false
	}
	switch final := guard.Body.List[len(guard.Body.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return final.Tok == token.BREAK || final.Tok == token.CONTINUE ||
			final.Tok == token.GOTO
	default:
		return false
	}
}

func isNonNilCondition(
	pass *analysis.Pass,
	expression ast.Expr,
	errorObject types.Object,
) bool {
	binary, ok := unparen(expression).(*ast.BinaryExpr)
	if !ok || binary.Op != token.NEQ {
		return false
	}
	return isObjectAndNil(pass, binary.X, binary.Y, errorObject) ||
		isObjectAndNil(pass, binary.Y, binary.X, errorObject)
}

func isObjectAndNil(
	pass *analysis.Pass,
	objectExpression ast.Expr,
	nilExpression ast.Expr,
	object types.Object,
) bool {
	identifier, ok := unparen(objectExpression).(*ast.Ident)
	if !ok || pass.TypesInfo.Uses[identifier] != object {
		return false
	}
	nilIdentifier, ok := unparen(nilExpression).(*ast.Ident)
	return ok && nilIdentifier.Name == "nil" &&
		pass.TypesInfo.Uses[nilIdentifier] == types.Universe.Lookup("nil")
}

func isOwnershipReturn(
	pass *analysis.Pass,
	statement ast.Stmt,
	transactionObject types.Object,
) bool {
	returned, ok := statement.(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, result := range returned.Results {
		identifier, ok := unparen(result).(*ast.Ident)
		if ok && pass.TypesInfo.Uses[identifier] == transactionObject {
			return true
		}
	}
	return false
}

func unparen(expression ast.Expr) ast.Expr {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}
		expression = parenthesized.X
	}
}

func calledObject(pass *analysis.Pass, expression ast.Expr) types.Object {
	switch expression := expression.(type) {
	case *ast.IndexExpr:
		return calledObject(pass, expression.X)
	case *ast.IndexListExpr:
		return calledObject(pass, expression.X)
	case *ast.Ident:
		return pass.TypesInfo.Uses[expression]
	case *ast.SelectorExpr:
		return pass.TypesInfo.Uses[expression.Sel]
	default:
		return nil
	}
}

func functionSymbol(function *types.Func) string {
	symbol := function.Name()
	signature := function.Type().(*types.Signature)
	if signature.Recv() != nil {
		receiver := signature.Recv().Type()
		if pointer, ok := receiver.(*types.Pointer); ok {
			receiver = pointer.Elem()
		}
		if named, ok := receiver.(*types.Named); ok {
			symbol = named.Obj().Name() + "." + symbol
		}
	}
	return symbol
}

func exactPackage(packagePath string) bool {
	return packagePath != "" && packagePath != "." &&
		!strings.HasPrefix(packagePath, "/") && path.Clean(packagePath) == packagePath &&
		!strings.Contains(packagePath, "*") && !strings.Contains(packagePath, "...")
}

func validSymbol(symbol string) bool {
	parts := strings.Split(symbol, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !token.IsIdentifier(part) || part == "_" {
			return false
		}
	}
	return true
}
