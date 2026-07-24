// Package forbiddenapi enforces organization-specific API migrations.
package forbiddenapi

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

const ruleID = "api/forbidden-call"

// Policy identifies one exact callable and its approved replacement.
type Policy struct {
	Package         string
	Symbol          string
	Replacement     string
	AllowedPackages []string
}

// Options configures organization-specific API migrations.
type Options struct {
	Policies []Policy
}

type symbolKey struct {
	packagePath string
	symbol      string
}

type compiledPolicy struct {
	replacement string
	allowed     map[string]struct{}
}

// Rule is the stable metadata for forbidden API migrations.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryAPI,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Repository-specific forbidden APIs preserve deprecated dependencies and boundary bypasses.",
	Remediation:       "Use the configured replacement or move the call into a reviewed adapter.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"policies": {
			Type:        shared.ConfigurationArray,
			Description: "Exact package symbols, replacements, and reviewed package exceptions.",
		},
	}},
}

// Analyzer is the unconfigured analyzer used by inventory tooling.
var Analyzer, _ = New(Options{})

// New validates migration policy and constructs an analyzer.
func New(options Options) (*analysis.Analyzer, error) {
	configured := make(map[symbolKey]compiledPolicy, len(options.Policies))
	for _, policy := range options.Policies {
		if !exactPackage(policy.Package) {
			return nil, errors.New("forbidden API policy requires an exact clean package path")
		}
		if !validSymbol(policy.Symbol) {
			return nil, errors.New("forbidden API policy requires a Function or Type.Method symbol")
		}
		replacement := strings.TrimSpace(policy.Replacement)
		if replacement == "" {
			return nil, errors.New("forbidden API policy requires a replacement")
		}
		key := symbolKey{packagePath: policy.Package, symbol: policy.Symbol}
		if _, duplicate := configured[key]; duplicate {
			return nil, errors.New("forbidden API policy contains a duplicate symbol")
		}
		allowed := make(map[string]struct{}, len(policy.AllowedPackages))
		for _, packagePath := range policy.AllowedPackages {
			if !exactPackage(packagePath) {
				return nil, errors.New("forbidden API exceptions require exact clean package paths")
			}
			allowed[packagePath] = struct{}{}
		}
		configured[key] = compiledPolicy{replacement: replacement, allowed: allowed}
	}

	return &analysis.Analyzer{
		Name: "forbiddenapi",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok {
						reportCall(pass, call, configured)
					}
					return true
				})
			}
			return nil, nil
		},
	}, nil
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

func reportCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	configured map[symbolKey]compiledPolicy,
) {
	object := calledObject(pass, call.Fun)
	called, ok := object.(*types.Func)
	if !ok || called.Pkg() == nil {
		return
	}
	symbol := functionSymbol(called)
	policy, forbidden := configured[symbolKey{
		packagePath: called.Pkg().Path(),
		symbol:      symbol,
	}]
	if !forbidden {
		return
	}
	if _, allowed := policy.allowed[pass.Pkg.Path()]; allowed {
		return
	}
	pass.Reportf(
		call.Fun.Pos(),
		"%s: %s.%s is forbidden; use %s",
		ruleID,
		called.Pkg().Path(),
		symbol,
		policy.replacement,
	)
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
