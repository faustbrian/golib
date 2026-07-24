// Package nodefaulthttp forbids process-global HTTP clients and transports.
package nodefaulthttp

import (
	"go/ast"
	"go/types"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "http/no-default-client"

var forbiddenVariables = map[string]struct{}{
	"DefaultClient":    {},
	"DefaultTransport": {},
}

// Rule is the stable metadata for HTTP lifecycle ownership policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryHTTP,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Process-global HTTP defaults hide connection, timeout, and shutdown ownership.",
	Remediation:       "Construct and inject an explicit http.Client and cloned transport with reviewed timeouts.",
	IntroducedVersion: "0.1.0",
	Configuration:     shared.ConfigurationSchema{},
}

// Analyzer reports references to net/http's process-global defaults.
var Analyzer = &analysis.Analyzer{
	Name: "nodefaulthttp",
	Doc:  Rule.Rationale,
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			ownedClones := ownedTransportClones(pass, file)
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if ok {
					reportReference(pass, identifier, ownedClones)
				}
				return true
			})
		}
		return nil, nil
	},
}

func ownedTransportClones(pass *analysis.Pass, file *ast.File) map[*ast.Ident]struct{} {
	clones := make(map[*ast.Ident]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "Clone" {
			return true
		}
		assertion, ok := method.X.(*ast.TypeAssertExpr)
		if !ok || !isHTTPTransport(pass.TypesInfo.TypeOf(assertion.Type)) {
			return true
		}
		identifier := referenceIdentifier(assertion.X)
		if identifier == nil {
			return true
		}
		variable, ok := pass.TypesInfo.Uses[identifier].(*types.Var)
		if ok && variable.Pkg() != nil && variable.Pkg().Path() == "net/http" &&
			variable.Name() == "DefaultTransport" {
			clones[identifier] = struct{}{}
		}
		return true
	})
	return clones
}

func referenceIdentifier(expression ast.Expr) *ast.Ident {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression
	case *ast.SelectorExpr:
		return expression.Sel
	default:
		return nil
	}
}

func isHTTPTransport(value types.Type) bool {
	pointer, ok := value.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := pointer.Elem().(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" &&
		named.Obj().Name() == "Transport"
}

func reportReference(
	pass *analysis.Pass,
	identifier *ast.Ident,
	ownedClones map[*ast.Ident]struct{},
) {
	if _, allowed := ownedClones[identifier]; allowed {
		return
	}
	variable, ok := pass.TypesInfo.Uses[identifier].(*types.Var)
	if !ok || variable.Pkg() == nil || variable.Pkg().Path() != "net/http" {
		return
	}
	if _, forbidden := forbiddenVariables[variable.Name()]; !forbidden {
		return
	}
	pass.Reportf(
		identifier.Pos(),
		"%s: http.%s has shared implicit lifecycle and timeout policy",
		ruleID,
		variable.Name(),
	)
}
