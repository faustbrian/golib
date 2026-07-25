// Package cleanupownership detects discarded cleanup results from configured constructors.
package cleanupownership

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

const ruleID = "lifecycle/cleanup-ownership"

// Constructor identifies one exact callable and its cleanup result position.
type Constructor struct {
	Package         string
	Symbol          string
	CleanupResult   int
	AllowedPackages []string
}

// Options configures cleanup ownership contracts.
type Options struct {
	Constructors []Constructor
}

type symbolKey struct {
	packagePath string
	symbol      string
}

type compiledConstructor struct {
	cleanupResult int
	allowed       map[string]struct{}
}

// Rule is the stable metadata for configured cleanup ownership.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryLifecycle,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Discarding an explicit cleanup result loses resource lifetime ownership.",
	Remediation:       "Bind the cleanup result and call it, defer it, or transfer it to the resource owner.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"resource_constructors": {
			Type:        shared.ConfigurationArray,
			Description: "Exact callables and zero-based cleanup result positions.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates cleanup ownership policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	constructors := make(map[symbolKey]compiledConstructor, len(options.Constructors))
	for _, constructor := range options.Constructors {
		if !exactPackage(constructor.Package) || !validSymbol(constructor.Symbol) {
			return nil, errors.New("resource constructors require an exact package and Function or Type.Method symbol")
		}
		if constructor.CleanupResult < 0 {
			return nil, errors.New("cleanup result positions must be non-negative")
		}
		key := symbolKey{packagePath: constructor.Package, symbol: constructor.Symbol}
		if _, duplicate := constructors[key]; duplicate {
			return nil, errors.New("resource constructor policy contains a duplicate callable")
		}
		compiled := compiledConstructor{
			cleanupResult: constructor.CleanupResult,
			allowed:       make(map[string]struct{}, len(constructor.AllowedPackages)),
		}
		for _, packagePath := range constructor.AllowedPackages {
			if !exactPackage(packagePath) {
				return nil, errors.New("resource constructor exceptions require exact package paths")
			}
			compiled.allowed[packagePath] = struct{}{}
		}
		constructors[key] = compiled
	}

	return &analysis.Analyzer{
		Name: "cleanupownership",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if len(constructors) == 0 {
				return nil, nil
			}
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					switch node := node.(type) {
					case *ast.AssignStmt:
						reportAssignment(pass, node.Lhs, node.Rhs, constructors)
					case *ast.ValueSpec:
						names := make([]ast.Expr, len(node.Names))
						for index, name := range node.Names {
							names[index] = name
						}
						reportAssignment(pass, names, node.Values, constructors)
					case *ast.ExprStmt:
						reportIgnoredCall(pass, node.X, constructors)
					case *ast.GoStmt:
						reportIgnoredCall(pass, node.Call, constructors)
					case *ast.DeferStmt:
						reportIgnoredCall(pass, node.Call, constructors)
					}
					return true
				})
			}
			return nil, nil
		},
	}, nil
}

func reportAssignment(
	pass *analysis.Pass,
	left []ast.Expr,
	right []ast.Expr,
	constructors map[symbolKey]compiledConstructor,
) {
	if len(right) != 1 {
		return
	}
	call, ok := right[0].(*ast.CallExpr)
	if !ok {
		return
	}
	called, constructor, ok := configuredCall(pass, call, constructors)
	if !ok || constructor.cleanupResult >= len(left) {
		return
	}
	identifier, blank := left[constructor.cleanupResult].(*ast.Ident)
	if !blank || identifier.Name != "_" {
		return
	}
	report(pass, identifier.Pos(), called, constructor.cleanupResult)
}

func reportIgnoredCall(
	pass *analysis.Pass,
	expression ast.Expr,
	constructors map[symbolKey]compiledConstructor,
) {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return
	}
	called, constructor, ok := configuredCall(pass, call, constructors)
	if !ok {
		return
	}
	report(pass, call.Pos(), called, constructor.cleanupResult)
}

func configuredCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	constructors map[symbolKey]compiledConstructor,
) (*types.Func, compiledConstructor, bool) {
	called, ok := calledObject(pass, call.Fun).(*types.Func)
	if !ok || called.Pkg() == nil {
		return nil, compiledConstructor{}, false
	}
	constructor, configured := constructors[symbolKey{
		packagePath: called.Pkg().Path(),
		symbol:      functionSymbol(called),
	}]
	if !configured {
		return nil, compiledConstructor{}, false
	}
	if _, allowed := constructor.allowed[pass.Pkg.Path()]; allowed {
		return nil, compiledConstructor{}, false
	}
	return called, constructor, true
}

func report(pass *analysis.Pass, position token.Pos, called *types.Func, cleanupResult int) {
	pass.Reportf(
		position,
		"%s: cleanup result %d from %s.%s is discarded",
		ruleID,
		cleanupResult+1,
		called.Pkg().Path(),
		functionSymbol(called),
	)
}

func exactPackage(packagePath string) bool {
	return packagePath != "" && path.Clean(packagePath) == packagePath &&
		!strings.Contains(packagePath, "*")
}

func validSymbol(symbol string) bool {
	parts := strings.Split(symbol, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !token.IsIdentifier(part) {
			return false
		}
	}
	return true
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
	if signature.Recv() == nil {
		return symbol
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := receiver.(*types.Named); ok {
		return named.Obj().Name() + "." + symbol
	}
	return symbol
}
