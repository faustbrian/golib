// Package constructorgoroutine forbids hidden goroutine startup in constructors.
package constructorgoroutine

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

const ruleID = "lifecycle/no-constructor-goroutine"

// Policy names exact constructor functions and methods in one package.
type Policy struct {
	Package string
	Symbols []string
}

// Options configures constructor lifecycle policy.
type Options struct {
	Policies []Policy
}

// Rule is the stable metadata for constructor goroutine ownership.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryLifecycle,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Constructors that start goroutines hide lifetime, shutdown, and error ownership from callers.",
	Remediation:       "Start work explicitly after construction and expose cancellation, waiting, and error contracts.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"policies": {
			Type:        shared.ConfigurationArray,
			Description: "Exact packages and constructor function or method symbols.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates constructor policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	configured := make(map[string]map[string]struct{}, len(options.Policies))
	for _, policy := range options.Policies {
		if policy.Package == "" || path.Clean(policy.Package) != policy.Package ||
			strings.Contains(policy.Package, "*") {
			return nil, errors.New("constructor policy requires an exact clean package path")
		}
		if len(policy.Symbols) == 0 {
			return nil, errors.New("constructor policy requires at least one symbol")
		}
		if configured[policy.Package] == nil {
			configured[policy.Package] = make(map[string]struct{}, len(policy.Symbols))
		}
		for _, symbol := range policy.Symbols {
			if !validSymbol(symbol) {
				return nil, errors.New("constructor symbols must be Function or Type.Method names")
			}
			if _, duplicate := configured[policy.Package][symbol]; duplicate {
				return nil, errors.New("constructor policy contains a duplicate symbol")
			}
			configured[policy.Package][symbol] = struct{}{}
		}
	}

	return &analysis.Analyzer{
		Name: "constructorgoroutine",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			symbols := configured[pass.Pkg.Path()]
			if len(symbols) == 0 {
				return nil, nil
			}
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, ok := declaration.(*ast.FuncDecl)
					if !ok || function.Body == nil {
						continue
					}
					object, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
					if !ok {
						continue
					}
					symbol := functionSymbol(object)
					if _, configured := symbols[symbol]; !configured {
						continue
					}
					inspectExecuted(function.Body, func(statement *ast.GoStmt) {
						pass.Reportf(
							statement.Go,
							"%s: constructor %s starts a goroutine without caller-visible ownership",
							ruleID,
							symbol,
						)
					})
				}
			}
			return nil, nil
		},
	}, nil
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

func inspectExecuted(node ast.Node, report func(*ast.GoStmt)) {
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.GoStmt:
			report(node)
			return false
		case *ast.CallExpr:
			if literal := calledLiteral(node.Fun); literal != nil {
				inspectExecuted(literal.Body, report)
			}
		case *ast.FuncLit:
			return false
		}
		return true
	})
}

func calledLiteral(expression ast.Expr) *ast.FuncLit {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = parenthesized.X
	}
	literal, _ := expression.(*ast.FuncLit)
	return literal
}
