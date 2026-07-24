// Package globalgoroutine detects goroutines started by package initialization.
package globalgoroutine

import (
	"go/ast"
	"go/token"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "lifecycle/no-global-goroutine"

// Rule is the stable metadata for package-global goroutine ownership.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryLifecycle,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Goroutines started during package initialization have no caller-visible lifetime, shutdown, or error owner.",
	Remediation:       "Move startup behind explicit construction or Start and expose cancellation, waiting, and error contracts.",
	IntroducedVersion: "0.1.0",
}

// Analyzer reports goroutines in immediately executed global initializers.
var Analyzer = &analysis.Analyzer{
	Name: "globalgoroutine",
	Doc:  Rule.Rationale,
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			global, ok := declaration.(*ast.GenDecl)
			if !ok || global.Tok != token.VAR {
				continue
			}
			for _, specification := range global.Specs {
				variable, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, value := range variable.Values {
					inspectExecuted(value, func(statement *ast.GoStmt) {
						pass.Reportf(
							statement.Go,
							"%s: package initializer starts a goroutine without caller-visible ownership",
							ruleID,
						)
					})
				}
			}
		}
	}
	return nil, nil
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
