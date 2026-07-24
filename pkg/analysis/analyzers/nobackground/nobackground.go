// Package nobackground restricts root context creation to composition roots.
package nobackground

import (
	"go/ast"
	"go/types"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"golang.org/x/tools/go/analysis"
)

const ruleID = "context/no-background"

// Options configures packages that own root context creation.
type Options struct {
	AllowedPackages []string
}

// Rule is the stable metadata for root context policy.
var Rule = shared.Rule{
	ID:                ruleID,
	Category:          shared.CategoryContext,
	Severity:          shared.SeverityError,
	DefaultStatus:     shared.StatusAdvisory,
	Rationale:         "Replacing an available lifecycle with a root context loses cancellation and deadlines.",
	Remediation:       "Propagate the caller context or create roots only in approved composition packages.",
	IntroducedVersion: "0.1.0",
	Configuration: shared.ConfigurationSchema{Properties: map[string]shared.ConfigurationProperty{
		"allowed_packages": {
			Type:        shared.ConfigurationArray,
			Description: "Exact package paths allowed to create root contexts.",
		},
	}},
}

// Analyzer reports root and detached contexts outside approved roots.
var Analyzer = New(Options{})

// New constructs an independently configured analyzer.
func New(options Options) *analysis.Analyzer {
	allowed := make(map[string]struct{}, len(options.AllowedPackages))
	for _, packagePath := range options.AllowedPackages {
		allowed[packagePath] = struct{}{}
	}

	return &analysis.Analyzer{
		Name: "nobackground",
		Doc:  Rule.Rationale,
		Run: func(pass *analysis.Pass) (any, error) {
			if _, ok := allowed[pass.Pkg.Path()]; ok {
				return nil, nil
			}
			for _, file := range pass.Files {
				if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
					continue
				}
				ast.Inspect(file, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok {
						reportCall(pass, call)
					}
					return true
				})
			}
			return nil, nil
		},
	}
}

func reportCall(pass *analysis.Pass, call *ast.CallExpr) {
	var object types.Object
	switch function := call.Fun.(type) {
	case *ast.Ident:
		object = pass.TypesInfo.Uses[function]
	case *ast.SelectorExpr:
		object = pass.TypesInfo.Uses[function.Sel]
	default:
		return
	}
	called, ok := object.(*types.Func)
	if !ok || called.Pkg() == nil || called.Pkg().Path() != "context" {
		return
	}
	if called.Name() != "Background" && called.Name() != "TODO" &&
		called.Name() != "WithoutCancel" {
		return
	}
	pass.Reportf(
		call.Fun.Pos(),
		"%s: context.%s is restricted to approved roots",
		ruleID,
		called.Name(),
	)
}
